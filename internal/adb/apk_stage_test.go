package adb

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStageKeySeparatesVersions(t *testing.T) {
	a := stageKey("com.example.app", "12")
	b := stageKey("com.example.app", "13")
	if a == b {
		t.Fatalf("two version codes must not share a staging directory: %q", a)
	}
	if got := stageKey("com.example.app", ""); !strings.HasSuffix(got, "-unknown") {
		t.Errorf("an unreadable version code must still produce a usable key, got %q", got)
	}
}

// A value that reaches stageKey comes off the device, so it is untrusted: it
// must not be able to escape the staging root or split into subdirectories.
func TestStageKeyCannotEscapeItsDirectory(t *testing.T) {
	for _, in := range []string{"../../etc", "a/b", `a\b`, "..", "."} {
		got := stageKey(in, in)
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("stageKey(%q) = %q contains a path separator", in, got)
		}
		joined := filepath.Join("/root", got)
		if !strings.HasPrefix(joined, "/root/") {
			t.Errorf("stageKey(%q) = %q escapes the root (%q)", in, got, joined)
		}
	}
}

func TestSanitizePathSegmentKeepsReadableNames(t *testing.T) {
	if got := sanitizePathSegment("com.example.app"); got != "com.example.app" {
		t.Errorf("an ordinary name must survive unchanged, got %q", got)
	}
	if got := sanitizePathSegment(""); got == "" {
		t.Error("an empty input must still yield a usable segment")
	}
}
