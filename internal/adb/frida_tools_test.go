package adb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadPypiFixture(t *testing.T) []pypiFile {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "pypi_frida.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var pr pypiResponse
	if err := json.Unmarshal(b, &pr); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if pr.Info.Version != "17.15.0" {
		t.Fatalf("fixture version: got %q", pr.Info.Version)
	}
	return parsePypiFiles(&pr)
}

func TestParsePypiFilesExcludesYanked(t *testing.T) {
	files := loadPypiFixture(t)
	// Fixture has 7 entries; the yanked win32 wheel must be dropped → 6.
	if len(files) != 6 {
		t.Fatalf("want 6 non-yanked files, got %d", len(files))
	}
	for _, f := range files {
		if f.Filename == "frida-17.15.0-cp37-abi3-win32.whl" {
			t.Fatal("yanked file should have been excluded")
		}
		if f.SHA256 == "" || f.URL == "" {
			t.Fatalf("file missing sha256/url: %+v", f)
		}
	}
}

func TestSelectHostWheel(t *testing.T) {
	files := loadPypiFixture(t)
	cases := []struct {
		goos, goarch string
		want         string // "" = expect error
	}{
		{"darwin", "arm64", "frida-17.15.0-cp37-abi3-macosx_11_0_arm64.whl"},
		{"darwin", "amd64", "frida-17.15.0-cp37-abi3-macosx_10_13_x86_64.whl"},
		{"linux", "amd64", "frida-17.15.0-cp37-abi3-manylinux1_x86_64.whl"},
		{"linux", "arm64", "frida-17.15.0-cp37-abi3-manylinux2014_aarch64.whl"},
		{"windows", "amd64", "frida-17.15.0-cp37-abi3-win_amd64.whl"},
		{"linux", "arm", ""},    // no armv7l wheel in the fixture
		{"linux", "mips64", ""}, // unsupported platform
		{"plan9", "amd64", ""},  // unmapped OS
	}
	for _, c := range cases {
		got, err := selectHostWheel(files, c.goos, c.goarch)
		if c.want == "" {
			if err == nil {
				t.Errorf("%s/%s: expected error, got %q", c.goos, c.goarch, got.Filename)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s/%s: unexpected error: %v", c.goos, c.goarch, err)
			continue
		}
		if got.Filename != c.want {
			t.Errorf("%s/%s: got %q, want %q", c.goos, c.goarch, got.Filename, c.want)
		}
		if got.PackageType != "bdist_wheel" {
			t.Errorf("%s/%s: selected a non-wheel: %q", c.goos, c.goarch, got.PackageType)
		}
	}
}

func TestSelectHostWheelNeverPicksSdist(t *testing.T) {
	// A files list with only the sdist must error, never return the tarball.
	only := []pypiFile{{Filename: "frida-17.15.0.tar.gz", PackageType: "sdist"}}
	if got, err := selectHostWheel(only, "linux", "amd64"); err == nil {
		t.Fatalf("expected error, got %q", got.Filename)
	}
}

func TestPythonAtLeast(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"3.7.0", true}, {"3.7", true}, {"3.11.9", true}, {"4.0.0", true},
		{"3.6.15", false}, {"2.7.18", false}, {"3", false}, {"", false}, {"bogus", false},
	}
	for _, c := range cases {
		if got := pythonAtLeast(c.v, 3, 7); got != c.want {
			t.Errorf("pythonAtLeast(%q,3,7)=%v want %v", c.v, got, c.want)
		}
	}
}

func TestParseVersionToken(t *testing.T) {
	cases := []struct{ in, want string }{
		{"16.4.7", "16.4.7"},
		{"frida 16.4.7\n", "16.4.7"},
		{"Frida 17.0\n(c) ...", "17.0"},
		{"  12.11.18  ", "12.11.18"},
		{"no version here", ""},
		{"v17", ""}, // no dot → not a version token
	}
	for _, c := range cases {
		if got := parseVersionToken(c.in); got != c.want {
			t.Errorf("parseVersionToken(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"16.4.7", "16.4.6", 1},
		{"16.4.6", "16.4.7", -1},
		{"16.4.7", "16.4.7", 0},
		{"17.0.0", "16.9.9", 1},
		{"16.4", "16.4.0", 0},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestNeededDeps(t *testing.T) {
	// frida's real metadata: typing_extensions only on Python < 3.11.
	reqs := []string{`typing_extensions; python_version < "3.11"`}
	if got := neededDeps(reqs, "3.9.6"); len(got) != 1 || got[0] != "typing_extensions" {
		t.Fatalf("py3.9 should need typing_extensions, got %v", got)
	}
	if got := neededDeps(reqs, "3.11.2"); len(got) != 0 {
		t.Fatalf("py3.11 should need nothing, got %v", got)
	}
	if got := neededDeps(reqs, "3.13.0"); len(got) != 0 {
		t.Fatalf("py3.13 should need nothing, got %v", got)
	}
	// Extras are never requested → never needed.
	if got := neededDeps([]string{`colorama; extra == "cli"`}, "3.9.0"); len(got) != 0 {
		t.Fatalf("extra-gated dep should be skipped, got %v", got)
	}
	// Unmarked dep is always needed; version spec stripped from the name.
	if got := neededDeps([]string{"requests>=2.0"}, "3.12.0"); len(got) != 1 || got[0] != "requests" {
		t.Fatalf("unmarked dep: got %v", got)
	}
}

func TestSplitRequirement(t *testing.T) {
	cases := []struct{ in, name, marker string }{
		{`typing_extensions; python_version < "3.11"`, "typing_extensions", `python_version < "3.11"`},
		{"requests>=2.0", "requests", ""},
		{"colorama; extra == 'cli'", "colorama", "extra == 'cli'"},
		{"frida[tools] (>=1.0)", "frida", ""},
	}
	for _, c := range cases {
		n, m := splitRequirement(c.in)
		if n != c.name || m != c.marker {
			t.Errorf("splitRequirement(%q) = (%q,%q), want (%q,%q)", c.in, n, m, c.name, c.marker)
		}
	}
}

func TestMarkerApplies(t *testing.T) {
	cases := []struct {
		marker, pyVer string
		want          bool
	}{
		{`python_version < "3.11"`, "3.9.6", true},
		{`python_version < "3.11"`, "3.11.0", false},
		{`python_version >= "3.8"`, "3.9.0", true},
		{`python_version >= "3.8"`, "3.7.5", false},
		{`extra == "cli"`, "3.9.0", false},
		{`sys_platform == "linux"`, "3.9.0", true}, // unknown → default apply
	}
	for _, c := range cases {
		if got := markerApplies(c.marker, c.pyVer); got != c.want {
			t.Errorf("markerApplies(%q, %q) = %v, want %v", c.marker, c.pyVer, got, c.want)
		}
	}
}

func TestSelectUniversalWheel(t *testing.T) {
	files := []pypiFile{
		{Filename: "typing_extensions-4.12.2.tar.gz", PackageType: "sdist"},
		{Filename: "typing_extensions-4.12.2-py3-none-any.whl", PackageType: "bdist_wheel"},
	}
	w, err := selectUniversalWheel(files)
	if err != nil || w.Filename != "typing_extensions-4.12.2-py3-none-any.whl" {
		t.Fatalf("selectUniversalWheel: %q err=%v", w.Filename, err)
	}
	if _, err := selectUniversalWheel(files[:1]); err == nil {
		t.Fatal("expected error when only an sdist is available")
	}
}

func TestResolveForVersion(t *testing.T) {
	// Isolate the cache dir so listVenvs() finds no real managed venvs.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	s := &FridaStore{external: []FridaRuntime{
		{ID: "ext:1", Kind: "external", PythonPath: "/x/py", FridaVersion: "16.4.7"},
		{ID: "ext:2", Kind: "external", PythonPath: "/y/py", FridaVersion: "17.2.1"},
	}}

	if rt, kind := s.ResolveForVersion("16.4.7"); kind != "exact" || rt.ID != "ext:1" {
		t.Fatalf("exact match: got %s/%s", rt.ID, kind)
	}
	if rt, kind := s.ResolveForVersion("16.9.9"); kind != "major" || rt.ID != "ext:1" {
		t.Fatalf("major match: got %s/%s", rt.ID, kind)
	}
	if _, kind := s.ResolveForVersion("15.0.0"); kind != "none" {
		t.Fatalf("no match: got %s", kind)
	}
}
