//go:build fridasmoke

// Opt-in integration smoke test for the host venv install path. It actually
// creates a venv and installs frida, so it never runs in CI or normal `go test`.
// Run with:  go test -tags fridasmoke -run TestEnsureVenvSmoke -v ./internal/adb/
//
// Everything happens under a temp HOME so the real user cache is untouched and
// is cleaned up automatically when the test ends.
package adb

import (
	"context"
	"testing"
)

func TestEnsureVenvSmoke(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	const ver = "17.15.0"
	s, err := NewFridaStore()
	if err != nil {
		t.Fatalf("NewFridaStore: %v", err)
	}
	rt, err := s.EnsureVenv(context.Background(), ver, func(stage string) { t.Logf("stage: %s", stage) })
	if err != nil {
		t.Fatalf("EnsureVenv: %v", err)
	}
	if rt.FridaVersion != ver {
		t.Fatalf("runtime version: got %q want %q", rt.FridaVersion, ver)
	}
	t.Logf("ok: managed runtime %+v", rt)

	// Idempotent second call must reuse the venv (and still report the version).
	rt2, err := s.EnsureVenv(context.Background(), ver, func(string) {})
	if err != nil {
		t.Fatalf("second EnsureVenv: %v", err)
	}
	if rt2.FridaVersion != ver {
		t.Fatalf("reused runtime version: got %q", rt2.FridaVersion)
	}

	// It must appear in the unified runtime listing.
	found := false
	for _, r := range s.ListRuntimes() {
		if r.Kind == "managed" && r.FridaVersion == ver {
			found = true
		}
	}
	if !found {
		t.Fatal("managed venv not found in ListRuntimes")
	}
}

// TestFridaSessionDriverSmoke exercises the real driver subprocess (JSON
// streaming, ring buffer, structured error, clean teardown) without a device:
// a bogus serial must yield a "ready" handshake then a structured "fatal".
func TestFridaSessionDriverSmoke(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	const ver = "17.15.0"
	s, err := NewFridaStore()
	if err != nil {
		t.Fatalf("NewFridaStore: %v", err)
	}
	rt, err := s.EnsureVenv(context.Background(), ver, func(string) {})
	if err != nil {
		t.Fatalf("EnsureVenv: %v", err)
	}

	sess, err := StartFridaSession(context.Background(), rt, "f-smoke",
		"bogus-serial-no-such-device", "com.example.app", "spawn",
		[]FridaScriptArg{{Name: "noop", Source: "console.log('loaded');"}})
	if err != nil {
		t.Fatalf("StartFridaSession: %v", err)
	}

	sawReady, sawFatal := false, false
	for m := range sess.Messages() {
		t.Logf("msg kind=%s payload=%q detail=%q", m.Kind, m.Payload, m.Detail)
		if m.Kind == "ready" {
			sawReady = true
		}
		if m.Kind == "fatal" {
			sawFatal = true
		}
	}
	if !sawReady {
		t.Error("expected a ready handshake from the driver")
	}
	if !sawFatal {
		t.Error("expected a fatal (no-device) message for a bogus serial")
	}
	if len(sess.LogSince(0)) == 0 {
		t.Error("ring buffer should retain messages")
	}
	sess.Stop() // idempotent + temp-dir cleanup
}
