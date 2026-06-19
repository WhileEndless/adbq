package adb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TcpdumpInfo describes a tcpdump binary discovered on the device.
type TcpdumpInfo struct {
	Path      string `json:"path"`
	Version   string `json:"version"`
	Exec      bool   `json:"exec"`   // true if we can run it (chmod 755 + exec policy ok)
	Source    string `json:"source"` // "system", "vendor", "magisk", "tmp", "termux", ...
	Available bool   `json:"available"`
}

// candidate tcpdump locations, ordered by preference (system PATH first since
// it's usually pre-validated for execution under SELinux).
var tcpdumpCandidates = []struct {
	path, source string
}{
	{"/system/bin/tcpdump", "system"},
	{"/system/xbin/tcpdump", "system-xbin"},
	{"/vendor/bin/tcpdump", "vendor"},
	{"/product/bin/tcpdump", "product"},
	{"/data/adb/magisk/tcpdump", "magisk"},
	{"/data/local/tmp/tcpdump", "tmp"},
	{"/data/data/com.termux/files/usr/bin/tcpdump", "termux"},
}

// FindTcpdump probes the device for a usable tcpdump binary, returning its
// path. It checks the well-known system locations first, then writable user
// areas, and finally falls back to whatever `command -v tcpdump` resolves to.
// Returns a clear, actionable error when nothing is found.
func (c *Client) FindTcpdump(ctx context.Context, serial string) (string, error) {
	info, err := c.ProbeTcpdump(ctx, serial)
	if err != nil {
		return "", err
	}
	if !info.Available {
		return "", fmt.Errorf("tcpdump not found on device — open Network → Capture and click \"Install tcpdump\" to push a binary, or install one via Magisk")
	}
	return info.Path, nil
}

// ProbeTcpdump returns a TcpdumpInfo describing whichever tcpdump binary is
// available, including the empty Available=false case when none was found.
// Never returns an error for "missing" — that's expressed via Available.
func (c *Client) ProbeTcpdump(ctx context.Context, serial string) (*TcpdumpInfo, error) {
	// Walk the candidate list using ONLY shell builtins (`[ -f ]`, `[ -x ]`,
	// echo). This ROM is heavily stripped: printf, stat, head etc. are all
	// missing, so the old stat/printf-based probe always reported "not found".
	// Each existing candidate emits "FOUND <source> <path>" and, when the exec
	// bit is set, "EXEC <path>"; we exit after the first hit and parse the
	// lines host-side.
	checks := make([]string, 0, len(tcpdumpCandidates)+2)
	for _, c := range tcpdumpCandidates {
		checks = append(checks, fmt.Sprintf(
			"[ -f %s ] && { echo 'FOUND %s %s'; [ -x %s ] && echo 'EXEC %s'; exit 0; }",
			c.path, c.source, c.path, c.path, c.path))
	}
	// Magisk module trees — vary by module name (e.g. /data/adb/modules/tcpdump/system/bin/tcpdump).
	checks = append(checks, "for f in /data/adb/modules/*/system/bin/tcpdump /data/adb/modules/*/system/xbin/tcpdump; do [ -f \"$f\" ] && { echo \"FOUND magisk-module $f\"; [ -x \"$f\" ] && echo \"EXEC $f\"; exit 0; }; done")
	// PATH fallback — `command -v` is a shell builtin so it's safe here.
	checks = append(checks, "p=$(command -v tcpdump 2>/dev/null) && [ -n \"$p\" ] && { echo \"FOUND path $p\"; [ -x \"$p\" ] && echo \"EXEC $p\"; exit 0; }")
	checks = append(checks, "exit 1")

	out, err := c.Shell(ctx, serial, strings.Join(checks, "; "))
	if err != nil && strings.TrimSpace(out) == "" {
		return &TcpdumpInfo{Available: false}, nil
	}

	var info *TcpdumpInfo
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(strings.TrimRight(ln, "\r"))
		if ln == "" {
			continue
		}
		fields := strings.Fields(ln)
		switch fields[0] {
		case "FOUND":
			// "FOUND <source> <path...>" — path may not contain spaces here.
			if len(fields) >= 3 {
				info = &TcpdumpInfo{
					Source:    fields[1],
					Path:      strings.Join(fields[2:], " "),
					Available: true,
				}
			}
		case "EXEC":
			if info != nil {
				info.Exec = true
			}
		}
	}
	if info == nil || info.Path == "" {
		return &TcpdumpInfo{Available: false}, nil
	}

	// Best-effort version probe — non-fatal. `head` is missing on stripped
	// ROMs, so we read the full output and pick the most relevant line in Go.
	if ver, err := c.Shell(ctx, serial, info.Path+" --version 2>&1"); err == nil {
		info.Version = firstVersionLine(ver)
	}
	return info, nil
}

// firstVersionLine extracts a usable version string from `tcpdump --version`
// output. Some builds print a leading "invalid option -- -" diagnostic before
// the actual version banner, so we prefer the first line mentioning "version"
// and fall back to the first non-empty line.
func firstVersionLine(out string) string {
	var firstNonEmpty string
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(strings.TrimRight(ln, "\r"))
		if ln == "" {
			continue
		}
		if firstNonEmpty == "" {
			firstNonEmpty = ln
		}
		if strings.Contains(strings.ToLower(ln), "version") {
			return ln
		}
	}
	return firstNonEmpty
}

// TcpdumpAutoPlan describes which manifest entry would be used for a device.
// Returned ahead of any network activity so the UI can show the user the
// URL and hash they're about to fetch.
type TcpdumpAutoPlan struct {
	Abi    string `json:"abi"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Source string `json:"source"`
	Size   int64  `json:"size"`
	Cached bool   `json:"cached"` // true when we have the verified blob locally already
}

// PlanTcpdumpAutoInstall picks the right manifest entry for the device and
// reports whether the verified binary is already in the local cache. Does
// not touch the network; safe to call before the user confirms.
func (c *Client) PlanTcpdumpAutoInstall(ctx context.Context, serial string) (*TcpdumpAutoPlan, error) {
	// Try the primary ABI first, then any secondary ABI from abilist: an
	// arm64-v8a device that also lists armeabi-v7a can run the 32-bit build, so
	// it shouldn't be forced down the file-picker path.
	caps := c.Capabilities(ctx, serial)
	candidates := append([]string{caps.ABI}, caps.ABIList...)
	var b *TcpdumpBuild
	primaryABI := ""
	for _, abi := range candidates {
		abi = strings.TrimSpace(abi)
		if abi == "" {
			continue
		}
		if primaryABI == "" {
			primaryABI = abi
		}
		if hit := tcpdumpBuildFor(abi); hit != nil {
			b = hit
			break
		}
	}
	if primaryABI == "" {
		return nil, fmt.Errorf("could not determine device ABI (ro.product.cpu.abi is empty)")
	}
	if b == nil {
		return nil, fmt.Errorf("no pinned tcpdump build for ABI %q — use \"Install from file\" with your own binary", primaryABI)
	}
	if b.SHA256 == "" {
		return nil, fmt.Errorf("manifest is not yet pinned for ABI %q (SHA256 missing); use \"Install from file\" until the next adbq release", b.Abi)
	}
	plan := &TcpdumpAutoPlan{Abi: b.Abi, URL: b.URL, SHA256: b.SHA256, Source: b.Source, Size: b.Size}
	if path, ok := tcpdumpCachedPath(b.SHA256); ok {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			plan.Cached = true
		}
	}
	return plan, nil
}

// InstallTcpdumpAuto downloads (if needed), verifies, pushes and chmods the
// tcpdump binary matching the device's ABI. confirmed must be true — callers
// surface PlanTcpdumpAutoInstall() to the user first and pass confirmed=true
// only after explicit acceptance, mirroring CLAUDE.md §1.2.
func (c *Client) InstallTcpdumpAuto(ctx context.Context, serial string, confirmed bool) (*TcpdumpInfo, error) {
	if !confirmed {
		return nil, fmt.Errorf("auto-install requires explicit user confirmation")
	}
	plan, err := c.PlanTcpdumpAutoInstall(ctx, serial)
	if err != nil {
		return nil, err
	}
	local, err := fetchTcpdumpBlob(ctx, plan.URL, plan.SHA256)
	if err != nil {
		return nil, err
	}
	if _, err := c.InstallTcpdump(ctx, serial, local); err != nil {
		return nil, err
	}
	return c.ProbeTcpdump(ctx, serial)
}

// tcpdumpCacheDir returns the per-user cache directory for verified binaries.
// Each blob is keyed by its SHA256 so a corrupted entry can't masquerade as
// the real one — if the hash doesn't match we never use the file.
func tcpdumpCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "adbq", "tcpdump")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func tcpdumpCachedPath(sum string) (string, bool) {
	dir, err := tcpdumpCacheDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(dir, sum+".bin"), true
}

// fetchTcpdumpBlob streams the manifest URL into the cache and verifies the
// hash on the fly. Returns the cache path. If a verified copy already exists
// it skips the network entirely.
func fetchTcpdumpBlob(ctx context.Context, url, wantSum string) (string, error) {
	if !(strings.HasPrefix(url, "https://github.com/") ||
		strings.HasPrefix(url, "https://raw.githubusercontent.com/") ||
		strings.HasPrefix(url, "https://objects.githubusercontent.com/")) {
		return "", fmt.Errorf("manifest URL is not on a github.com host — refusing: %s", url)
	}
	if len(wantSum) != 64 {
		return "", fmt.Errorf("manifest SHA256 looks malformed (%d chars)", len(wantSum))
	}
	dst, ok := tcpdumpCachedPath(wantSum)
	if !ok {
		return "", fmt.Errorf("could not resolve cache directory")
	}
	if info, err := os.Stat(dst); err == nil && info.Size() > 0 {
		// Re-verify the cached copy — disk bitrot is unlikely but free to check.
		if sumFile(dst) == strings.ToLower(wantSum) {
			return dst, nil
		}
		_ = os.Remove(dst)
	}
	dlCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(dlCtx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "adbq/tcpdump-installer")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "tcpdump-*.part")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	h := sha256.New()
	mw := io.MultiWriter(tmp, h)
	if _, err := io.Copy(mw, resp.Body); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("download body: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	gotSum := hex.EncodeToString(h.Sum(nil))
	if gotSum != strings.ToLower(wantSum) {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("sha256 mismatch: got %s, manifest expected %s", gotSum, wantSum)
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return dst, nil
}

func sumFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// InstallTcpdump copies a local tcpdump binary onto the device at
// /data/local/tmp/tcpdump and chmod 755's it. Returns the on-device path.
// Caller is expected to have validated the local file. We deliberately do not
// download from the internet here — project rules forbid auto-fetching binary
// blobs from unverified sources. The UI uses a file picker so the user is in
// the loop about which binary lands on their device.
func (c *Client) InstallTcpdump(ctx context.Context, serial, localPath string) (string, error) {
	const remote = "/data/local/tmp/tcpdump"
	if _, err := c.PushFile(ctx, serial, localPath, remote); err != nil {
		return "", fmt.Errorf("push failed: %w", err)
	}
	if _, err := c.Shell(ctx, serial, "chmod 755 "+remote); err != nil {
		return remote, fmt.Errorf("chmod failed: %w", err)
	}
	// Sanity check: does it actually run? Many users push x86 binaries to
	// arm64 devices and only find out when capture silently fails. Read the
	// full output and take the first line host-side — `head` is absent on the
	// stripped ROMs this tool targets.
	out, err := c.Shell(ctx, serial, remote+" --version 2>&1")
	low := strings.ToLower(out)
	if err != nil || strings.Contains(low, "not executable") || strings.Contains(low, "exec format error") {
		return remote, fmt.Errorf("pushed file is not executable on this device (wrong arch?): %s", firstLine(strings.TrimSpace(out)))
	}
	return remote, nil
}
