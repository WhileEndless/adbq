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
