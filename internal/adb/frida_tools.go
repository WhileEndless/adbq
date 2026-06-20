package adb

import (
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

const pypiFridaBase = "https://pypi.org/pypi/frida"
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
	var sawOld string
	for _, name := range []string{"python3", "python"} {
		p, ok := lookTool(name)
		if !ok {
			continue
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
	if !fileExists(venvPy) {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	out, _, err := runHostStdout(ctx, venvPy, "-c", "import frida;print(frida.__version__)")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(firstLine(out))
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

// pypiFridaFiles fetches the file list for a frida version from PyPI's JSON API
// (empty ver = latest). Returns the resolved version and the (non-yanked) files.
func pypiFridaFiles(ctx context.Context, ver string) (string, []pypiFile, error) {
	url := pypiFridaBase + "/json"
	if v := strings.TrimPrefix(strings.TrimSpace(ver), "v"); v != "" {
		url = pypiFridaBase + "/" + v + "/json"
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
		return "", nil, fmt.Errorf("query PyPI for frida: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil, fmt.Errorf("frida %s not found on PyPI", strings.TrimSpace(ver))
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("PyPI returned HTTP %d for frida %s", resp.StatusCode, strings.TrimSpace(ver))
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

// ListRuntimes returns managed venvs plus registered external interpreters.
func (s *FridaStore) ListRuntimes() []FridaRuntime {
	s.mu.Lock()
	exts := append([]FridaRuntime(nil), s.external...)
	s.mu.Unlock()
	out := listVenvs()
	return append(out, exts...)
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
	py, _, err := DetectHostPython()
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
	wheel, err := selectHostWheel(files, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		_ = os.RemoveAll(venvDir)
		return FridaRuntime{}, err
	}
	wheelsDir, err := fridaWheelsDir()
	if err != nil {
		_ = os.RemoveAll(venvDir)
		return FridaRuntime{}, err
	}
	wheelPath := filepath.Join(wheelsDir, wheel.Filename)

	progress("downloading wheel")
	if err := downloadVerifiedAsset(ctx, wheel.URL, wheel.SHA256, wheelPath, []string{pythonHostedHost}); err != nil {
		_ = os.RemoveAll(venvDir)
		return FridaRuntime{}, err
	}

	// Offline install of the one verified wheel: no index, no deps, no sdist
	// build — pip executes no network resolution and no arbitrary setup.py.
	progress("installing")
	ictx, icancel := context.WithTimeout(ctx, 3*time.Minute)
	out, err = runHost(ictx, venvPy, "-m", "pip", "install", "--no-index", "--no-deps", "--only-binary=:all:", wheelPath)
	icancel()
	if err != nil {
		_ = os.RemoveAll(venvDir)
		return FridaRuntime{}, fmt.Errorf("pip install: %s", firstLineOr(out, err))
	}

	got := venvFridaVersion(venvPy)
	if got != ver {
		_ = os.RemoveAll(venvDir)
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
