package main

import (
	"testing"

	"adbq/internal/adb"
)

func server(name, ver, arch string) adb.FridaServer {
	return adb.FridaServer{
		Name: name, Path: "/data/local/tmp/" + name,
		Version: ver, Arch: arch, Runnable: true,
	}
}

func noRuntime(string) bool { return false }

// A device that has been used for a while accumulates frida-server builds. The
// one-click flow used to refuse outright once there was more than one and none
// was running, which on a real pentest device (17 installed) meant it never
// worked at all.
func TestChooseFridaServerPicksFromManyInstalls(t *testing.T) {
	servers := []adb.FridaServer{
		server("frida-server-16.1.4-android-arm64", "16.1.4", "arm64"),
		server("frida-server-17.5.1-android-arm64", "17.5.1", "arm64"),
		server("frida-server-14.0.2-android-arm64", "14.0.2", "arm64"),
	}
	got := chooseFridaServer(servers, map[string]bool{"arm64": true}, noRuntime, nil)
	if got == nil {
		t.Fatal("no server chosen from three valid candidates")
	}
	if got.Version != "17.5.1" {
		t.Errorf("chose %s, want the newest (17.5.1)", got.Version)
	}
}

// Architecture is decisive: an x86 build on an arm64 device cannot run at all.
func TestChooseFridaServerRejectsWrongArch(t *testing.T) {
	servers := []adb.FridaServer{
		server("frida-server-17.9.9-android-x86_64", "17.9.9", "x86_64"),
		server("frida-server-16.1.4-android-arm64", "16.1.4", "arm64"),
	}
	got := chooseFridaServer(servers, map[string]bool{"arm64": true}, noRuntime, nil)
	if got == nil || got.Arch != "arm64" {
		t.Fatalf("chose %+v, want the arm64 build even though it is older", got)
	}
}

// A version the host already has frida for beats a newer one that would force a
// venv build — the newer build is not more useful if nothing can drive it.
func TestChooseFridaServerPrefersAnAlreadyMatchedRuntime(t *testing.T) {
	servers := []adb.FridaServer{
		server("frida-server-17.9.9-android-arm64", "17.9.9", "arm64"),
		server("frida-server-17.5.1-android-arm64", "17.5.1", "arm64"),
	}
	hasRuntime := func(v string) bool { return v == "17.5.1" }
	got := chooseFridaServer(servers, map[string]bool{"arm64": true}, hasRuntime, nil)
	if got == nil || got.Version != "17.5.1" {
		t.Fatalf("chose %+v, want 17.5.1 (the version the host can already drive)", got)
	}
}

// With everything else equal, what the user ran here before wins.
func TestChooseFridaServerPrefersPreviouslyUsed(t *testing.T) {
	servers := []adb.FridaServer{
		server("frida-server-17.5.1-android-arm64", "17.5.1", "arm64"),
		server("frida-server-17.5.2-android-arm64", "17.5.2", "arm64"),
	}
	got := chooseFridaServer(servers, map[string]bool{"arm64": true}, noRuntime, map[string]bool{"17.5.1": true})
	if got == nil || got.Version != "17.5.1" {
		t.Fatalf("chose %+v, want the previously used 17.5.1", got)
	}
}

// Archives and unversioned files sit in the same directory and match the same
// glob; neither is something to launch.
func TestChooseFridaServerSkipsUnlaunchable(t *testing.T) {
	archive := server("frida-server-17.5.1-android-arm64.xz", "17.5.1", "arm64")
	archive.Runnable = false
	unversioned := server("frida-server", "", "")
	good := server("frida-server-16.1.4-android-arm64", "16.1.4", "arm64")

	got := chooseFridaServer([]adb.FridaServer{archive, unversioned, good},
		map[string]bool{"arm64": true}, noRuntime, nil)
	if got == nil || got.Version != "16.1.4" {
		t.Fatalf("chose %+v, want the one runnable versioned binary", got)
	}
}

// When nothing can run here, say so rather than launching something that will
// fail on the device.
func TestChooseFridaServerReturnsNilWhenNothingFits(t *testing.T) {
	servers := []adb.FridaServer{server("frida-server-17.5.1-android-x86_64", "17.5.1", "x86_64")}
	if got := chooseFridaServer(servers, map[string]bool{"arm64": true}, noRuntime, nil); got != nil {
		t.Errorf("chose %+v, but no candidate runs on this device", got)
	}
}

// An unknown device arch must not disqualify every candidate — that would turn a
// failed probe into "no server can run here".
func TestChooseFridaServerToleratesUnknownArch(t *testing.T) {
	servers := []adb.FridaServer{server("frida-server-17.5.1-android-arm64", "17.5.1", "arm64")}
	if got := chooseFridaServer(servers, nil, noRuntime, nil); got == nil {
		t.Error("no server chosen when the device arch probe returned nothing")
	}
}
