package adb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAppLifecycleResultErrors(t *testing.T) {
	transport := errors.New("device offline")
	cases := []struct {
		name, output string
		check        func(string, error) error
		wantError    bool
	}{
		{"launch emits one event", "arg: 1\nEvents injected: 1\n## Network stats", appLaunchResultErr, false},
		{"launch has no activity", "** No activities found to run, monkey aborted.", appLaunchResultErr, true},
		{"launch emitted zero events", "Events injected: 0", appLaunchResultErr, true},
		{"launch absent result", "", appLaunchResultErr, true},
		{"clear succeeded", "Success\n", appClearResultErr, false},
		{"clear returned failure", "Failed", appClearResultErr, true},
		{"clear absent result", "", appClearResultErr, true},
		{"force stop succeeded", "", appStopResultErr, false},
		{"force stop remote exception", "Exception occurred while executing 'force-stop'", appStopResultErr, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.check(tc.output, nil); (err != nil) != tc.wantError {
				t.Fatalf("output %q, error %v, want error %t", tc.output, err, tc.wantError)
			}
			if err := tc.check("", transport); !errors.Is(err, transport) {
				t.Fatalf("got %v, want transport error", err)
			}
		})
	}
}

func TestRestartAppStopsBeforeLaunching(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "stop fails"}[fail], func(t *testing.T) {
			dir := t.TempDir()
			logFile := filepath.Join(dir, "calls")
			t.Setenv("ADBQ_TEST_COMMANDS", logFile)
			if fail {
				t.Setenv("ADBQ_TEST_STOP_FAILS", "yes")
			}
			path := filepath.Join(dir, "adb")
			fakeADB := `#!/bin/sh
printf '%s\n' "$*" >> "$ADBQ_TEST_COMMANDS"
case "$4" in
  "am force-stop "*) [ "$ADBQ_TEST_STOP_FAILS" != yes ] || printf 'Error: could not stop\n' ;;
  "monkey "*) printf 'Events injected: 1\n' ;;
esac
`
			if err := os.WriteFile(path, []byte(fakeADB), 0o700); err != nil {
				t.Fatal(err)
			}
			c := NewClient()
			c.SetBinary(path)
			if _, err := c.RestartApp(context.Background(), "test-device", "com.example.test"); (err != nil) != fail {
				t.Fatalf("restart error %v, want error %t", err, fail)
			}
			calls, err := os.ReadFile(logFile)
			if err != nil {
				t.Fatal(err)
			}
			want := "-s test-device shell am force-stop com.example.test"
			if !fail {
				want += "\n-s test-device shell monkey -p com.example.test -c android.intent.category.LAUNCHER 1"
			}
			if strings.TrimSpace(string(calls)) != want {
				t.Fatalf("commands = %q, want %q", calls, want)
			}
		})
	}
}
