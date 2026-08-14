package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"adbq/internal/adb"
)

// Opt-in end-to-end check of the one-click path, driven the way the Apps screen
// drives it: save scripts to the library, bind them to a package, resolve a
// server and runtime, run the session. Needs a rooted device or emulator with a
// frida-server installed and a matching host runtime already built.
//
//	ADBQ_PROBE_SERIAL=emulator-5554 go test . -run TestE2EOneClickMultiScript -v
//
// The binding deliberately carries TWO scripts that both use Java: that
// combination used to terminate the target app on startup, because each script
// got its own copy of the Java bridge and two of them patching ART at once kills
// the process.
//
// HOME is redirected so the real script library and history are untouched, with
// the frida cache (venvs, bridges) symlinked in so no venv has to be built.
func TestE2EOneClickMultiScript(t *testing.T) {
	serial := os.Getenv("ADBQ_PROBE_SERIAL")
	if serial == "" {
		t.Skip("set ADBQ_PROBE_SERIAL to run the end-to-end device test")
	}
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	tmpHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpHome, "Library", "Caches"), 0o755); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(realHome, "Library", "Caches", "adbq")
	if _, err := os.Stat(cache); err != nil {
		t.Skipf("no frida cache to borrow: %v", err)
	}
	if err := os.Symlink(cache, filepath.Join(tmpHome, "Library", "Caches", "adbq")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmpHome)

	a := NewApp()
	a.ctx = context.Background()

	const pkg = "com.android.settings"
	js := "Java.perform(function(){ console.log('%s: android ' + Java.androidVersion); });"
	var ids []string
	for _, n := range []string{"alpha", "beta"} {
		s, err := a.SaveFridaScript(adb.FridaScript{Name: n, Source: strings.Replace(js, "%s", n, 1)})
		if err != nil {
			t.Fatalf("SaveFridaScript(%s): %v", n, err)
		}
		ids = append(ids, s.ID)
	}
	if err := a.SetAppFridaScripts(pkg, ids, "spawn", ""); err != nil {
		t.Fatalf("SetAppFridaScripts: %v", err)
	}

	_, _ = a.client.StopFrida(a.ctx, serial)
	_, _ = a.client.ForceStopApp(a.ctx, serial, pkg)
	time.Sleep(time.Second)

	// StartAppWithFrida's body, minus the two progress events — runtime.EventsEmit
	// requires a Wails lifecycle context and aborts the process without one.
	binding := a.GetAppFridaScripts(pkg)
	if len(binding.ScriptIDs) != 2 {
		t.Fatalf("binding did not persist: %+v", binding)
	}
	ver, port, err := a.ensureDeviceFridaServer(serial, func(string) {})
	if err != nil {
		t.Fatalf("ensureDeviceFridaServer: %v", err)
	}
	t.Logf("frida-server %s on port %d", ver, port)

	rt, kind := a.frida.ResolveForVersion(ver)
	if kind == "none" {
		t.Skipf("no host runtime for frida %s", ver)
	}
	scripts, err := a.collectScripts(binding.ScriptIDs)
	if err != nil {
		t.Fatalf("collectScripts: %v", err)
	}
	sess, err := adb.StartFridaSession(a.ctx, a.client, rt, "e2e", serial, pkg, binding.Mode, port, scripts)
	if err != nil {
		t.Fatalf("StartFridaSession: %v", err)
	}
	defer sess.Stop()
	info := sess.Info()
	t.Logf("session %s · frida %s · %s", info.ID, info.Runtime, info.Mode)

	deadline := time.After(30 * time.Second)
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case <-deadline:
			t.Fatalf("only saw %v after 30s", seen)
		case <-time.After(500 * time.Millisecond):
			for _, m := range sess.LogSince(0) {
				if m.Kind == "detached" || m.Kind == "fatal" {
					t.Fatalf("session ended early: %s %s %s", m.Kind, m.Payload, m.Detail)
				}
				if m.Kind == "log" && strings.Contains(m.Payload, ": android ") {
					seen[m.Script] = true
				}
			}
		}
	}
	t.Logf("both scripts ran and were attributed: %v", seen)
	sess.Stop()
	_, _ = a.client.StopFrida(a.ctx, serial)
}
