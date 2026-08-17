package adb

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The promise these previews make is that the text runs (CLAUDE.md §4.1 K7).
// Type-checking cannot verify that; only handing the rendered line to a shell
// can. This test does exactly that, for the read-only previews:
//
//	ADBQ_PROBE_SERIAL=<serial> go test ./internal/adb/ -run TestDeviceRenderedCommandsRun -v
//
// Only listings and property reads are executed. Nothing here starts, stops,
// installs, deletes or reboots anything — a test that mutated the attached
// device would be a worse bug than the one it was checking for.
func TestDeviceRenderedCommandsRun(t *testing.T) {
	if os.Getenv("ADBQ_SKIP_DEVICE") == "1" {
		t.Skip("ADBQ_SKIP_DEVICE=1")
	}
	serial := os.Getenv("ADBQ_PROBE_SERIAL")
	if serial == "" {
		t.Skip("set ADBQ_PROBE_SERIAL to run device tests")
	}
	ctx := context.Background()
	c := NewClient()

	cases := []struct {
		name string
		line string
	}{
		{"files: list a directory", c.FileCommandsFor(ctx, serial, FileCommandRequest{Dir: "/sdcard"}).List[0]},
		{"processes: procfs sweep", ProcessCommands(serial, false, c.Renderer(ctx, serial))[0]},
		{"network: read the proxy", c.NetCommandsFor(ctx, serial, "").ReadProxy[0]},
		{"network: read the sockets", c.NetCommandsFor(ctx, serial, "").Connections[0]},
		{"forwards: list", ForwardCommandsFor(serial, "forward", "", "").List[0]},
		{"frida: list servers", c.FridaCommandsFor(ctx, serial, "", 0).List[0]},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.HasPrefix(tc.line, "adb ") {
				t.Fatalf("not an adb command: %s", tc.line)
			}
			t.Log(tc.line)
			// `sh -c` is the point: it proves the quoting survives a shell, which
			// is what a user pasting the line actually goes through.
			out, err := exec.CommandContext(ctx, "sh", "-c", tc.line).CombinedOutput()
			if err != nil {
				t.Fatalf("the rendered command failed: %v\n%s", err, out)
			}
		})
	}
}

// A rendered command must name the device it targets: with several attached,
// a serial-less line acts on whichever one adb feels like.
func TestDeviceRenderedCommandsCarryTheSerial(t *testing.T) {
	if os.Getenv("ADBQ_SKIP_DEVICE") == "1" {
		t.Skip("ADBQ_SKIP_DEVICE=1")
	}
	serial := os.Getenv("ADBQ_PROBE_SERIAL")
	if serial == "" {
		t.Skip("set ADBQ_PROBE_SERIAL to run device tests")
	}
	ctx := context.Background()
	c := NewClient()
	for _, line := range c.AppCommandsFor(ctx, serial, "com.android.settings").ExportData {
		if !strings.Contains(line, serial) {
			t.Errorf("no serial in %q", line)
		}
	}
}

// The root previews claim to show the `su` form this device accepts, not a
// generic one. That is only checkable against a device: render a root command,
// then run it if root is really available and confirm it reaches uid 0.
func TestDeviceRootRenderingMatchesTheDevice(t *testing.T) {
	if os.Getenv("ADBQ_SKIP_DEVICE") == "1" {
		t.Skip("ADBQ_SKIP_DEVICE=1")
	}
	serial := os.Getenv("ADBQ_PROBE_SERIAL")
	if serial == "" {
		t.Skip("set ADBQ_PROBE_SERIAL to run device tests")
	}
	ctx := context.Background()
	c := NewClient()

	line := c.ShellCommandTextRoot(ctx, serial, "id", true)
	t.Log(line)
	if _, err := c.suStyleFor(ctx, serial); err != nil {
		// Unrooted: the preview shows the attempt, and showing it must not imply
		// it works. Nothing to execute here.
		if !strings.Contains(line, "su -c") {
			t.Errorf("an unrooted device should still show the attempt: %s", line)
		}
		t.Skip("device is not rooted — nothing to execute")
	}
	out, err := exec.CommandContext(ctx, "sh", "-c", line).CombinedOutput()
	if err != nil {
		t.Fatalf("the rendered root command failed: %v\n%s", err, out)
	}
	if !hasUID0(string(out)) {
		t.Errorf("the rendered root command did not run as root: %s", out)
	}
}
