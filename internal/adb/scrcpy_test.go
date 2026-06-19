package adb

import (
	"os/exec"
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
