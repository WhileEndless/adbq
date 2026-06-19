package adb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// One-click frida-server install. We list the official GitHub releases, filter
// to the device's architecture, mark which versions are already on the device,
// and (on demand) download → verify → decompress → push the chosen build.
//
// Per CLAUDE.md §1.2 we never run an unverified blob: downloads are restricted
// to official GitHub release hosts, the streamed bytes are checked against the
// GitHub-published asset digest when present, and the decompressed ELF's arch
// is sanity-checked host-side before it is pushed. Decompression uses a
// host-installed tool (xz / 7-Zip) — no new Go dependency, and if none is
// present we tell the user where to get one rather than bundling a binary.

const fridaReleasesAPI = "https://api.github.com/repos/frida/frida/releases?per_page=50"

// FridaRelease is one installable frida-server build matching the device arch.
type FridaRelease struct {
	Version   string `json:"version"`
	Arch      string `json:"arch"`
	AssetURL  string `json:"assetURL"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`    // from GitHub asset digest; "" when unavailable
	Installed bool   `json:"installed"` // already present in /data/local/tmp
}

// fridaArchForABI maps a device ABI (ro.product.cpu.abi) to the frida-server
// asset architecture token. Returns "" for unsupported ABIs.
func fridaArchForABI(abi string) string {
	switch strings.TrimSpace(abi) {
	case "arm64-v8a":
		return "arm64"
	case "armeabi-v7a", "armeabi":
		return "arm"
	case "x86_64":
		return "x86_64"
	case "x86":
		return "x86"
	}
	return ""
}

// FridaArchInfo describes the device's CPU ABIs and which frida-server
// architectures are usable on it, so the UI can show the detected target and
// let the user override it when auto-detection is ambiguous.
type FridaArchInfo struct {
	ABI       string   `json:"abi"`       // ro.product.cpu.abi (primary)
	ABIList   string   `json:"abiList"`   // ro.product.cpu.abilist
	Bits64    bool     `json:"bits64"`    // device has a 64-bit ABI
	Primary   string   `json:"primary"`   // frida arch for the primary ABI
	Supported []string `json:"supported"` // frida arches available on the device, primary first
}

// FridaArchInfo reports the device ABIs and the frida-server arches it can run.
func (c *Client) FridaArchInfo(ctx context.Context, serial string) (*FridaArchInfo, error) {
	abi := strings.TrimSpace(firstNonEmpty(c.Shell(ctx, serial, "getprop ro.product.cpu.abi")))
	abilist := strings.TrimSpace(firstNonEmpty(c.Shell(ctx, serial, "getprop ro.product.cpu.abilist")))
	abilist64 := strings.TrimSpace(firstNonEmpty(c.Shell(ctx, serial, "getprop ro.product.cpu.abilist64")))
	info := &FridaArchInfo{ABI: abi, ABIList: abilist, Bits64: abilist64 != "", Primary: fridaArchForABI(abi)}

	seen := map[string]bool{}
	add := func(a string) {
		if a != "" && !seen[a] {
			seen[a] = true
			info.Supported = append(info.Supported, a)
		}
	}
	add(info.Primary)
	for _, x := range strings.Split(abilist, ",") {
		add(fridaArchForABI(strings.TrimSpace(x)))
	}
	return info, nil
}

func firstNonEmpty(s string, _ error) string { return s }

// ListFridaReleases returns the frida-server versions installable on the device,
// newest first, each flagged if already on the device. When arch is empty it is
// auto-detected from the device's primary ABI; pass a specific frida arch
// ("arm64"/"arm"/"x86_64"/"x86") to override.
func (c *Client) ListFridaReleases(ctx context.Context, serial, arch string) ([]FridaRelease, error) {
	if arch == "" {
		abi, err := c.Shell(ctx, serial, "getprop ro.product.cpu.abi")
		if err != nil {
			return nil, fmt.Errorf("read device abi: %w", err)
		}
		abi = strings.TrimSpace(abi)
		arch = fridaArchForABI(abi)
		if arch == "" {
			return nil, fmt.Errorf("no frida-server build for device ABI %q", abi)
		}
	}

	releases, err := fetchFridaReleases(ctx)
	if err != nil {
		return nil, err
	}

	// Versions already on the device (best-effort; absence just means none flagged).
	installed := map[string]bool{}
	if servers, err := c.ListFridaServers(ctx, serial); err == nil {
		for _, s := range servers {
			if s.Version != "" {
				installed[s.Version+"|"+s.Arch] = true
			}
		}
	}

	wantSuffix := "android-" + arch + ".xz"
	out := make([]FridaRelease, 0, len(releases))
	for _, r := range releases {
		ver := strings.TrimPrefix(r.TagName, "v")
		for _, a := range r.Assets {
			// Exact arch match: the "android-<arch>.xz" suffix can't confuse
			// arm/arm64 or x86/x86_64 because the arch token is anchored to ".xz".
			if !strings.HasPrefix(a.Name, "frida-server-") || !strings.HasSuffix(a.Name, wantSuffix) {
				continue
			}
			out = append(out, FridaRelease{
				Version:   ver,
				Arch:      arch,
				AssetURL:  a.URL,
				Size:      a.Size,
				SHA256:    strings.TrimPrefix(a.Digest, "sha256:"),
				Installed: installed[ver+"|"+arch],
			})
			break
		}
	}
	return out, nil
}

// InstallFridaServer downloads, verifies, decompresses and pushes the chosen
// frida-server version for the device's architecture. progress (optional) is
// called with short stage labels ("downloading", "decompressing", "pushing")
// so the UI can reflect the long-running steps. Returns the on-device path.
func (c *Client) InstallFridaServer(ctx context.Context, serial, version, arch string, progress func(string)) (string, error) {
	if progress == nil {
		progress = func(string) {}
	}
	// Resolve the asset for this version + arch via the release listing.
	releases, err := c.ListFridaReleases(ctx, serial, arch)
	if err != nil {
		return "", err
	}
	var rel *FridaRelease
	for i := range releases {
		if releases[i].Version == version {
			rel = &releases[i]
			break
		}
	}
	if rel == nil {
		return "", fmt.Errorf("frida-server %s is not available for this device's architecture", version)
	}

	// Fail fast if we can't decompress before spending a download on it.
	dec, err := findHostDecompressor()
	if err != nil {
		return "", err
	}

	dir, err := fridaCacheDir()
	if err != nil {
		return "", err
	}
	binName := fmt.Sprintf("frida-server-%s-android-%s", rel.Version, rel.Arch)
	xzPath := filepath.Join(dir, binName+".xz")
	binPath := filepath.Join(dir, binName)

	progress("downloading")
	if err := downloadFridaAsset(ctx, rel.AssetURL, rel.SHA256, xzPath); err != nil {
		return "", err
	}

	progress("decompressing")
	if err := dec.run(ctx, xzPath, binPath); err != nil {
		return "", err
	}
	if err := verifyFridaELF(binPath, rel.Arch); err != nil {
		_ = os.Remove(binPath)
		return "", err
	}

	progress("pushing")
	remote := "/data/local/tmp/" + binName
	if _, err := c.PushFile(ctx, serial, binPath, remote); err != nil {
		return "", fmt.Errorf("push to device: %w", err)
	}
	if _, err := c.Shell(ctx, serial, "chmod 755 "+remote); err != nil {
		return remote, fmt.Errorf("chmod on device: %w", err)
	}
	return remote, nil
}

// ─── GitHub releases ───────────────────────────────────────────────────────

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"` // e.g. "sha256:abcd…"
}

func fetchFridaReleases(ctx context.Context) ([]ghRelease, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, "GET", fridaReleasesAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "adbq/frida-installer")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch frida releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("GitHub API rate limit reached (60 requests/hour unauthenticated) — try again shortly")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned HTTP %d for frida releases", resp.StatusCode)
	}
	// The releases payload is large — each release carries full metadata for
	// ~50 per-arch assets, so 50 releases is ~20 MB. Cap generously; we only
	// keep the handful of fields we need and the heavy JSON never leaves the
	// backend (ListFridaReleases returns one small entry per version).
	var rels []ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&rels); err != nil {
		return nil, fmt.Errorf("parse frida releases: %w", err)
	}
	return rels, nil
}

// downloadFridaAsset streams the asset into dst, verifying the SHA256 against
// the GitHub-published digest when one is available. A verified copy already on
// disk is reused. URLs are restricted to official GitHub release hosts.
func downloadFridaAsset(ctx context.Context, url, wantSum, dst string) error {
	if !(strings.HasPrefix(url, "https://github.com/") ||
		strings.HasPrefix(url, "https://objects.githubusercontent.com/")) {
		return fmt.Errorf("refusing to download frida from non-github host: %s", url)
	}
	wantSum = strings.ToLower(strings.TrimSpace(wantSum))

	if info, err := os.Stat(dst); err == nil && info.Size() > 0 {
		if wantSum == "" || sumFile(dst) == wantSum {
			return nil
		}
		_ = os.Remove(dst)
	}

	dlCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(dlCtx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "adbq/frida-installer")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), "frida-*.part")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("download body: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if wantSum != "" {
		if got := hex.EncodeToString(h.Sum(nil)); got != wantSum {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("sha256 mismatch: got %s, GitHub published %s", got, wantSum)
		}
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func fridaCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "adbq", "frida")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// ─── Host-side .xz decompression (no Go dependency) ────────────────────────

// decompressor is a host CLI tool that reads an .xz file and writes the raw
// bytes to stdout. args precede the source path.
type decompressor struct {
	name string
	args []string
}

func (d decompressor) run(ctx context.Context, src, dst string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	cmd := exec.CommandContext(ctx, d.name, append(append([]string{}, d.args...), src)...)
	cmd.Stdout = out
	var errBuf capBuffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		_ = os.Remove(dst)
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("decompress with %s: %s", filepath.Base(d.name), msg)
	}
	return nil
}

// findHostDecompressor locates a usable .xz decompressor on the host, or
// returns an actionable error pointing the user at a download.
func findHostDecompressor() (decompressor, error) {
	for _, cand := range []decompressor{
		{"xz", []string{"-dc"}},      // xz -dc <src> → stdout
		{"unxz", []string{"-c"}},     // unxz -c <src> → stdout
		{"7z", []string{"e", "-so"}}, // 7z e -so <src> → stdout
		{"7za", []string{"e", "-so"}},
		{"7zz", []string{"e", "-so"}},
	} {
		if p, ok := lookTool(cand.name); ok {
			cand.name = p
			return cand, nil
		}
	}
	return decompressor{}, fmt.Errorf("no .xz decompressor found on this computer — install xz (https://tukaani.org/xz/, or `brew install xz` on macOS) or 7-Zip (https://www.7-zip.org/) and try again")
}

// lookTool resolves a CLI tool by name, falling back to common install
// locations when PATH lookup fails. GUI apps launched from Finder/Dock on macOS
// inherit a minimal PATH (no /opt/homebrew/bin or /usr/local/bin), so a plain
// exec.LookPath misses Homebrew/MacPorts tools the user clearly has installed.
func lookTool(name string) (string, bool) {
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	var dirs []string
	exts := []string{""}
	switch runtime.GOOS {
	case "darwin":
		dirs = []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin", "/usr/bin", "/bin"}
	case "windows":
		exts = []string{".exe"}
		dirs = []string{`C:\Program Files\7-Zip`, `C:\Program Files (x86)\7-Zip`}
		if pf := os.Getenv("ProgramFiles"); pf != "" {
			dirs = append(dirs, filepath.Join(pf, "7-Zip"))
		}
	default:
		dirs = []string{"/usr/local/bin", "/usr/bin", "/bin", "/snap/bin"}
	}
	for _, d := range dirs {
		for _, e := range exts {
			p := filepath.Join(d, name+e)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p, true
			}
		}
	}
	return "", false
}

// verifyFridaELF checks the decompressed file is an ELF whose machine type
// matches the requested Android arch, so a corrupted download or wrong asset is
// caught host-side before anything lands on the device.
func verifyFridaELF(path, arch string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var hdr [20]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return fmt.Errorf("read ELF header: %w", err)
	}
	if hdr[0] != 0x7f || hdr[1] != 'E' || hdr[2] != 'L' || hdr[3] != 'F' {
		return fmt.Errorf("decompressed frida-server is not an ELF binary (corrupt download?)")
	}
	// All Android targets are little-endian; e_machine is at offset 18.
	machine := uint16(hdr[18]) | uint16(hdr[19])<<8
	want := map[string]uint16{"arm64": 183, "arm": 40, "x86_64": 62, "x86": 3}[arch]
	if want != 0 && machine != want {
		return fmt.Errorf("decompressed frida-server arch mismatch (ELF machine=%d, expected %s)", machine, arch)
	}
	return nil
}
