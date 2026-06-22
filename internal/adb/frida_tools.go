package adb

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Host-side Frida runtime management.
//
// To actually drive instrumentation we need the `frida` Python package on the
// HOST, and its version must match the frida-server running on the device. Two
// interchangeable modes, both keyed by frida version:
//
//   - managed venv  — adbq creates ~/…/venvs/<ver> and installs the single
//                     host-matching wheel it downloaded from PyPI and verified by
//                     SHA256 (pip --no-index --no-deps --only-binary=:all:, so pip
//                     never resolves over the network and never builds an sdist).
//   - external      — the user installs frida themselves and registers an
//                     interpreter/venv path; adbq installs nothing and only reads
//                     the frida/Python versions from it.
//
// frida ships abi3 (cp37) wheels — one per platform, valid on any CPython ≥3.7 —
// so wheel selection is purely OS+arch; the Python minor version is irrelevant.

const pypiBase = "https://pypi.org/pypi"
const pythonHostedHost = "https://files.pythonhosted.org/"

// FridaRuntime is a host interpreter able to drive Frida.
type FridaRuntime struct {
	ID            string `json:"id"`   // "managed:<ver>" | "ext:<n>"
	Kind          string `json:"kind"` // "managed" | "external"
	Label         string `json:"label"`
	PythonPath    string `json:"pythonPath"` // interpreter that runs the driver
	FridaVersion  string `json:"fridaVersion"`
	PythonVersion string `json:"pythonVersion"`
	AddedAt       int64  `json:"addedAt,omitempty"`
}

// FridaHostInfo describes the host Python the Runtime UI can build venvs with.
type FridaHostInfo struct {
	Available     bool   `json:"available"`
	PythonPath    string `json:"pythonPath"`
	PythonVersion string `json:"pythonVersion"`
	HasVenv       bool   `json:"hasVenv"`
	Error         string `json:"error"`
}

// ─── Host Python discovery ─────────────────────────────────────────────────

// DetectHostPython finds a Python 3.7+ on the host (PATH plus the common
// install dirs lookTool covers, since a Finder-launched app has a minimal PATH).
func DetectHostPython() (path, version string, err error) {
	// An explicit override wins, so a user with several Pythons can pin the one
	// adbq builds venvs with (e.g. a 3.11+ interpreter).
	var candidates []string
	if env := strings.TrimSpace(os.Getenv("ADBQ_FRIDA_PYTHON")); env != "" {
		candidates = append(candidates, env)
	}
	candidates = append(candidates, "python3", "python")

	var sawOld string
	for _, name := range candidates {
		p, ok := lookTool(name)
		if !ok {
			if fileExists(name) { // an absolute override path lookTool didn't resolve
				p, ok = name, true
			} else {
				continue
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		out, _, e := runHostStdout(ctx, p, "-c", "import sys;print('%d.%d.%d'%sys.version_info[:3])")
		cancel()
		if e != nil {
			continue
		}
		v := strings.TrimSpace(firstLine(out))
		if !pythonAtLeast(v, 3, 7) {
			sawOld = v + " (" + p + ")"
			continue
		}
		return p, v, nil
	}
	if sawOld != "" {
		return "", "", fmt.Errorf("found Python %s but Frida needs Python 3.7+ — install a newer Python 3", sawOld)
	}
	return "", "", fmt.Errorf("no Python 3 found — install Python 3.7+ (e.g. `brew install python` on macOS, python.org, or your package manager) to use Frida tooling")
}

// DetectFridaHost reports host-Python availability for the Runtime UI.
func DetectFridaHost() FridaHostInfo {
	py, ver, err := DetectHostPython()
	if err != nil {
		return FridaHostInfo{Error: err.Error()}
	}
	info := FridaHostInfo{Available: true, PythonPath: py, PythonVersion: ver, HasVenv: hostHasVenv(py)}
	if !info.HasVenv {
		info.Error = "Python found, but its venv/ensurepip modules are missing — install python3-venv (Debian/Ubuntu) or use an external interpreter"
	}
	return info
}

func hostHasVenv(py string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _, err := runHostStdout(ctx, py, "-c", "import venv, ensurepip")
	return err == nil
}

// detectFridaInfo reads the frida and Python versions from an interpreter (or a
// venv folder). Used to validate an external interpreter at registration.
func detectFridaInfo(pythonPath string) (fridaVer, pyVer string, err error) {
	py := resolveInterpreter(pythonPath)
	if py == "" {
		return "", "", fmt.Errorf("empty interpreter path")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	out, errOut, e := runHostStdout(ctx, py, "-c", "import frida,sys;print(frida.__version__);print('%d.%d.%d'%sys.version_info[:3])")
	if e != nil {
		detail := strings.TrimSpace(firstLine(errOut))
		if detail == "" {
			detail = e.Error()
		}
		return "", "", fmt.Errorf("could not import frida with %s: %s", py, detail)
	}
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(out, "\r\n", "\n")), "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) == "" {
		return "", "", fmt.Errorf("unexpected interpreter output: %q", strings.TrimSpace(out))
	}
	return strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]), nil
}

// venvFridaVersion returns the frida version importable from a venv's
// interpreter, or "" if the venv is absent/broken/missing frida.
func venvFridaVersion(venvPy string) string {
	v, _ := venvFridaProbe(venvPy)
	return v
}

// venvFridaProbe imports frida in the interpreter and returns its version. On
// failure it returns the interpreter's stderr (e.g. a ModuleNotFoundError for a
// missing dependency) so callers can surface an actionable message instead of an
// empty string. A version token in stdout wins even if the process exits non-zero
// (some frida builds print the version then crash in native cleanup at exit).
func venvFridaProbe(venvPy string) (string, error) {
	if !fileExists(venvPy) {
		return "", fmt.Errorf("interpreter not found: %s", venvPy)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	out, errOut, err := runHostStdout(ctx, venvPy, "-c", "import frida;print(frida.__version__)")
	if v := parseVersionToken(out); v != "" {
		return v, nil
	}
	if detail := strings.TrimSpace(firstLine(errOut)); detail != "" {
		return "", fmt.Errorf("%s", detail)
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("no version reported")
}

// hostPythonTarget reports the (GOOS, GOARCH) of a Python interpreter so wheel
// selection matches the interpreter that will import the wheel, not adbq's own
// process. Falls back to the build's GOOS/GOARCH if the probe fails.
func hostPythonTarget(py string) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, _, err := runHostStdout(ctx, py, "-c", "import sys,platform;print(sys.platform);print(platform.machine())")
	if err != nil {
		return runtime.GOOS, runtime.GOARCH
	}
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(out, "\r\n", "\n")), "\n")
	if len(lines) < 2 {
		return runtime.GOOS, runtime.GOARCH
	}
	goos := map[string]string{"darwin": "darwin", "linux": "linux", "win32": "windows", "cygwin": "windows"}[strings.TrimSpace(lines[0])]
	goarch := map[string]string{
		"arm64": "arm64", "aarch64": "arm64",
		"x86_64": "amd64", "amd64": "amd64", "AMD64": "amd64",
		"armv7l": "arm", "armv8l": "arm",
		"i686": "386", "i386": "386", "x86": "386",
	}[strings.TrimSpace(lines[1])]
	if goos == "" || goarch == "" {
		return runtime.GOOS, runtime.GOARCH
	}
	return goos, goarch
}

// ─── PyPI wheel resolution ─────────────────────────────────────────────────

// pypiFile is one downloadable artifact for a frida release.
type pypiFile struct {
	Filename    string
	URL         string
	SHA256      string
	PackageType string // "bdist_wheel" | "sdist"
	PythonTag   string
}

type pypiResponse struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
	URLs []struct {
		Filename    string `json:"filename"`
		URL         string `json:"url"`
		PackageType string `json:"packagetype"`
		PythonVer   string `json:"python_version"`
		Yanked      bool   `json:"yanked"`
		Digests     struct {
			SHA256 string `json:"sha256"`
		} `json:"digests"`
	} `json:"urls"`
}

// pypiFridaFiles fetches the file list for a frida version (empty = latest).
func pypiFridaFiles(ctx context.Context, ver string) (string, []pypiFile, error) {
	return pypiFiles(ctx, "frida", ver)
}

// pypiFiles fetches the file list for a package version from PyPI's JSON API
// (empty ver = latest). Returns the resolved version and the (non-yanked) files.
func pypiFiles(ctx context.Context, pkg, ver string) (string, []pypiFile, error) {
	url := pypiBase + "/" + pkg + "/json"
	if v := strings.TrimPrefix(strings.TrimSpace(ver), "v"); v != "" {
		url = pypiBase + "/" + pkg + "/" + v + "/json"
	}
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, "GET", url, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", "adbq/frida-tools")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("query PyPI for %s: %w", pkg, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil, fmt.Errorf("%s %s not found on PyPI", pkg, strings.TrimSpace(ver))
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("PyPI returned HTTP %d for %s %s", resp.StatusCode, pkg, strings.TrimSpace(ver))
	}
	var pr pypiResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&pr); err != nil {
		return "", nil, fmt.Errorf("parse PyPI response: %w", err)
	}
	return pr.Info.Version, parsePypiFiles(&pr), nil
}

func parsePypiFiles(pr *pypiResponse) []pypiFile {
	out := make([]pypiFile, 0, len(pr.URLs))
	for _, u := range pr.URLs {
		if u.Yanked {
			continue
		}
		out = append(out, pypiFile{
			Filename:    u.Filename,
			URL:         u.URL,
			SHA256:      u.Digests.SHA256,
			PackageType: u.PackageType,
			PythonTag:   u.PythonVer,
		})
	}
	return out
}

// hostWheelPlatforms returns the platform-tag substrings (all must be present)
// identifying the frida wheel for this host, in preference order.
func hostWheelPlatforms(goos, goarch string) [][]string {
	switch goos + "/" + goarch {
	case "darwin/arm64":
		return [][]string{{"macosx", "arm64"}}
	case "darwin/amd64":
		return [][]string{{"macosx", "x86_64"}}
	case "linux/amd64":
		return [][]string{{"manylinux", "x86_64"}}
	case "linux/arm64":
		return [][]string{{"manylinux", "aarch64"}}
	case "linux/arm":
		return [][]string{{"manylinux", "armv7l"}}
	case "linux/386":
		return [][]string{{"manylinux", "i686"}}
	case "windows/amd64":
		return [][]string{{"win_amd64"}}
	case "windows/arm64":
		return [][]string{{"win_arm64"}}
	case "windows/386":
		return [][]string{{"win32"}}
	}
	return nil
}

// selectHostWheel picks the single frida wheel matching the host OS+arch.
func selectHostWheel(files []pypiFile, goos, goarch string) (pypiFile, error) {
	groups := hostWheelPlatforms(goos, goarch)
	if groups == nil {
		return pypiFile{}, fmt.Errorf("Frida has no prebuilt wheel for %s/%s", goos, goarch)
	}
	for _, want := range groups {
		for _, f := range files {
			if f.PackageType == "bdist_wheel" && containsAll(f.Filename, want) {
				return f, nil
			}
		}
	}
	return pypiFile{}, fmt.Errorf("no matching Frida wheel for %s/%s among %d files", goos, goarch, len(files))
}

// selectUniversalWheel picks a pure-Python (py3-none-any) wheel — used for
// frida's declared dependencies (e.g. typing_extensions), which are platform-
// independent.
func selectUniversalWheel(files []pypiFile) (pypiFile, error) {
	for _, f := range files {
		if f.PackageType == "bdist_wheel" && strings.HasSuffix(f.Filename, "-none-any.whl") {
			return f, nil
		}
	}
	return pypiFile{}, fmt.Errorf("no pure-python wheel available")
}

// wheelRequires reads the Requires-Dist lines from a wheel's METADATA. A wheel is
// a zip; reading METADATA executes no code (unlike an sdist's setup.py).
func wheelRequires(wheelPath string) ([]string, error) {
	zr, err := zip.OpenReader(wheelPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".dist-info/METADATA") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		var reqs []string
		sc := bufio.NewScanner(rc)
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				break // headers end at the first blank line
			}
			if r, ok := strings.CutPrefix(line, "Requires-Dist:"); ok {
				reqs = append(reqs, strings.TrimSpace(r))
			}
		}
		return reqs, sc.Err()
	}
	return nil, nil
}

// neededDeps returns the bare package names from a wheel's Requires-Dist that
// apply to the target Python version (markers evaluated). Extra-gated deps are
// skipped since we never request extras.
func neededDeps(requires []string, pyVer string) []string {
	var out []string
	for _, req := range requires {
		name, marker := splitRequirement(req)
		if name == "" {
			continue
		}
		if marker != "" && !markerApplies(marker, pyVer) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// splitRequirement parses "name[extras] (spec) ; marker" into the package name
// and the marker expression.
func splitRequirement(req string) (name, marker string) {
	parts := strings.SplitN(req, ";", 2)
	spec := strings.TrimSpace(parts[0])
	if len(parts) == 2 {
		marker = strings.TrimSpace(parts[1])
	}
	name = spec
	if i := strings.IndexAny(spec, " [(<>=!~"); i >= 0 {
		name = spec[:i]
	}
	return strings.TrimSpace(name), marker
}

var rePyVerMarker = regexp.MustCompile(`python_version\s*(<=|>=|==|!=|<|>)\s*["']([0-9.]+)["']`)

// markerApplies evaluates a PEP 508 environment marker for our install context.
// We don't request extras, so extra-gated deps never apply. python_version
// comparisons are evaluated against pyVer; unknown markers default to applying
// (better to install a maybe-needed pure-python dep than to miss a required one).
func markerApplies(marker, pyVer string) bool {
	if strings.Contains(marker, "extra") {
		return false
	}
	if strings.Contains(marker, "python_version") {
		m := rePyVerMarker.FindStringSubmatch(marker)
		if m == nil {
			return true
		}
		cmp := compareVersions(majorMinor(pyVer), m[2])
		switch m[1] {
		case "<":
			return cmp < 0
		case "<=":
			return cmp <= 0
		case ">":
			return cmp > 0
		case ">=":
			return cmp >= 0
		case "==":
			return cmp == 0
		case "!=":
			return cmp != 0
		}
	}
	return true
}

func majorMinor(v string) string {
	p := strings.Split(strings.TrimSpace(v), ".")
	if len(p) >= 2 {
		return p[0] + "." + p[1]
	}
	return v
}

// ─── Runtime bridges (Java/ObjC/Swift) ─────────────────────────────────────
//
// Frida 17 removed the legacy `Java`/`ObjC`/`Swift` globals from the agent
// runtime; scripts now obtain them on demand. The `frida` CLI works because
// frida-tools ships the bridge implementations (frida_tools/bridges/*.js) and
// injects them. The bare `frida` package we install does NOT include them, so a
// pinning-bypass script that references `Java` fails with "Java is not defined".
//
// We mirror what frida-tools does: download the matching frida-tools wheel
// (verified by SHA256, no install, no deps — we only read the data files out of
// the zip) and cache the three bridge .js files. The driver then prepends the
// needed bridge(s), wrapped exactly like frida-tools' repl agent does
// (run the bridge source, then `globalThis.<Name> = bridge`).

// fridaBridgesDir is where extracted bridge .js files are cached, per version.
func fridaBridgesDir(ver string) (string, error) {
	base, err := fridaCacheSub("bridges")
	if err != nil {
		return "", err
	}
	return filepath.Join(base, ver), nil
}

// ensureFridaBridges makes sure java.js/objc.js/swift.js for the given frida
// version are cached on disk, downloading the frida-tools wheel once if needed.
// Returns the directory holding them. Best-effort: callers proceed without
// bridges if this fails (only scripts using Java/ObjC/Swift are affected).
func ensureFridaBridges(ctx context.Context, ver string) (string, error) {
	ver = strings.TrimPrefix(strings.TrimSpace(ver), "v")
	if ver == "" {
		return "", fmt.Errorf("no frida version for bridges")
	}
	dir, err := fridaBridgesDir(ver)
	if err != nil {
		return "", err
	}
	if fileExists(filepath.Join(dir, "java.js")) {
		return dir, nil // already extracted
	}

	// frida-tools has its OWN version scheme (independent of frida core — e.g.
	// frida-tools 14.x ships bridges for frida 17). Use the latest; its bridges
	// target the current frida major.
	_, files, err := pypiFiles(ctx, "frida-tools", "")
	if err != nil {
		return "", fmt.Errorf("resolve frida-tools: %w", err)
	}
	art, isSdist, err := selectFridaToolsArtifact(files)
	if err != nil {
		return "", err
	}
	wheelsDir, err := fridaWheelsDir()
	if err != nil {
		return "", err
	}
	artPath := filepath.Join(wheelsDir, art.Filename)
	if err := downloadVerifiedAsset(ctx, art.URL, art.SHA256, artPath, []string{pythonHostedHost}); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	extract := extractFridaBridgesZip
	if isSdist {
		extract = extractFridaBridgesTar
	}
	if err := extract(artPath, dir); err != nil {
		return "", err
	}
	if !fileExists(filepath.Join(dir, "java.js")) {
		return "", fmt.Errorf("frida-tools artifact %s contained no bridges", art.Filename)
	}
	return dir, nil
}

// selectFridaToolsArtifact prefers a pure-python wheel, falling back to the
// source tarball (recent frida-tools releases publish only an sdist, which still
// bundles the prebuilt bridge .js files).
func selectFridaToolsArtifact(files []pypiFile) (pypiFile, bool, error) {
	for _, f := range files {
		if f.PackageType == "bdist_wheel" && strings.HasSuffix(f.Filename, "-none-any.whl") {
			return f, false, nil
		}
	}
	for _, f := range files {
		if f.PackageType == "sdist" && strings.HasSuffix(f.Filename, ".tar.gz") {
			return f, true, nil
		}
	}
	return pypiFile{}, false, fmt.Errorf("no usable frida-tools artifact (wheel or sdist)")
}

var fridaBridgeFiles = map[string]bool{"java.js": true, "objc.js": true, "swift.js": true}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// extractFridaBridgesZip copies frida_tools/bridges/{java,objc,swift}.js out of
// a frida-tools wheel (a zip) without installing the package or its deps.
func extractFridaBridgesZip(wheelPath, destDir string) error {
	zr, err := zip.OpenReader(wheelPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if !strings.Contains(f.Name, "frida_tools/bridges/") || !fridaBridgeFiles[baseName(f.Name)] {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(rc, 16<<20))
		rc.Close()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(destDir, baseName(f.Name)), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// extractFridaBridgesTar does the same from a frida-tools source tarball
// (.tar.gz). Recent frida-tools releases ship only an sdist, but it still
// bundles the prebuilt bridge .js files as package data.
func extractFridaBridgesTar(tgzPath, destDir string) error {
	f, err := os.Open(tgzPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if !strings.Contains(h.Name, "frida_tools/bridges/") || !fridaBridgeFiles[baseName(h.Name)] {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, 16<<20))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(destDir, baseName(h.Name)), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ─── Runtime store (runtime.json) ──────────────────────────────────────────

type fridaRuntimeConfig struct {
	ManagedEnabled *bool          `json:"managedEnabled,omitempty"`
	External       []FridaRuntime `json:"external"`
}

// FridaStore persists host-runtime config (runtime.json), the script library
// (scripts.json + .js sidecars), and per-app script bindings (app-scripts.json),
// and owns managed-venv discovery/creation. All state is guarded by mu.
type FridaStore struct {
	mu       sync.Mutex
	path     string // runtime.json ("" = in-memory)
	managed  bool
	external []FridaRuntime

	// script library (frida_scripts.go)
	scriptsPath    string
	appScriptsPath string
	scriptsDir     string
	scripts        map[string]FridaScript // id → metadata (source lives in sidecars)
	appScripts     map[string]AppScripts  // package → binding
	scriptSeq      int
}

// NewFridaStore loads runtime.json and the script library, degrading to an
// in-memory store if the config dir is unavailable.
func NewFridaStore() (*FridaStore, error) {
	s := &FridaStore{managed: true}
	dir, err := fridaDataDir()
	if err != nil {
		s.initScripts()
		return s, nil
	}
	s.path = filepath.Join(dir, "runtime.json")
	if b, err := os.ReadFile(s.path); err == nil {
		var cfg fridaRuntimeConfig
		if json.Unmarshal(b, &cfg) == nil {
			s.external = cfg.External
			if cfg.ManagedEnabled != nil {
				s.managed = *cfg.ManagedEnabled
			}
		}
	}
	s.initScripts()
	return s, nil
}

func (s *FridaStore) save() error {
	if s.path == "" {
		return nil
	}
	me := s.managed
	return atomicWriteJSON(s.path, fridaRuntimeConfig{ManagedEnabled: &me, External: s.external})
}

// ManagedEnabled reports whether adbq may auto-create managed venvs.
func (s *FridaStore) ManagedEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.managed
}

// SetManagedEnabled toggles auto-managed venv installs (off = pure bring-your-own).
func (s *FridaStore) SetManagedEnabled(v bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.managed = v
	return s.save()
}

// ListRuntimes returns every host runtime able to drive Frida: managed venvs,
// registered external interpreters, and any frida already importable from a host
// Python (auto-discovered, so an existing install needs no manual registration).
func (s *FridaStore) ListRuntimes() []FridaRuntime {
	s.mu.Lock()
	exts := append([]FridaRuntime(nil), s.external...)
	s.mu.Unlock()

	out := listVenvs()
	seen := map[string]bool{}
	for _, r := range out {
		seen[r.PythonPath] = true
	}
	for _, e := range exts {
		if !seen[e.PythonPath] {
			seen[e.PythonPath] = true
			out = append(out, e)
		}
	}
	for _, d := range discoverSystemRuntimes() {
		if !seen[d.PythonPath] {
			seen[d.PythonPath] = true
			out = append(out, d)
		}
	}
	return out
}

// discoverSystemRuntimes probes the host Python interpreters (the ADBQ_FRIDA_PYTHON
// override, then python3/python) and reports any that can already import frida.
// Most system Pythons lack frida and fail fast; one that has it becomes a usable
// runtime with no registration step.
func discoverSystemRuntimes() []FridaRuntime {
	var cands []string
	if env := strings.TrimSpace(os.Getenv("ADBQ_FRIDA_PYTHON")); env != "" {
		cands = append(cands, env)
	}
	cands = append(cands, "python3", "python")

	var out []FridaRuntime
	seen := map[string]bool{}
	for _, name := range cands {
		p, ok := lookTool(name)
		if !ok {
			if fileExists(name) {
				p = name
			} else {
				continue
			}
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		if fv, pv, err := detectFridaInfo(p); err == nil && fv != "" {
			out = append(out, FridaRuntime{
				ID:            "system:" + p,
				Kind:          "system",
				Label:         "System " + filepath.Base(p) + " (frida " + fv + ")",
				PythonPath:    p,
				FridaVersion:  fv,
				PythonVersion: pv,
			})
		}
	}
	return out
}

// EnsureVenv provisions (or reuses) a managed venv with frida pinned to ver.
func (s *FridaStore) EnsureVenv(ctx context.Context, ver string, progress func(string)) (FridaRuntime, error) {
	if progress == nil {
		progress = func(string) {}
	}
	ver = strings.TrimPrefix(strings.TrimSpace(ver), "v")
	if ver == "" {
		return FridaRuntime{}, fmt.Errorf("no Frida version specified")
	}
	py, pyVer, err := DetectHostPython()
	if err != nil {
		return FridaRuntime{}, err
	}
	if !hostHasVenv(py) {
		return FridaRuntime{}, fmt.Errorf("Python's venv/ensurepip modules are unavailable — install python3-venv, or register an external interpreter instead")
	}
	venvsDir, err := fridaVenvsDir()
	if err != nil {
		return FridaRuntime{}, err
	}
	venvDir := filepath.Join(venvsDir, ver)
	venvPy := venvPython(venvDir)

	// Idempotent: a good venv already exists.
	if got := venvFridaVersion(venvPy); got == ver {
		return managedRuntime(ver, venvPy, got), nil
	}

	progress("creating venv")
	_ = os.RemoveAll(venvDir) // clear any half-built remnant
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	out, err := runHost(cctx, py, "-m", "venv", venvDir)
	cancel()
	if err != nil {
		_ = os.RemoveAll(venvDir)
		return FridaRuntime{}, fmt.Errorf("create venv: %s", firstLineOr(out, err))
	}

	progress("resolving wheel")
	_, files, err := pypiFridaFiles(ctx, ver)
	if err != nil {
		_ = os.RemoveAll(venvDir)
		return FridaRuntime{}, err
	}
	// Match the wheel to the venv's own interpreter arch, not adbq's process arch
	// — a user may run an x86_64 Python under Rosetta on an arm64 mac, etc.
	goos, goarch := hostPythonTarget(venvPy)
	wheel, err := selectHostWheel(files, goos, goarch)
	if err != nil {
		_ = os.RemoveAll(venvDir)
		return FridaRuntime{}, err
	}
	wheelsDir, err := fridaWheelsDir()
	if err != nil {
		_ = os.RemoveAll(venvDir)
		return FridaRuntime{}, err
	}

	progress("downloading wheel")
	wheelPath := filepath.Join(wheelsDir, wheel.Filename)
	if err := downloadVerifiedAsset(ctx, wheel.URL, wheel.SHA256, wheelPath, []string{pythonHostedHost}); err != nil {
		_ = os.RemoveAll(venvDir)
		return FridaRuntime{}, err
	}

	// frida declares deps (e.g. typing_extensions on Python < 3.11). Resolve the
	// ones that apply to this interpreter and fetch each as a SHA256-verified
	// wheel too, so the offline install below stays self-contained yet correct.
	installFiles := []string{wheelPath}
	reqs, _ := wheelRequires(wheelPath)
	for _, dep := range neededDeps(reqs, pyVer) {
		_, dfiles, derr := pypiFiles(ctx, dep, "")
		if derr != nil {
			continue // best-effort; the post-install import check catches a real miss
		}
		dw, werr := selectUniversalWheel(dfiles)
		if werr != nil {
			continue
		}
		dwPath := filepath.Join(wheelsDir, dw.Filename)
		if err := downloadVerifiedAsset(ctx, dw.URL, dw.SHA256, dwPath, []string{pythonHostedHost}); err != nil {
			_ = os.RemoveAll(venvDir)
			return FridaRuntime{}, fmt.Errorf("download dependency %s: %w", dep, err)
		}
		installFiles = append(installFiles, dwPath)
	}

	// Offline install of the verified wheels: no index, no transitive deps, no
	// sdist build — pip executes no network resolution and no arbitrary setup.py.
	progress("installing")
	args := append([]string{"-m", "pip", "install", "--no-index", "--no-deps", "--only-binary=:all:"}, installFiles...)
	ictx, icancel := context.WithTimeout(ctx, 3*time.Minute)
	out, err = runHost(ictx, venvPy, args...)
	icancel()
	if err != nil {
		_ = os.RemoveAll(venvDir)
		return FridaRuntime{}, fmt.Errorf("pip install: %s", firstLineOr(out, err))
	}

	got, perr := venvFridaProbe(venvPy)
	if got != ver {
		_ = os.RemoveAll(venvDir)
		if perr != nil {
			return FridaRuntime{}, fmt.Errorf("post-install check failed: %v", perr)
		}
		return FridaRuntime{}, fmt.Errorf("post-install check failed: venv reports frida %q, expected %q", got, ver)
	}
	progress("ready")
	return managedRuntime(ver, venvPy, got), nil
}

// RegisterExternal validates and records a user-provided interpreter/venv path.
func (s *FridaStore) RegisterExternal(path string) (FridaRuntime, error) {
	py := resolveInterpreter(path)
	fv, pv, err := detectFridaInfo(py)
	if err != nil {
		return FridaRuntime{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.external {
		if s.external[i].PythonPath == py {
			s.external[i].FridaVersion = fv
			s.external[i].PythonVersion = pv
			s.external[i].Label = externalLabel(py, fv)
			rt := s.external[i]
			_ = s.save()
			return rt, nil
		}
	}
	rt := FridaRuntime{
		ID:            fmt.Sprintf("ext:%d", time.Now().UnixNano()),
		Kind:          "external",
		Label:         externalLabel(py, fv),
		PythonPath:    py,
		FridaVersion:  fv,
		PythonVersion: pv,
		AddedAt:       time.Now().Unix(),
	}
	s.external = append(s.external, rt)
	if err := s.save(); err != nil {
		return rt, err
	}
	return rt, nil
}

// RemoveRuntime deletes a managed venv (by version) or forgets an external entry.
func (s *FridaStore) RemoveRuntime(id string) error {
	if strings.HasPrefix(id, "system:") {
		return fmt.Errorf("auto-discovered system runtime — uninstall frida from that Python to remove it")
	}
	if ver, ok := strings.CutPrefix(id, "managed:"); ok {
		if ver == "" || strings.ContainsAny(ver, "/\\") {
			return fmt.Errorf("invalid runtime id")
		}
		dir, err := fridaVenvsDir()
		if err != nil {
			return err
		}
		return os.RemoveAll(filepath.Join(dir, ver))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.external[:0]
	found := false
	for _, e := range s.external {
		if e.ID == id {
			found = true
			continue
		}
		kept = append(kept, e)
	}
	s.external = kept
	if !found {
		return fmt.Errorf("runtime %s not found", id)
	}
	return s.save()
}

// ResolveForVersion picks the best existing runtime for a device frida-server
// version: exact frida match first, then same-major. matchKind is
// "exact" | "major" | "none".
func (s *FridaStore) ResolveForVersion(deviceVer string) (FridaRuntime, string) {
	dv := strings.TrimPrefix(strings.TrimSpace(deviceVer), "v")
	var major FridaRuntime
	haveMajor := false
	for _, rt := range s.ListRuntimes() {
		if rt.FridaVersion == dv {
			return rt, "exact"
		}
		if !haveMajor && majorOf(dv) > 0 && majorOf(rt.FridaVersion) == majorOf(dv) {
			major, haveMajor = rt, true
		}
	}
	if haveMajor {
		return major, "major"
	}
	return FridaRuntime{}, "none"
}

// listVenvs scans the managed venv dir, returning one runtime per venv whose
// frida import works (half-built/broken venvs are skipped), newest frida first.
func listVenvs() []FridaRuntime {
	dir, err := fridaVenvsDir()
	if err != nil {
		return nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []FridaRuntime
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		py := venvPython(filepath.Join(dir, e.Name()))
		if fv := venvFridaVersion(py); fv != "" {
			out = append(out, managedRuntime(e.Name(), py, fv))
		}
	}
	sort.Slice(out, func(i, j int) bool { return compareVersions(out[i].FridaVersion, out[j].FridaVersion) > 0 })
	return out
}

func managedRuntime(ver, py, fv string) FridaRuntime {
	return FridaRuntime{ID: "managed:" + ver, Kind: "managed", Label: "Managed venv " + fv, PythonPath: py, FridaVersion: fv}
}

// ─── small host helpers ────────────────────────────────────────────────────

// runHost runs a host command with combined output (for diagnostics on failure).
func runHost(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

// runHostStdout runs a host command capturing stdout and stderr separately, so a
// warning printed to stderr can't corrupt the value we parse from stdout.
func runHostStdout(ctx context.Context, name string, args ...string) (stdout, stderr string, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var so, se strings.Builder
	cmd.Stdout = &so
	cmd.Stderr = &se
	err = cmd.Run()
	return so.String(), se.String(), err
}

// venvPython returns the interpreter path inside a venv for the host OS.
func venvPython(venvDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvDir, "Scripts", "python.exe")
	}
	return filepath.Join(venvDir, "bin", "python")
}

// resolveInterpreter maps a user-selected path to a runnable interpreter: a venv
// folder resolves to its python; a binary path is returned as-is.
func resolveInterpreter(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		if p := venvPython(path); fileExists(p) {
			return p
		}
	}
	return path
}

func externalLabel(py, fv string) string {
	dir := filepath.Dir(py)
	name := filepath.Base(py)
	if b := filepath.Base(dir); b == "bin" || b == "Scripts" {
		name = filepath.Base(filepath.Dir(dir)) // the venv folder name
	}
	return name + " (frida " + fv + ")"
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func firstLineOr(out string, err error) string {
	if s := strings.TrimSpace(firstLine(out)); s != "" {
		return s
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

func containsAll(s string, subs []string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// pythonAtLeast reports whether a "maj.min.patch" version string is >= maj.min.
func pythonAtLeast(v string, maj, min int) bool {
	parts := strings.Split(strings.TrimSpace(v), ".")
	if len(parts) < 2 {
		return false
	}
	a, e1 := strconv.Atoi(parts[0])
	b, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil {
		return false
	}
	if a != maj {
		return a > maj
	}
	return b >= min
}

func majorOf(v string) int {
	p := strings.SplitN(strings.TrimSpace(v), ".", 2)
	n, _ := strconv.Atoi(p[0])
	return n
}

// compareVersions compares dotted numeric versions; returns 1, 0, or -1.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

// parseVersionToken extracts the first dotted-numeric version token from s
// (e.g. "frida 16.4.7\n" → "16.4.7"). Returns "" when none is present.
func parseVersionToken(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			j := i
			for j < len(s) && (s[j] == '.' || (s[j] >= '0' && s[j] <= '9')) {
				j++
			}
			if tok := strings.Trim(s[i:j], "."); strings.Contains(tok, ".") {
				return tok
			}
			i = j
		}
	}
	return ""
}
