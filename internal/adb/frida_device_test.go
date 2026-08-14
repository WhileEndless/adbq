package adb

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Opt-in device tests for the Frida path. They need a real device or emulator,
// so they stay off by default and are driven by environment variables:
//
//	ADBQ_PROBE_SERIAL      adb serial to target (required; without it everything skips)
//	ADBQ_PROBE_FRIDA_PATH  on-device frida-server to launch (enables the start test)
//
// The unit tests cover the pure logic; these cover what only a device can prove
// — that a launch returns instead of hanging, that root is reachable, and that
// the running server is identified correctly.

func probeClient(t *testing.T) (*Client, string) {
	t.Helper()
	serial := strings.TrimSpace(os.Getenv("ADBQ_PROBE_SERIAL"))
	if serial == "" {
		t.Skip("set ADBQ_PROBE_SERIAL to run device tests")
	}
	c := NewClient()
	if _, err := c.Binary(); err != nil {
		t.Skipf("adb not available: %v", err)
	}
	return c, serial
}

// TestDeviceCapabilities_Probe reports what adbq sees on the device. It asserts
// only the basics; its value is the log when diagnosing a specific ROM.
func TestDeviceCapabilities_Probe(t *testing.T) {
	c, serial := probeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	caps := c.Capabilities(ctx, serial)
	t.Logf("sdk=%d release=%s abi=%s abilist=%v selinux=%s bits64=%v su=%v magisk=%v",
		caps.SDK, caps.Release, caps.ABI, caps.ABIList, caps.SELinux, caps.Bits64,
		caps.Has["su"], caps.Has["magisk"])
	if caps.SDK == 0 {
		t.Error("no SDK level read from the device")
	}

	style, err := c.suStyleFor(ctx, serial)
	t.Logf("su style=%d err=%v", style, err)

	d := Device{ID: serial, Online: true}
	c.Enrich(ctx, &d)
	t.Logf("enrich: root=%v method=%q arch=%s", d.Root, d.RootMethod, d.Arch)
}

// TestListFridaServers_Probe checks the on-device inventory is coherent: every
// listed entry must be something we could actually launch.
func TestListFridaServers_Probe(t *testing.T) {
	c, serial := probeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	servers, err := c.ListFridaServers(ctx, serial)
	if err != nil {
		t.Fatalf("ListFridaServers: %v", err)
	}
	seenPorts := map[int]string{}
	for _, s := range servers {
		t.Logf("  %-42s ver=%-9s arch=%-7s active=%v pid=%d port=%d runnable=%v ambiguous=%v perms=%s",
			s.Name, s.Version, s.Arch, s.Active, s.PID, s.Port, s.Runnable, s.Ambiguous, s.Perms)

		// Download artifacts share the directory and match the same glob. They
		// may be listed — the user can still delete them — but must never be
		// offered as something to launch.
		for _, ext := range []string{".xz", ".gz", ".zip", ".bz2"} {
			if strings.HasSuffix(s.Name, ext) && s.Runnable {
				t.Errorf("archive %s reported as runnable", s.Name)
			}
		}
		if !s.Active {
			continue
		}
		// Two servers cannot share a port, so a repeated port means one process
		// was matched to several binaries — the bug that made the UI claim two
		// servers were running at once.
		if other, dup := seenPorts[s.Port]; dup {
			t.Errorf("%s and %s both reported active on port %d", other, s.Name, s.Port)
		}
		seenPorts[s.Port] = s.Name
		if s.Port == 0 {
			t.Errorf("%s is active but reports no port", s.Name)
		}
	}

	ver, err := c.DetectRunningFridaVersion(ctx, serial)
	t.Logf("running version=%q err=%v", ver, err)
}

// TestStartFridaReturnsPromptly_Probe is the regression guard for the launch
// hang: a daemonized frida-server used to keep the adb shell's fds open, so the
// command never returned and the caller (which passes a background context)
// waited forever. The launch must complete in seconds, and the server must
// actually be running afterwards.
func TestStartFridaReturnsPromptly_Probe(t *testing.T) {
	c, serial := probeClient(t)
	path := strings.TrimSpace(os.Getenv("ADBQ_PROBE_FRIDA_PATH"))
	if path == "" {
		t.Skip("set ADBQ_PROBE_FRIDA_PATH to run the start test")
	}
	// Deliberately unbounded, like app.go: the timeout has to come from
	// StartFrida itself, not from the caller.
	ctx := context.Background()

	if _, err := c.StopFrida(ctx, serial); err != nil {
		t.Logf("pre-stop: %v", err)
	}

	start := time.Now()
	out, err := c.StartFrida(ctx, serial, path, "", 0)
	elapsed := time.Since(start)
	t.Logf("StartFrida took %s, out=%q err=%v", elapsed.Round(time.Millisecond), out, err)
	if err != nil {
		t.Fatalf("StartFrida: %v", err)
	}
	if elapsed > 10*time.Second {
		t.Errorf("StartFrida took %s — it should return as soon as the daemon forks", elapsed)
	}

	ver, err := c.DetectRunningFridaVersion(ctx, serial)
	if err != nil || ver == "" {
		t.Fatalf("server not running after start: ver=%q err=%v", ver, err)
	}
	t.Logf("running frida-server %s", ver)

	if _, err := c.StopFrida(ctx, serial); err != nil {
		t.Errorf("StopFrida: %v", err)
	}
}
