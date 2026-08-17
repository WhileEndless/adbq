package adb

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// AndroidSDKInfo describes the Android SDK / Android Studio toolchain found on
// this computer. It mirrors FridaHostInfo: one flat struct the UI can render as
// a status card, with a single actionable sentence in Error when unavailable.
type AndroidSDKInfo struct {
	Available bool `json:"available"`

	SDKRoot string `json:"sdkRoot"`
	// Source records which rule resolved SDKRoot, so the UI can explain itself:
	// "setting" | "ANDROID_HOME" | "ANDROID_SDK_ROOT" | "default".
	Source string `json:"source"`

	Emulator    string `json:"emulator"`
	EmulatorVer string `json:"emulatorVer"`
	AVDManager  string `json:"avdManager"`
	SDKManager  string `json:"sdkManager"`
	ADB         string `json:"adb"`
	AVDHome     string `json:"avdHome"`

	StudioPath string `json:"studioPath"`
	StudioVer  string `json:"studioVer"`

	// Accelerated is the result of `emulator -accel-check`: without hardware
	// acceleration an AVD still boots, but slowly enough to look broken.
	Accelerated bool   `json:"accelerated"`
	AccelNote   string `json:"accelNote"`

	Error string `json:"error"`
}

// SDKManager resolves and caches the Android SDK toolchain. Like ScrcpyManager
// it caches the resolved paths and revalidates them cheaply, because every AVD
// listing and launch goes through it.
type SDKManager struct {
	mu    sync.Mutex
	host  *HostStore
	cache *AndroidSDKInfo
}

func NewSDKManager(host *HostStore) *SDKManager {
	if host == nil {
		host = NewHostStore()
	}
	return &SDKManager{host: host}
}

// Info returns the (cached) toolchain description. The probe shells out to
// `emulator -version` and `-accel-check`, so it is cached until Recheck.
func (m *SDKManager) Info() AndroidSDKInfo {
	m.mu.Lock()
	cached := m.cache
	m.mu.Unlock()
	if cached != nil {
		// A cached path that has since been deleted (SDK moved, uninstalled)
		// must not be handed out as if it were live.
		if cached.SDKRoot == "" || dirExists(cached.SDKRoot) {
			return *cached
		}
	}
	info := m.probe()
	m.mu.Lock()
	m.cache = &info
	m.mu.Unlock()
	return info
}

// Recheck drops the cache and re-probes.
func (m *SDKManager) Recheck() AndroidSDKInfo {
	m.mu.Lock()
	m.cache = nil
	m.mu.Unlock()
	return m.Info()
}

// Available reports whether an emulator binary was found — the minimum needed
// for the Emulators screen to do anything at all.
func (m *SDKManager) Available() bool { return m.Info().Emulator != "" }

// Emulator returns the emulator binary path or an actionable error.
func (m *SDKManager) Emulator() (string, error) {
	i := m.Info()
	if i.Emulator == "" {
		return "", sdkErr(i)
	}
	return i.Emulator, nil
}

// AVDManagerBin returns the avdmanager path or an actionable error.
func (m *SDKManager) AVDManagerBin() (string, error) {
	i := m.Info()
	if i.AVDManager == "" {
		return "", errCmdlineTools(i)
	}
	return i.AVDManager, nil
}

// SDKManagerBin returns the sdkmanager path or an actionable error.
func (m *SDKManager) SDKManagerBin() (string, error) {
	i := m.Info()
	if i.SDKManager == "" {
		return "", errCmdlineTools(i)
	}
	return i.SDKManager, nil
}

// AVDHome returns the directory holding <name>.ini / <name>.avd entries.
func (m *SDKManager) AVDHome() string { return m.Info().AVDHome }

// Root returns the resolved SDK root ("" when not found).
func (m *SDKManager) Root() string { return m.Info().SDKRoot }

func sdkErr(i AndroidSDKInfo) error {
	if i.Error != "" {
		return errors.New(i.Error)
	}
	return errors.New("Android emulator not found — install Android Studio or the SDK's emulator package, then point adbq at the SDK from Emulators → Host")
}

func errCmdlineTools(i AndroidSDKInfo) error {
	if i.SDKRoot == "" {
		return sdkErr(i)
	}
	return errors.New("Android SDK command-line tools not found under " + i.SDKRoot +
		" — install them from Android Studio (SDK Manager → SDK Tools → Android SDK Command-line Tools)")
}

// ─── probing ───────────────────────────────────────────────────────────────

func (m *SDKManager) probe() AndroidSDKInfo {
	var info AndroidSDKInfo
	info.SDKRoot, info.Source = m.resolveRoot()
	info.AVDHome = m.resolveAVDHome()

	if info.SDKRoot == "" {
		info.Error = "Android SDK not found — set ANDROID_HOME, install Android Studio, or choose the SDK folder from Emulators → Host"
	} else {
		info.Emulator = firstExisting(
			filepath.Join(info.SDKRoot, "emulator", exeName("emulator")),
			// Very old SDK layouts kept the emulator in tools/.
			filepath.Join(info.SDKRoot, "tools", exeName("emulator")),
		)
		info.AVDManager = firstExisting(cmdlineToolCandidates(info.SDKRoot, "avdmanager")...)
		info.SDKManager = firstExisting(cmdlineToolCandidates(info.SDKRoot, "sdkmanager")...)
		info.ADB = firstExisting(filepath.Join(info.SDKRoot, "platform-tools", exeName("adb")))
	}

	// PATH fallback: a Homebrew/apt install without a full SDK still gives us
	// working binaries, and lookTool already handles the Finder-launch PATH gap.
	if info.Emulator == "" {
		if p, ok := lookTool("emulator"); ok {
			info.Emulator = p
		}
	}
	if info.AVDManager == "" {
		if p, ok := lookTool("avdmanager"); ok {
			info.AVDManager = p
		}
	}
	if info.SDKManager == "" {
		if p, ok := lookTool("sdkmanager"); ok {
			info.SDKManager = p
		}
	}

	if info.Emulator != "" {
		info.EmulatorVer = parseEmulatorVersion(runQuick(info.Emulator, "-version"))
		info.Accelerated, info.AccelNote = parseAccelCheck(runQuick(info.Emulator, "-accel-check"))
		info.Error = ""
		info.Available = true
	} else if info.Error == "" {
		info.Error = "Android emulator binary not found under " + info.SDKRoot +
			" — install it from Android Studio (SDK Manager → SDK Tools → Android Emulator)"
	}

	info.StudioPath, info.StudioVer = detectAndroidStudio()
	return info
}

// resolveRoot applies the precedence: explicit user setting → ANDROID_HOME →
// ANDROID_SDK_ROOT → per-OS default location.
func (m *SDKManager) resolveRoot() (root, source string) {
	if s := strings.TrimSpace(m.host.Get().SDKRoot); s != "" && looksLikeSDK(s) {
		return s, "setting"
	}
	if s := strings.TrimSpace(os.Getenv("ANDROID_HOME")); s != "" && looksLikeSDK(s) {
		return s, "ANDROID_HOME"
	}
	if s := strings.TrimSpace(os.Getenv("ANDROID_SDK_ROOT")); s != "" && looksLikeSDK(s) {
		return s, "ANDROID_SDK_ROOT"
	}
	for _, c := range defaultSDKRoots() {
		if looksLikeSDK(c) {
			return c, "default"
		}
	}
	return "", ""
}

// resolveAVDHome mirrors the emulator's own lookup order. ANDROID_AVD_HOME wins;
// otherwise AVDs live under ~/.android/avd regardless of where the SDK is.
func (m *SDKManager) resolveAVDHome() string {
	if s := strings.TrimSpace(m.host.Get().AVDHome); s != "" {
		return s
	}
	if s := strings.TrimSpace(os.Getenv("ANDROID_AVD_HOME")); s != "" {
		return s
	}
	// ANDROID_SDK_HOME / ANDROID_EMULATOR_HOME relocate the whole .android dir.
	for _, env := range []string{"ANDROID_EMULATOR_HOME", "ANDROID_SDK_HOME"} {
		if s := strings.TrimSpace(os.Getenv(env)); s != "" {
			if d := filepath.Join(s, "avd"); dirExists(d) {
				return d
			}
			if d := filepath.Join(s, ".android", "avd"); dirExists(d) {
				return d
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".android", "avd")
}

// defaultSDKRoots lists the per-OS locations Android Studio installs into.
func defaultSDKRoots() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		if home == "" {
			return nil
		}
		return []string{filepath.Join(home, "Library", "Android", "sdk")}
	case "windows":
		var out []string
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			out = append(out, filepath.Join(la, "Android", "Sdk"))
		}
		if home != "" {
			out = append(out, filepath.Join(home, "AppData", "Local", "Android", "Sdk"))
		}
		return out
	default:
		if home == "" {
			return nil
		}
		return []string{
			filepath.Join(home, "Android", "Sdk"),
			filepath.Join(home, "Android", "sdk"),
			"/usr/lib/android-sdk",
			"/opt/android-sdk",
		}
	}
}

// looksLikeSDK guards against a stale env var pointing at a deleted or wrong
// directory: a real SDK root always has at least one of these subdirectories.
func looksLikeSDK(root string) bool {
	if root == "" || !dirExists(root) {
		return false
	}
	for _, sub := range []string{"platform-tools", "emulator", "cmdline-tools", "system-images", "platforms"} {
		if dirExists(filepath.Join(root, sub)) {
			return true
		}
	}
	return false
}

// cmdlineToolCandidates lists where avdmanager/sdkmanager may live. Google has
// moved them twice: tools/bin → cmdline-tools/<ver>/bin, with "latest" the
// conventional current version.
func cmdlineToolCandidates(root, name string) []string {
	bin := exeName(name)
	out := []string{
		filepath.Join(root, "cmdline-tools", "latest", "bin", bin),
	}
	if entries, err := os.ReadDir(filepath.Join(root, "cmdline-tools")); err == nil {
		for _, e := range entries {
			if e.IsDir() && e.Name() != "latest" {
				out = append(out, filepath.Join(root, "cmdline-tools", e.Name(), "bin", bin))
			}
		}
	}
	out = append(out, filepath.Join(root, "tools", "bin", bin))
	return out
}

// exeName appends the Windows script/executable suffix. avdmanager and
// sdkmanager ship as .bat on Windows; emulator and adb as .exe.
func exeName(name string) string {
	if runtime.GOOS != "windows" {
		return name
	}
	switch name {
	case "avdmanager", "sdkmanager":
		return name + ".bat"
	default:
		return name + ".exe"
	}
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// runQuick executes a short-lived probe command, tolerating failure: these are
// all "tell me about yourself" calls where partial output still informs.
func runQuick(bin string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	return string(out)
}

// ─── pure parsers (unit-tested) ────────────────────────────────────────────

// parseEmulatorVersion extracts "36.3.10.0" from the first line of
// `emulator -version`, e.g. "Android emulator version 36.3.10.0 (build_id …)".
func parseEmulatorVersion(out string) string {
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		low := strings.ToLower(ln)
		if !strings.Contains(low, "emulator version") {
			continue
		}
		fields := strings.Fields(ln)
		for i, f := range fields {
			if strings.EqualFold(f, "version") && i+1 < len(fields) {
				return strings.Trim(fields[i+1], "()")
			}
		}
	}
	return ""
}

// parseAccelCheck reads `emulator -accel-check`. The exit status is unreliable
// across versions, so we go by the reported accel value: "accel:\n0\n<desc>"
// means acceleration is available, any non-zero code means it is not.
func parseAccelCheck(out string) (ok bool, note string) {
	lines := make([]string, 0, 4)
	for _, ln := range strings.Split(out, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			lines = append(lines, t)
		}
	}
	for i, ln := range lines {
		if !strings.EqualFold(strings.TrimSuffix(ln, ":"), "accel") {
			continue
		}
		if i+1 >= len(lines) {
			break
		}
		code := lines[i+1]
		if i+2 < len(lines) {
			note = lines[i+2]
		}
		return code == "0", note
	}
	if len(lines) > 0 {
		return false, lines[len(lines)-1]
	}
	return false, ""
}

// ─── Android Studio ────────────────────────────────────────────────────────

// detectAndroidStudio locates the IDE itself. adbq never drives Studio; we only
// report it (it tells the user where their SDK came from) and offer to open it
// for the few things adbq deliberately doesn't do.
func detectAndroidStudio() (path, version string) {
	for _, c := range studioCandidates() {
		if _, err := os.Stat(c); err == nil {
			return c, studioVersion(c)
		}
	}
	return "", ""
}

func studioCandidates() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		out := []string{"/Applications/Android Studio.app"}
		out = append(out, globDirs("/Applications/Android Studio*.app")...)
		if home != "" {
			out = append(out, filepath.Join(home, "Applications", "Android Studio.app"))
			out = append(out, globDirs(filepath.Join(home, "Applications", "Android Studio*.app"))...)
		}
		return out
	case "windows":
		var out []string
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
			if v := os.Getenv(env); v != "" {
				out = append(out, filepath.Join(v, "Android", "Android Studio", "bin", "studio64.exe"))
			}
		}
		return out
	default:
		out := []string{"/opt/android-studio/bin/studio.sh", "/usr/local/android-studio/bin/studio.sh"}
		if home != "" {
			out = append(out, globDirs(filepath.Join(home, ".local", "share", "JetBrains", "Toolbox", "apps", "*", "*", "*", "bin", "studio.sh"))...)
			out = append(out, filepath.Join(home, "android-studio", "bin", "studio.sh"))
		}
		return out
	}
}

func globDirs(pattern string) []string {
	m, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return m
}

// studioVersion reads the IDE version without launching it. JetBrains ships a
// product-info.json next to the app on every platform.
func studioVersion(path string) string {
	var candidates []string
	if strings.HasSuffix(path, ".app") {
		candidates = append(candidates, filepath.Join(path, "Contents", "Resources", "product-info.json"))
	} else {
		// <install>/bin/studio.sh → <install>/product-info.json
		base := filepath.Dir(filepath.Dir(path))
		candidates = append(candidates, filepath.Join(base, "product-info.json"))
	}
	for _, c := range candidates {
		b, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		return studioDisplayVersion(
			jsonStringField(string(b), "dataDirectoryName"),
			jsonStringField(string(b), "version"),
		)
	}
	return ""
}

// studioDisplayVersion prefers the marketing version ("2025.2.2") that
// dataDirectoryName encodes over the raw build string ("AI-252.27397.103…"),
// which means nothing to a user comparing against the download page.
func studioDisplayVersion(dataDirName, version string) string {
	if v := strings.TrimPrefix(dataDirName, "AndroidStudio"); v != dataDirName && v != "" {
		return v
	}
	return version
}

// jsonStringField pulls one top-level string field out of a JSON document
// without binding the whole (large, version-dependent) product-info schema.
func jsonStringField(doc, key string) string {
	needle := `"` + key + `"`
	i := strings.Index(doc, needle)
	if i < 0 {
		return ""
	}
	rest := doc[i+len(needle):]
	c := strings.Index(rest, ":")
	if c < 0 {
		return ""
	}
	rest = strings.TrimSpace(rest[c+1:])
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	e := strings.Index(rest, `"`)
	if e < 0 {
		return ""
	}
	return rest[:e]
}
