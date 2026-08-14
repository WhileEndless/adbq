package adb

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// systemImageListTTL bounds how long a remote package listing is reused.
// `sdkmanager --list` fetches every repository manifest Google publishes and
// takes tens of seconds; re-running it per screen render would be unusable.
const systemImageListTTL = 15 * time.Minute

// SystemImage is one installable emulator system image.
type SystemImage struct {
	Pkg        string `json:"pkg"`   // system-images;android-34;google_apis;arm64-v8a
	Level      string `json:"level"` // android-34 / android-36.1 / android-CinnamonBun
	API        int    `json:"api"`   // major level; 0 for preview codenames
	AndroidVer string `json:"androidVer"`
	Tag        string `json:"tag"` // default | google_apis | google_apis_playstore | aosp_atd …
	ABI        string `json:"abi"`
	PlayStore  bool   `json:"playStore"`
	Revision   string `json:"revision"`
	Desc       string `json:"desc"`
	Installed  bool   `json:"installed"`
	Location   string `json:"location"` // absolute path when installed
	// Rootable records whether `adb root` alone can root this image. Play Store
	// images refuse it, which is the whole reason rootAVD exists.
	Rootable bool `json:"rootable"`

	// Compatible reports whether this image's ABI can actually run on this
	// computer. An x86_64 image on an Apple Silicon Mac installs happily and
	// then never boots, so the answer belongs next to the Install button rather
	// than in the emulator's error output half an hour later.
	Compatible bool   `json:"compatible"`
	Note       string `json:"note"`

	Commands []string `json:"commands"`
}

// hostABIs lists the Android ABIs this computer can emulate at native speed,
// best first. The emulator will not run a foreign-architecture image at all on
// Apple Silicon, and does so unusably slowly elsewhere.
func hostABIs() []string {
	switch runtime.GOARCH {
	case "arm64":
		return []string{"arm64-v8a", "armeabi-v7a"}
	case "amd64":
		return []string{"x86_64", "x86"}
	default:
		return nil
	}
}

// HostABIs exposes the preferred ABI order to the UI, so an image list can be
// sorted and labelled without duplicating the rule in TypeScript.
func HostABIs() []string {
	abis := hostABIs()
	if abis == nil {
		return []string{}
	}
	return abis
}

// abiRank orders images by how well they suit this host: 0 is native, higher is
// worse, and a negative result means the host has no opinion.
func abiRank(abi string) int {
	for i, a := range hostABIs() {
		if a == abi {
			return i
		}
	}
	return -1
}

// applyHostCompat marks an image runnable-or-not on this computer.
func applyHostCompat(img *SystemImage) {
	preferred := hostABIs()
	if len(preferred) == 0 {
		// Unknown host architecture: claim nothing rather than mislabel.
		img.Compatible = true
		return
	}
	rank := abiRank(img.ABI)
	switch {
	case rank == 0:
		img.Compatible = true
	case rank > 0:
		img.Compatible = true
		img.Note = img.ABI + " runs on this computer but is emulated, so it will be slow — prefer " + preferred[0]
	default:
		img.Compatible = false
		img.Note = "This computer is " + runtime.GOARCH + "; a " + img.ABI + " image will not run here. Choose " + preferred[0] + "."
	}
}

// PackageManager wraps sdkmanager and the on-disk system-images tree.
type PackageManager struct {
	sdk *SDKManager

	mu       sync.Mutex
	cache    []SystemImage
	cachedAt time.Time
}

func NewPackageManager(sdk *SDKManager) *PackageManager {
	return &PackageManager{sdk: sdk}
}

// ListInstalledImages reads the SDK's system-images tree directly. This never
// touches the network and works without the command-line tools installed, so
// the "what do I already have" half of the UI is always correct and instant.
func (p *PackageManager) ListInstalledImages() []SystemImage {
	root := p.sdk.Root()
	out := []SystemImage{}
	if root == "" {
		return out
	}
	base := filepath.Join(root, "system-images")
	levels, err := os.ReadDir(base)
	if err != nil {
		return out
	}
	for _, lvl := range levels {
		if !lvl.IsDir() {
			continue
		}
		tags, err := os.ReadDir(filepath.Join(base, lvl.Name()))
		if err != nil {
			continue
		}
		for _, tag := range tags {
			if !tag.IsDir() {
				continue
			}
			abis, err := os.ReadDir(filepath.Join(base, lvl.Name(), tag.Name()))
			if err != nil {
				continue
			}
			for _, abi := range abis {
				if !abi.IsDir() {
					continue
				}
				dir := filepath.Join(base, lvl.Name(), tag.Name(), abi.Name())
				// source.properties is what makes a directory a real package
				// rather than a leftover from a half-deleted install.
				props, err := readIni(filepath.Join(dir, "source.properties"))
				if err != nil {
					continue
				}
				img := newSystemImage(lvl.Name(), tag.Name(), abi.Name())
				img.Installed = true
				img.Location = dir
				img.Revision = props["Pkg.Revision"]
				img.Desc = props["Pkg.Desc"]
				img.Commands = p.commandsFor(img)
				out = append(out, img)
			}
		}
	}
	sortSystemImages(out)
	return out
}

// ListSystemImages returns installed images merged with everything installable.
// The remote half needs sdkmanager and the network; when either is missing the
// installed half is still returned, with the error explaining what is missing.
func (p *PackageManager) ListSystemImages(ctx context.Context, refresh bool) ([]SystemImage, error) {
	installed := p.ListInstalledImages()

	p.mu.Lock()
	fresh := !refresh && p.cache != nil && time.Since(p.cachedAt) < systemImageListTTL
	cached := p.cache
	p.mu.Unlock()

	var remote []SystemImage
	var err error
	if fresh {
		remote = cached
	} else {
		remote, err = p.fetchRemoteImages(ctx)
		if err == nil {
			p.mu.Lock()
			p.cache, p.cachedAt = remote, time.Now()
			p.mu.Unlock()
		}
	}

	merged := mergeSystemImages(installed, remote)
	for i := range merged {
		merged[i].Commands = p.commandsFor(merged[i])
	}
	return merged, err
}

func (p *PackageManager) fetchRemoteImages(ctx context.Context) ([]SystemImage, error) {
	bin, err := p.SDKManagerBin()
	if err != nil {
		return nil, err
	}
	// The manifest fetch is slow but bounded; without a cap a dead network
	// would hang the screen indefinitely.
	lctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(lctx, bin, "--list").CombinedOutput()
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("sdkmanager --list failed: %w", err)
	}
	return parseSDKManagerList(string(out)), nil
}

func (p *PackageManager) SDKManagerBin() (string, error) { return p.sdk.SDKManagerBin() }

// InstallSystemImage downloads and installs one package, reporting percentage
// progress parsed from sdkmanager's progress bar.
func (p *PackageManager) InstallSystemImage(ctx context.Context, pkg string, onProgress func(stage string, pct int)) error {
	if onProgress == nil {
		onProgress = func(string, int) {}
	}
	if err := validateSDKPackage(pkg); err != nil {
		return err
	}
	bin, err := p.SDKManagerBin()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, pkg)
	// sdkmanager asks about licences on stdin. adbq accepts them explicitly in
	// the UI beforehand, never silently: an empty stdin makes it fail loudly
	// rather than hang forever waiting for an answer that never comes.
	cmd.Stdin = strings.NewReader("")

	// One pipe for both streams: sdkmanager splits its progress bar and its
	// errors across them, and reading them separately interleaves badly.
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return err
	}
	pw.Close() // the child holds the only remaining write end

	var tail []string
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	// The progress bar is redrawn with \r, not \n, so split on both.
	sc.Split(scanLinesOrCR)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		stage, pct := parseSDKManagerProgress(line)
		onProgress(stage, pct)
		tail = append(tail, line)
		if len(tail) > 12 {
			tail = tail[1:]
		}
	}
	pr.Close()
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%s", sdkManagerError(tail, err))
	}
	p.Invalidate()
	return nil
}

// UninstallSystemImage removes an installed package. Destructive: the caller is
// responsible for confirming with the user first.
func (p *PackageManager) UninstallSystemImage(ctx context.Context, pkg string) error {
	if err := validateSDKPackage(pkg); err != nil {
		return err
	}
	bin, err := p.SDKManagerBin()
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(cctx, bin, "--uninstall", pkg).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", sdkManagerError(strings.Split(string(out), "\n"), err))
	}
	p.Invalidate()
	return nil
}

// Invalidate drops the cached remote listing, so the next call re-reads it.
func (p *PackageManager) Invalidate() {
	p.mu.Lock()
	p.cache, p.cachedAt = nil, time.Time{}
	p.mu.Unlock()
}

// commandsFor renders what adbq would run for this image (CLAUDE.md §4.1).
func (p *PackageManager) commandsFor(img SystemImage) []string {
	bin := p.sdk.Info().SDKManager
	if bin == "" {
		bin = "sdkmanager"
	}
	q := shellQuoteLocal(bin)
	if img.Installed {
		return []string{q + " --uninstall " + shellQuoteLocal(img.Pkg)}
	}
	return []string{q + " " + shellQuoteLocal(img.Pkg)}
}

// ─── pure parsing ──────────────────────────────────────────────────────────

// newSystemImage builds an image record from its three path components, which
// are exactly the last three fields of the sdkmanager package path.
func newSystemImage(level, tag, abi string) SystemImage {
	img := SystemImage{
		Pkg:   strings.Join([]string{"system-images", level, tag, abi}, ";"),
		Level: level,
		Tag:   tag,
		ABI:   abi,
	}
	img.API = apiFromTarget(level)
	if img.API > 0 {
		img.AndroidVer = AndroidVersionForSdk(strconv.Itoa(img.API))
	} else {
		img.AndroidVer = strings.TrimPrefix(level, "android-") // preview codename
	}
	img.PlayStore = strings.Contains(tag, "playstore") || strings.Contains(tag, "_ps")
	// Google's Play Store images ship a production adbd that refuses `adb root`;
	// every other tag boots a debuggable image where root is one command away.
	img.Rootable = !img.PlayStore
	applyHostCompat(&img)
	return img
}

// parseSDKManagerList reads the pipe-separated table sdkmanager prints. Only
// system images are kept — the full listing is thousands of rows the Emulators
// screen has no use for.
func parseSDKManagerList(out string) []SystemImage {
	var images []SystemImage
	installedSection := false
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		low := strings.ToLower(line)
		switch {
		case strings.HasPrefix(low, "installed packages"):
			installedSection = true
			continue
		case strings.HasPrefix(low, "available packages"),
			strings.HasPrefix(low, "available updates"):
			installedSection = false
			continue
		}
		if !strings.HasPrefix(line, "system-images;") {
			continue
		}
		cols := splitPipeRow(line)
		img, ok := systemImageFromPkg(cols[0])
		if !ok {
			continue
		}
		if len(cols) > 1 {
			img.Revision = cols[1]
		}
		if len(cols) > 2 {
			img.Desc = cols[2]
		}
		if len(cols) > 3 {
			img.Location = cols[3]
		}
		img.Installed = installedSection
		images = append(images, img)
	}
	sortSystemImages(images)
	return images
}

func splitPipeRow(line string) []string {
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// systemImageFromPkg parses "system-images;android-34;google_apis;arm64-v8a".
func systemImageFromPkg(pkg string) (SystemImage, bool) {
	f := strings.Split(strings.TrimSpace(pkg), ";")
	if len(f) != 4 || f[0] != "system-images" {
		return SystemImage{}, false
	}
	return newSystemImage(f[1], f[2], f[3]), true
}

// validateSDKPackage refuses anything that isn't a well-formed package path, so
// a value from the UI can never turn into extra sdkmanager arguments.
func validateSDKPackage(pkg string) error {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return fmt.Errorf("no package selected")
	}
	if strings.ContainsAny(pkg, " \t\n\"'`$&|<>") {
		return fmt.Errorf("invalid SDK package name %q", pkg)
	}
	if _, ok := systemImageFromPkg(pkg); !ok {
		return fmt.Errorf("%q is not a system-image package path", pkg)
	}
	return nil
}

// mergeSystemImages overlays the remote catalogue on the on-disk truth. Disk
// wins for Installed/Location: the manifest can be stale or unreachable, the
// filesystem cannot.
func mergeSystemImages(installed, remote []SystemImage) []SystemImage {
	byPkg := map[string]SystemImage{}
	order := []string{}
	add := func(img SystemImage) {
		if prev, ok := byPkg[img.Pkg]; ok {
			if prev.Installed {
				img.Installed = true
				if prev.Location != "" {
					img.Location = prev.Location
				}
				if prev.Revision != "" {
					img.Revision = prev.Revision
				}
			}
		} else {
			order = append(order, img.Pkg)
		}
		byPkg[img.Pkg] = img
	}
	for _, i := range installed {
		add(i)
	}
	for _, r := range remote {
		add(r)
	}
	out := make([]SystemImage, 0, len(order))
	for _, k := range order {
		out = append(out, byPkg[k])
	}
	sortSystemImages(out)
	return out
}

// sortSystemImages orders newest API first — the version a user is most likely
// looking for — then by tag and ABI for a stable list.
func sortSystemImages(s []SystemImage) {
	sort.SliceStable(s, func(i, j int) bool {
		// Images this computer can actually run come first — an incompatible
		// one is never the answer, however new it is.
		if s[i].Compatible != s[j].Compatible {
			return s[i].Compatible
		}
		if s[i].API != s[j].API {
			return s[i].API > s[j].API
		}
		if s[i].Level != s[j].Level {
			return s[i].Level > s[j].Level
		}
		if s[i].Tag != s[j].Tag {
			return s[i].Tag < s[j].Tag
		}
		return s[i].ABI < s[j].ABI
	})
}

// parseSDKManagerProgress turns one output line into a stage label and percent.
// sdkmanager interleaves "Downloading …", "Unzipping …" and a redrawn
// "[====    ] 45% Downloading foo" bar on the same stream.
func parseSDKManagerProgress(line string) (stage string, pct int) {
	line = strings.TrimSpace(line)
	if i := strings.Index(line, "]"); strings.HasPrefix(line, "[") && i > 0 {
		line = strings.TrimSpace(line[i+1:])
	}
	if i := strings.Index(line, "%"); i > 0 {
		start := i - 1
		for start >= 0 && line[start] >= '0' && line[start] <= '9' {
			start--
		}
		if n, err := strconv.Atoi(line[start+1 : i]); err == nil {
			pct = n
		}
		line = strings.TrimSpace(line[i+1:])
	}
	return strings.TrimSpace(line), pct
}

// sdkManagerError turns a failed run into one actionable sentence. sdkmanager's
// own diagnostics are buried in a Java stack trace nobody should have to read.
func sdkManagerError(tail []string, err error) string {
	joined := strings.ToLower(strings.Join(tail, "\n"))
	switch {
	case strings.Contains(joined, "license") && strings.Contains(joined, "not accepted"):
		return "the SDK licence for this package has not been accepted — accept it in Android Studio (SDK Manager) or run `sdkmanager --licenses`"
	case strings.Contains(joined, "unknown filter") || strings.Contains(joined, "failed to find package"):
		return "the SDK has no such package — refresh the list and try again"
	case strings.Contains(joined, "warning: unable to compute") || strings.Contains(joined, "connection") ||
		strings.Contains(joined, "unknownhost") || strings.Contains(joined, "timed out"):
		return "could not reach the Android SDK repository — check the network or proxy settings"
	case strings.Contains(joined, "no space left"):
		return "not enough disk space to install this system image"
	}
	for i := len(tail) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(tail[i]); t != "" && !strings.HasPrefix(t, "at ") {
			return t
		}
	}
	return err.Error()
}

// scanLinesOrCR splits on \n and \r, so the progress bar's carriage-return
// redraws are seen as separate updates instead of one enormous line.
func scanLinesOrCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}
