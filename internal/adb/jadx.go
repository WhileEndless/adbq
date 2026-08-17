package adb

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// jadx is a third-party decompiler adbq drives but never vendors: it is not a
// Go or npm dependency, it is a tool the user downloads — like frida-server and
// rootAVD. Its licence (Apache-2.0), activity and adoption all clear
// CLAUDE.md §1.1; the download is what needs discipline, not the licence.
//
// The version is pinned in code together with the archive's SHA-256, so the
// default path fetches exactly one known artifact. Moving off the pin is a
// deliberate, manual act: the user either points adbq at their own installation
// or asks it to check for a newer release, in which case the new version and its
// digest are shown before anything is fetched. adbq never upgrades itself.
const (
	jadxVersion = "1.5.6"
	jadxProject = "https://github.com/skylot/jadx"
	jadxAsset   = "https://github.com/skylot/jadx/releases/download/v" + jadxVersion + "/jadx-" + jadxVersion + ".zip"
	jadxSHA     = "545ea2be9c242511bc145755cf4bda2485ade42966e096f8b4d3da2a230e8974"
	jadxLicense = "Apache-2.0"

	// The release archive ships every dependency, so it is large by nature;
	// these caps only stop an absurd or hostile one from filling the disk.
	jadxMaxArchive = 512 << 20
	jadxMaxEntry   = 256 << 20

	// jadxMinJava is the runtime jadx itself requires.
	jadxMinJava = 11

	jadxReleaseAPI = "https://api.github.com/repos/skylot/jadx/releases/latest"
)

// jadxHosts is the complete set of origins a jadx download may come from.
var jadxHosts = []string{
	"https://github.com/",
	"https://objects.githubusercontent.com/",
}

// JadxInfo describes the tool, where it came from and whether it can run right
// now. Disclosures are written here rather than in the UI so the consent dialog
// cannot quietly drop one.
type JadxInfo struct {
	Installed bool   `json:"installed"`
	Kind      string `json:"kind"` // "managed" | "external" | ""
	Bin       string `json:"bin"`
	Dir       string `json:"dir"`
	Version   string `json:"version"`

	PinnedVersion string `json:"pinnedVersion"`
	Source        string `json:"source"`
	Asset         string `json:"asset"`
	License       string `json:"license"`
	SHA256        string `json:"sha256"`

	Java        string `json:"java"`
	JavaVersion string `json:"javaVersion"`
	JavaSource  string `json:"javaSource"` // "path" | "JAVA_HOME" | "java_home" | "studio" | "user"
	JavaError   string `json:"javaError"`

	Ready       bool     `json:"ready"` // tool and a usable Java both resolved
	Disclosures []string `json:"disclosures"`
}

// JadxRelease is a newer release the user may choose to install by hand. The
// digest comes from GitHub's release metadata, which is what makes an
// unpinned download verifiable at all.
type JadxRelease struct {
	Version   string `json:"version"`
	Asset     string `json:"asset"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Published string `json:"published"`
	Newer     bool   `json:"newer"` // newer than what is installed
}

// jadxRoot is the parent of the per-version managed installs. Disposable: the
// user can delete it and adbq re-fetches on demand.
func jadxRoot() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "adbq", "jadx")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// jadxLauncher returns the GUI launcher inside an unpacked release, or "" when
// there is none. Existence is checked rather than assumed, so an archive whose
// layout changed reads as "not installed" instead of yielding a path that fails
// at exec time.
func jadxLauncher(dir string) string {
	names := []string{"jadx-gui"}
	if runtime.GOOS == "windows" {
		names = []string{"jadx-gui.bat", "jadx-gui.exe"}
	}
	for _, n := range names {
		p := filepath.Join(dir, "bin", n)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// managedJadx returns the launcher of the best managed install: the pinned
// version when present, otherwise the highest version the user installed by
// hand through the update path.
func managedJadx() (bin, version, dir string) {
	root, err := jadxRoot()
	if err != nil {
		return "", "", ""
	}
	if b := jadxLauncher(filepath.Join(root, jadxVersion)); b != "" {
		return b, jadxVersion, filepath.Join(root, jadxVersion)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", "", ""
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() {
			versions = append(versions, e.Name())
		}
	}
	sort.Slice(versions, func(i, j int) bool { return compareVersions(versions[i], versions[j]) > 0 })
	for _, v := range versions {
		d := filepath.Join(root, v)
		if b := jadxLauncher(d); b != "" {
			return b, v, d
		}
	}
	return "", "", ""
}

// JadxStatus resolves the tool and the Java it would run under.
//
// A path the user set wins over everything: it is the escape hatch for a
// manually updated or distro-packaged jadx, and adbq must not second-guess it.
func JadxStatus(hs HostSettings, studioPath string) JadxInfo {
	info := JadxInfo{
		PinnedVersion: jadxVersion,
		Source:        jadxProject,
		Asset:         jadxAsset,
		License:       jadxLicense,
		SHA256:        jadxSHA,
	}
	info.Disclosures = []string{
		"jadx is a third-party decompiler (" + jadxLicense + "), not part of adbq. Version " + jadxVersion + " is downloaded from " + jadxProject + " and verified against a SHA-256 recorded in adbq before anything runs.",
		"It needs a Java " + strconv.Itoa(jadxMinJava) + "+ runtime. adbq looks for one already on this computer and never installs one.",
		"Decompilation happens entirely on this computer; nothing is uploaded anywhere.",
		"The download lives in adbq's cache directory and can be removed at any time — adbq re-fetches it on demand.",
		"adbq stays pinned to " + jadxVersion + ". Newer releases are only installed when you ask for them, and their version and digest are shown first.",
	}

	if p := strings.TrimSpace(hs.JadxPath); p != "" {
		if bin := resolveJadxUserPath(p); bin != "" {
			info.Installed, info.Kind, info.Bin = true, "external", bin
			info.Dir = filepath.Dir(filepath.Dir(bin))
			info.Version = jadxVersionOf(bin)
		}
	}
	if !info.Installed {
		if bin, ver, dir := managedJadx(); bin != "" {
			info.Installed, info.Kind, info.Bin, info.Version, info.Dir = true, "managed", bin, ver, dir
		}
	}
	if !info.Installed {
		name := "jadx-gui"
		if runtime.GOOS == "windows" {
			name = "jadx-gui.bat"
		}
		if bin, ok := lookTool(name); ok {
			info.Installed, info.Kind, info.Bin = true, "external", bin
			info.Version = jadxVersionOf(bin)
		}
	}

	java, ver, src, err := resolveJava(hs.JavaPath, studioPath)
	info.Java, info.JavaVersion, info.JavaSource = java, ver, src
	if err != nil {
		info.JavaError = err.Error()
	}
	info.Ready = info.Installed && info.Java != ""
	return info
}

// resolveJadxUserPath accepts whatever the user pointed at — the launcher, the
// bin directory, or the unpacked release root — and returns the launcher.
func resolveJadxUserPath(p string) string {
	fi, err := os.Stat(p)
	if err != nil {
		return ""
	}
	if !fi.IsDir() {
		return p
	}
	if b := jadxLauncher(p); b != "" {
		return b
	}
	// The user may have picked the bin directory itself.
	if b := jadxLauncher(filepath.Dir(p)); b != "" {
		return b
	}
	return ""
}

// jadxVersionOf reads the version out of an install's directory layout. An
// external install may not reveal one, and that is not an error — the version is
// shown to the user, not depended on.
func jadxVersionOf(bin string) string {
	dir := filepath.Dir(filepath.Dir(bin))
	base := filepath.Base(dir)
	if base != "" && base[0] >= '0' && base[0] <= '9' {
		return base
	}
	if _, rest, ok := strings.Cut(base, "jadx-"); ok && rest != "" {
		return rest
	}
	return ""
}

// ─── Java ──────────────────────────────────────────────────────────────────

// resolveJava finds a Java runtime jadx can use, in order of how much the user
// has said about it: an explicit setting, then the places a real JDK actually
// installs itself, then the JDK Android Studio ships — which is the one
// machine-wide Java many Android developers have.
//
// Only a runtime that lives inside a Java home is accepted. That rules out the
// macOS stub at /usr/bin/java, which is not a runtime but a shim that asks the
// user to install one: it answers `-version` when a JDK happens to be
// registered, so probing alone accepts it, and then hangs as a child of a GUI
// app with no console instead of failing. It also has no usable home — deriving
// one gives `/usr`, which the launcher would pass on as JAVA_HOME.
func resolveJava(userPath, studioPath string) (bin, version, source string, err error) {
	type cand struct{ path, source string }
	var cands []cand
	if p := strings.TrimSpace(userPath); p != "" {
		cands = append(cands, cand{javaBinOf(p), "user"})
	}
	if h := os.Getenv("JAVA_HOME"); h != "" {
		cands = append(cands, cand{filepath.Join(h, "bin", javaExe()), "JAVA_HOME"})
	}
	if runtime.GOOS == "darwin" {
		if p := macJavaHome(); p != "" {
			cands = append(cands, cand{filepath.Join(p, "bin", "java"), "java_home"})
		}
	}
	for _, p := range studioJavaCandidates(studioPath) {
		cands = append(cands, cand{p, "studio"})
	}
	for _, p := range installedJavaCandidates() {
		cands = append(cands, cand{p, "installed"})
	}
	// PATH comes last: on macOS it is where the stub lives, and by this point a
	// real JDK has had every chance to be found. It still matters on systems
	// where Java is only on PATH.
	if p, ok := lookTool(javaExe()); ok {
		cands = append(cands, cand{p, "path"})
	}

	var tooOld, stubbed string
	for _, c := range cands {
		if c.path == "" {
			continue
		}
		if fi, statErr := os.Stat(c.path); statErr != nil || fi.IsDir() {
			continue
		}
		if !isJavaHome(javaHomeOf(c.path)) {
			// Almost always the macOS stub. Remember it so the error can say so.
			stubbed = c.path
			continue
		}
		v, major := javaVersion(c.path)
		if major <= 0 {
			continue
		}
		if major < jadxMinJava {
			if tooOld == "" {
				tooOld = v
			}
			continue
		}
		return c.path, v, c.source, nil
	}
	switch {
	case tooOld != "":
		return "", "", "", fmt.Errorf("the Java on this computer is %s; jadx needs %d or newer — install a newer JDK or set the Java path in Settings", tooOld, jadxMinJava)
	case stubbed != "":
		return "", "", "", fmt.Errorf("%s is a stub, not a Java runtime, and no real JDK %d+ was found — install one (Android Studio ships one) or set the Java path in Settings", stubbed, jadxMinJava)
	}
	return "", "", "", fmt.Errorf("no Java %d+ runtime found — install one (Android Studio ships one) or set the Java path in Settings", jadxMinJava)
}

// isJavaHome reports whether dir looks like a Java home rather than a system
// prefix that happens to contain a java executable. A home always ships the
// runtime's own files next to bin/.
func isJavaHome(dir string) bool {
	if dir == "" || dir == string(filepath.Separator) {
		return false
	}
	if fi, err := os.Stat(filepath.Join(dir, "bin", javaExe())); err != nil || fi.IsDir() {
		return false
	}
	// `release` is present in every JDK/JRE since 9; `lib/modules` and the older
	// `jre/lib/rt.jar` cover the rest.
	//
	// Each marker must be a regular FILE. In a JDK, `lib/modules` is the runtime
	// image — one file — but on Linux `/usr/lib/modules` is the kernel's module
	// directory, and `/usr/bin/java` exists on any machine with Java installed.
	// Accepting a directory there made `/usr` look like a Java home, which is the
	// same mistake as trusting the macOS stub: JAVA_HOME=/usr launches something
	// that is not a runtime.
	for _, marker := range []string{"release", filepath.Join("lib", "modules"), filepath.Join("jre", "lib", "rt.jar")} {
		if fi, err := os.Stat(filepath.Join(dir, marker)); err == nil && fi.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// installedJavaCandidates lists where a real JDK puts itself on each platform.
// These are generic locations, not this machine's: whoever installs adbq gets
// the same search.
func installedJavaCandidates() []string {
	var homes []string
	switch runtime.GOOS {
	case "darwin":
		homes = append(homes, globDirs("/Library/Java/JavaVirtualMachines/*/Contents/Home")...)
		if home, err := os.UserHomeDir(); err == nil {
			homes = append(homes, globDirs(filepath.Join(home, "Library", "Java", "JavaVirtualMachines", "*", "Contents", "Home"))...)
		}
		// Homebrew keeps its JDKs under the prefix rather than registering them.
		for _, prefix := range []string{"/opt/homebrew/opt", "/usr/local/opt"} {
			homes = append(homes, globDirs(prefix+"/openjdk*/libexec/openjdk.jdk/Contents/Home")...)
			homes = append(homes, globDirs(prefix+"/openjdk*")...)
		}
	case "windows":
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
			if v := os.Getenv(env); v != "" {
				for _, vendor := range []string{"Java", "Eclipse Adoptium", "Microsoft", "Zulu", "Amazon Corretto"} {
					homes = append(homes, globDirs(filepath.Join(v, vendor, "*"))...)
				}
			}
		}
	default:
		homes = append(homes, globDirs("/usr/lib/jvm/*")...)
		homes = append(homes, globDirs("/usr/lib64/jvm/*")...)
		homes = append(homes, globDirs("/opt/java/*")...)
	}
	// Newest first, so a machine with several JDKs does not get pinned to the
	// oldest one by directory order.
	sort.Slice(homes, func(i, j int) bool { return homes[i] > homes[j] })
	out := make([]string, 0, len(homes))
	for _, h := range homes {
		out = append(out, filepath.Join(h, "bin", javaExe()))
	}
	return out
}

func javaExe() string {
	if runtime.GOOS == "windows" {
		return "java.exe"
	}
	return "java"
}

// javaBinOf accepts either a java executable or a JAVA_HOME and returns the
// executable, so the settings field forgives whichever the user picks.
func javaBinOf(p string) string {
	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		return filepath.Join(p, "bin", javaExe())
	}
	return p
}

// javaHomeOf is the inverse: <home>/bin/java → <home>.
func javaHomeOf(bin string) string {
	if bin == "" {
		return ""
	}
	return filepath.Dir(filepath.Dir(bin))
}

// macJavaHome asks macOS which JDK it considers current. Absent any JDK the
// helper exists but fails, which is why its output is checked rather than used.
func macJavaHome() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/libexec/java_home", "-v", strconv.Itoa(jadxMinJava)+"+").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// studioJavaCandidates maps an Android Studio location to the JDK bundled
// inside it. Pure, because the layout differs per platform and is worth testing
// without a Studio install present.
func studioJavaCandidates(studioPath string) []string {
	studioPath = strings.TrimSpace(studioPath)
	if studioPath == "" {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		// The picked path is the .app bundle.
		return []string{filepath.Join(studioPath, "Contents", "jbr", "Contents", "Home", "bin", "java")}
	case "windows":
		// …/Android Studio/bin/studio64.exe → …/Android Studio/jbr/bin/java.exe
		root := filepath.Dir(filepath.Dir(studioPath))
		return []string{filepath.Join(root, "jbr", "bin", "java.exe")}
	default:
		// …/android-studio/bin/studio.sh → …/android-studio/jbr/bin/java
		root := filepath.Dir(filepath.Dir(studioPath))
		return []string{
			filepath.Join(root, "jbr", "bin", "java"),
			filepath.Join(root, "jre", "bin", "java"),
		}
	}
}

// javaVersion runs the interpreter and reports its version string and major
// number. `java -version` writes to stderr, which is why output is combined.
func javaVersion(bin string) (string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-version").CombinedOutput()
	if err != nil && len(out) == 0 {
		return "", 0
	}
	return parseJavaVersion(string(out))
}

// parseJavaVersion extracts the version from `java -version` output. Both
// shapes matter: 9+ reports "17.0.9", while 8 and earlier report "1.8.0_301"
// and must come out as 8 so they can be rejected.
func parseJavaVersion(out string) (string, int) {
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if !strings.Contains(ln, "version") {
			continue
		}
		_, rest, ok := strings.Cut(ln, `"`)
		if !ok {
			continue
		}
		v, _, ok := strings.Cut(rest, `"`)
		if !ok {
			continue
		}
		parts := strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '_' || r == '-' || r == '+' })
		if len(parts) == 0 {
			continue
		}
		major, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		if major == 1 && len(parts) > 1 {
			if second, err := strconv.Atoi(parts[1]); err == nil {
				major = second
			}
		}
		return v, major
	}
	return "", 0
}

// ─── install ───────────────────────────────────────────────────────────────

// InstallJadx downloads and verifies the pinned release. The caller is
// responsible for having shown JadxInfo.Disclosures and obtained consent — this
// function fetches, it does not ask.
func InstallJadx(ctx context.Context, onStage func(string)) error {
	return installJadxArchive(ctx, JadxRelease{Version: jadxVersion, Asset: jadxAsset, SHA256: jadxSHA}, onStage)
}

// InstallJadxRelease installs a release the user chose from LatestJadxRelease.
// The digest is GitHub's, not adbq's, so it is required: a download nobody can
// check is not installed.
func InstallJadxRelease(ctx context.Context, rel JadxRelease, onStage func(string)) error {
	if rel.SHA256 == "" {
		return fmt.Errorf("that release publishes no checksum for its archive, so adbq cannot verify the download — install jadx yourself and point adbq at it instead")
	}
	if sanitizePathSegment(rel.Version) != rel.Version || rel.Version == "" {
		return fmt.Errorf("unexpected jadx version %q", rel.Version)
	}
	return installJadxArchive(ctx, rel, onStage)
}

func installJadxArchive(ctx context.Context, rel JadxRelease, onStage func(string)) error {
	note := func(s string) {
		if onStage != nil {
			onStage(s)
		}
	}
	root, err := jadxRoot()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, rel.Version)
	if jadxLauncher(dir) != "" {
		return nil
	}

	note("downloading jadx " + rel.Version)
	archive := filepath.Join(root, "jadx-"+rel.Version+".zip")
	if err := downloadVerifiedAsset(ctx, rel.Asset, rel.SHA256, archive, jadxHosts); err != nil {
		return fmt.Errorf("could not download jadx: %w", err)
	}
	defer os.Remove(archive)

	note("extracting")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := extractJadx(archive, dir); err != nil {
		// A tree we cannot vouch for must not be left where a later run picks
		// it up.
		_ = os.RemoveAll(dir)
		return err
	}
	note("verifying")
	if jadxLauncher(dir) == "" {
		_ = os.RemoveAll(dir)
		return fmt.Errorf("the jadx archive did not contain the expected bin/jadx-gui launcher — the download has been discarded")
	}
	return nil
}

// RemoveJadx deletes every managed install.
func RemoveJadx() error {
	base, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(base, "adbq", "jadx"))
}

// extractJadx unpacks the release, stripping the single `jadx-<version>/`
// wrapper when the archive has one. Entry paths are checked against traversal
// and the total size is bounded; a single bad entry rejects the whole archive.
func extractJadx(src, dst string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("jadx archive is not a readable zip: %w", err)
	}
	defer zr.Close()

	strip := zipCommonPrefix(zr)
	var total int64
	for _, e := range zr.File {
		rel := strings.TrimPrefix(filepath.ToSlash(e.Name), strip)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			continue
		}
		target, ok := safeJoin(dst, rel)
		if !ok {
			return fmt.Errorf("jadx archive contains an unsafe path: %s", e.Name)
		}
		if e.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if !e.Mode().IsRegular() {
			// Symlinks and device nodes have no business in this tree.
			continue
		}
		if int64(e.UncompressedSize64) > jadxMaxEntry {
			return fmt.Errorf("jadx archive entry %s is implausibly large", e.Name)
		}
		total += int64(e.UncompressedSize64)
		if total > jadxMaxArchive {
			return fmt.Errorf("jadx archive is larger than expected (%d bytes) and has been rejected", total)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		// The launchers must stay executable; the zip records that and so does
		// the directory they live in.
		if e.Mode()&0o111 != 0 || strings.HasPrefix(rel, "bin/") {
			mode = 0o755
		}
		if err := writeZipEntry(e, target, mode); err != nil {
			return err
		}
	}
	return nil
}

func writeZipEntry(e *zip.File, target string, mode os.FileMode) error {
	rc, err := e.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(rc, jadxMaxEntry)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// zipCommonPrefix returns the single top-level directory every entry sits under,
// or "" when the archive has more than one root. Release archives wrap their
// contents in `jadx-<version>/`, and staging that as a subdirectory would put
// the launcher one level deeper than every later lookup expects.
func zipCommonPrefix(zr *zip.ReadCloser) string {
	prefix := ""
	for _, e := range zr.File {
		name := strings.TrimPrefix(filepath.ToSlash(e.Name), "./")
		if name == "" {
			continue
		}
		root, _, nested := strings.Cut(name, "/")
		if !nested && !e.FileInfo().IsDir() {
			return "" // a file at the archive root: no wrapper
		}
		if prefix == "" {
			prefix = root
			continue
		}
		if prefix != root {
			return ""
		}
	}
	return prefix
}

// ─── manual update ─────────────────────────────────────────────────────────

// LatestJadxRelease reports the newest published release and the digest GitHub
// records for its archive. Nothing is downloaded; this only produces what the
// consent dialog needs to describe an update honestly.
func LatestJadxRelease(ctx context.Context) (JadxRelease, error) {
	rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, "GET", jadxReleaseAPI, nil)
	if err != nil {
		return JadxRelease{}, err
	}
	req.Header.Set("User-Agent", "adbq")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return JadxRelease{}, fmt.Errorf("could not reach the jadx release feed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return JadxRelease{}, fmt.Errorf("jadx release feed returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		TagName     string `json:"tag_name"`
		PublishedAt string `json:"published_at"`
		Assets      []struct {
			Name   string `json:"name"`
			Size   int64  `json:"size"`
			Digest string `json:"digest"`
			URL    string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return JadxRelease{}, fmt.Errorf("could not read the jadx release feed: %w", err)
	}
	return jadxReleaseFromFeed(payload.TagName, payload.PublishedAt, func() []jadxFeedAsset {
		out := make([]jadxFeedAsset, 0, len(payload.Assets))
		for _, a := range payload.Assets {
			out = append(out, jadxFeedAsset{Name: a.Name, Size: a.Size, Digest: a.Digest, URL: a.URL})
		}
		return out
	}())
}

type jadxFeedAsset struct {
	Name   string
	Size   int64
	Digest string
	URL    string
}

// jadxReleaseFromFeed is the pure half of LatestJadxRelease: it picks the
// cross-platform archive and normalises the digest, so both the choice and the
// "no verifiable asset" case are unit-testable.
func jadxReleaseFromFeed(tag, published string, assets []jadxFeedAsset) (JadxRelease, error) {
	version := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	if version == "" {
		return JadxRelease{}, fmt.Errorf("the jadx release feed reported no version")
	}
	want := "jadx-" + version + ".zip"
	for _, a := range assets {
		if !strings.EqualFold(a.Name, want) {
			continue
		}
		rel := JadxRelease{
			Version:   version,
			Asset:     a.URL,
			Size:      a.Size,
			Published: published,
			SHA256:    strings.TrimPrefix(strings.ToLower(strings.TrimSpace(a.Digest)), "sha256:"),
			Newer:     compareVersions(version, jadxVersion) > 0,
		}
		if strings.Contains(rel.SHA256, ":") {
			// A digest in some other algorithm is not one we can check.
			rel.SHA256 = ""
		}
		return rel, nil
	}
	return JadxRelease{}, fmt.Errorf("release %s publishes no %s archive", version, want)
}
