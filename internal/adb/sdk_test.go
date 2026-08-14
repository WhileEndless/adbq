package adb

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseEmulatorVersion(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string
	}{
		{"modern", "Android emulator version 36.3.10.0 (build_id 14472402) (CL:N/A)\nCopyright (C) 2006-2024\n", "36.3.10.0"},
		{"older", "Android emulator version 31.3.14.0 (build_id 8807927)\n", "31.3.14.0"},
		{"noise first", "warning: something\nAndroid emulator version 30.0.0.0 (build_id 1)\n", "30.0.0.0"},
		{"absent", "some other tool\n", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseEmulatorVersion(tc.out); got != tc.want {
				t.Errorf("parseEmulatorVersion() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseAccelCheck(t *testing.T) {
	cases := []struct {
		name     string
		out      string
		wantOK   bool
		wantNote string
	}{
		{
			name:     "hypervisor available",
			out:      "accel:\n0\nHypervisor.Framework OS X Version 26.5\naccel\n",
			wantOK:   true,
			wantNote: "Hypervisor.Framework OS X Version 26.5",
		},
		{
			name:     "kvm missing",
			out:      "accel:\n1\nKVM is not installed on this machine\naccel\n",
			wantOK:   false,
			wantNote: "KVM is not installed on this machine",
		},
		{
			name:   "unparseable falls back to last line",
			out:    "emulator: ERROR: could not find accel\n",
			wantOK: false, wantNote: "emulator: ERROR: could not find accel",
		},
		{"empty", "", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, note := parseAccelCheck(tc.out)
			if ok != tc.wantOK || note != tc.wantNote {
				t.Errorf("parseAccelCheck() = (%v, %q), want (%v, %q)", ok, note, tc.wantOK, tc.wantNote)
			}
		})
	}
}

func TestJSONStringField(t *testing.T) {
	doc := `{"name": "Android Studio", "version": "2025.2.1", "buildNumber": "252.27397"}`
	if got := jsonStringField(doc, "version"); got != "2025.2.1" {
		t.Errorf("version = %q, want 2025.2.1", got)
	}
	if got := jsonStringField(doc, "name"); got != "Android Studio" {
		t.Errorf("name = %q, want Android Studio", got)
	}
	if got := jsonStringField(doc, "missing"); got != "" {
		t.Errorf("missing = %q, want empty", got)
	}
}

func TestStudioDisplayVersion(t *testing.T) {
	cases := []struct{ dataDir, version, want string }{
		{"AndroidStudio2025.2.2", "AI-252.27397.103.2522.14514259", "2025.2.2"},
		{"AndroidStudio2023.1", "AI-231.1", "2023.1"},
		{"", "AI-252.1", "AI-252.1"},                   // older layouts lack the field
		{"IntelliJIdea2024.1", "IU-241.1", "IU-241.1"}, // not Studio → keep raw
		{"AndroidStudio", "AI-1", "AI-1"},              // suffix empty → keep raw
	}
	for _, tc := range cases {
		if got := studioDisplayVersion(tc.dataDir, tc.version); got != tc.want {
			t.Errorf("studioDisplayVersion(%q, %q) = %q, want %q", tc.dataDir, tc.version, got, tc.want)
		}
	}
}

func TestLooksLikeSDKRejectsUnrelatedDirs(t *testing.T) {
	empty := t.TempDir()
	if looksLikeSDK(empty) {
		t.Error("an empty directory must not pass as an SDK root")
	}
	if looksLikeSDK(filepath.Join(empty, "does-not-exist")) {
		t.Error("a missing directory must not pass as an SDK root")
	}
	if err := os.MkdirAll(filepath.Join(empty, "platform-tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !looksLikeSDK(empty) {
		t.Error("a directory with platform-tools/ must pass as an SDK root")
	}
}

// resolveRoot must prefer the user's explicit setting over the environment,
// otherwise the Settings override silently does nothing on a machine that also
// has ANDROID_HOME set (which is the common case).
func TestResolveRootPrefersUserSetting(t *testing.T) {
	setting := t.TempDir()
	env := t.TempDir()
	for _, d := range []string{setting, env} {
		if err := os.MkdirAll(filepath.Join(d, "emulator"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ANDROID_HOME", env)
	t.Setenv("ANDROID_SDK_ROOT", "")

	m := &SDKManager{host: &HostStore{settings: HostSettings{SDKRoot: setting}}}
	root, source := m.resolveRoot()
	if root != setting || source != "setting" {
		t.Errorf("resolveRoot() = (%q, %q), want (%q, \"setting\")", root, source, setting)
	}

	// With no setting, ANDROID_HOME wins.
	m = &SDKManager{host: &HostStore{}}
	root, source = m.resolveRoot()
	if root != env || source != "ANDROID_HOME" {
		t.Errorf("resolveRoot() = (%q, %q), want (%q, \"ANDROID_HOME\")", root, source, env)
	}
}

// A setting pointing at a directory that is not an SDK must fall through to the
// environment rather than leaving the app with a dead root.
func TestResolveRootIgnoresBogusSetting(t *testing.T) {
	env := t.TempDir()
	if err := os.MkdirAll(filepath.Join(env, "platform-tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANDROID_HOME", env)
	t.Setenv("ANDROID_SDK_ROOT", "")

	m := &SDKManager{host: &HostStore{settings: HostSettings{SDKRoot: t.TempDir()}}}
	root, source := m.resolveRoot()
	if root != env || source != "ANDROID_HOME" {
		t.Errorf("resolveRoot() = (%q, %q), want the ANDROID_HOME fallback %q", root, source, env)
	}
}

func TestCmdlineToolCandidatesPrefersLatest(t *testing.T) {
	root := t.TempDir()
	for _, v := range []string{"latest", "11.0", "9.0"} {
		if err := os.MkdirAll(filepath.Join(root, "cmdline-tools", v, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := cmdlineToolCandidates(root, "avdmanager")
	if len(got) < 2 {
		t.Fatalf("want several candidates, got %v", got)
	}
	want := filepath.Join(root, "cmdline-tools", "latest", "bin", exeName("avdmanager"))
	if got[0] != want {
		t.Errorf("first candidate = %q, want %q", got[0], want)
	}
	// The legacy tools/bin location must remain as a last resort.
	last := got[len(got)-1]
	if last != filepath.Join(root, "tools", "bin", exeName("avdmanager")) {
		t.Errorf("last candidate = %q, want the legacy tools/bin path", last)
	}
}

func TestExeNameUsesBatForCmdlineToolsOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		if exeName("avdmanager") != "avdmanager" || exeName("adb") != "adb" {
			t.Error("non-Windows names must be unchanged")
		}
		return
	}
	if exeName("avdmanager") != "avdmanager.bat" {
		t.Error("avdmanager ships as a .bat on Windows")
	}
	if exeName("emulator") != "emulator.exe" {
		t.Error("emulator ships as an .exe on Windows")
	}
}

func TestHostStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &HostStore{path: filepath.Join(dir, "host.json")}
	if got := s.Get(); got.SDKRoot != "" {
		t.Errorf("fresh store must be empty, got %+v", got)
	}
	if err := s.Set(HostSettings{SDKRoot: "/x/sdk", ADBPath: "/x/adb"}); err != nil {
		t.Fatal(err)
	}
	reloaded := &HostStore{path: s.path}
	reloaded.load()
	if reloaded.Get().SDKRoot != "/x/sdk" || reloaded.Get().ADBPath != "/x/adb" {
		t.Errorf("settings did not survive a reload: %+v", reloaded.Get())
	}
}

// A store with no config dir must stay usable rather than panicking or erroring
// on every write — losing preferences is acceptable, crashing is not.
func TestHostStoreInMemoryFallback(t *testing.T) {
	s := &HostStore{}
	if err := s.Set(HostSettings{SDKRoot: "/x"}); err != nil {
		t.Fatalf("in-memory Set must not fail: %v", err)
	}
	if s.Get().SDKRoot != "/x" {
		t.Error("in-memory store must still hold the value")
	}
}
