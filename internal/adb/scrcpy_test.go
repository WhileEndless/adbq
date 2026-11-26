package adb

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestParseScrcpyMajor(t *testing.T) {
	cases := map[string]int{
		"scrcpy 2.4 <https://github.com/Genymobile/scrcpy>": 2,
		"scrcpy 1.25":  1,
		"scrcpy 3.1\n": 3,
		"garbage":      0,
		"":             0,
	}
	for in, want := range cases {
		if got := parseScrcpyMajor(in); got != want {
			t.Errorf("parseScrcpyMajor(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestScrcpyConcurrentStartsShareOneMirror(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	binary := filepath.Join(dir, "scrcpy")
	t.Setenv("ADBQ_TEST_SCRCPY_CALLS", calls)
	script := "#!/bin/sh\nprintf 'start\\n' >> \"$ADBQ_TEST_SCRCPY_CALLS\"\nexec /bin/sleep 30\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	manager := NewScrcpyManager()
	manager.binPath, manager.major = binary, 2
	t.Cleanup(manager.StopAll)

	const serial = "test-device"
	ready := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-ready
			results <- manager.Start(context.Background(), serial, ScrcpyDefaultArgs(serial))
		}()
	}
	close(ready)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if !manager.IsActive(serial) {
		t.Fatal("mirror is missing after concurrent starts")
	}
	out, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "start\n" {
		t.Fatalf("expected one process, got %q", out)
	}
	if err := manager.Stop(serial); err != nil {
		t.Fatal(err)
	}
	if manager.IsActive(serial) {
		t.Fatal("mirror still tracked after stop")
	}
}

func TestFilterUnsupportedArgs(t *testing.T) {
	args := []string{"--max-fps", "30", "--video-codec", "h264", "--window-title", "x"}

	// v1: --video-codec and its value must be stripped.
	m1 := &ScrcpyManager{procs: map[string]*exec.Cmd{}, major: 1}
	got := m1.filterUnsupportedArgs(args)
	want := []string{"--max-fps", "30", "--window-title", "x"}
	if !slices.Equal(got, want) {
		t.Errorf("v1 filter = %v, want %v", got, want)
	}

	// v2: kept as-is.
	m2 := &ScrcpyManager{procs: map[string]*exec.Cmd{}, major: 2}
	if got := m2.filterUnsupportedArgs(args); !slices.Equal(got, args) {
		t.Errorf("v2 filter = %v, want unchanged", got)
	}
}
