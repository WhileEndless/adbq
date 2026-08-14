package adb

import (
	"context"
	"os"
	"testing"
	"time"
)

// AVD host tests drive the real emulator on this machine. They are opt-in for
// the same reason the device tests are: CI has no SDK, and booting an AVD costs
// minutes and gigabytes.
//
//	ADBQ_PROBE_SDK=1 go test ./internal/adb/ -run TestHostAVDList -v
//	ADBQ_PROBE_SDK=1 ADBQ_PROBE_AVD=Pixel_8 go test ./internal/adb/ -run TestHostAVDLifecycle -v -timeout 15m
func hostEmulator(t *testing.T) *EmulatorManager {
	t.Helper()
	sdk := hostSDK(t)
	return NewEmulatorManager(sdk, NewClient())
}

func TestHostAVDList(t *testing.T) {
	m := hostEmulator(t)
	avds, err := m.ListAVDs(context.Background())
	if err != nil {
		t.Fatalf("ListAVDs: %v", err)
	}
	if len(avds) == 0 {
		t.Skip("no AVDs defined on this host")
	}
	for _, a := range avds {
		t.Logf("%-24s api=%-3d tag=%-22s abi=%-10s play=%-5v state=%-8s serial=%s patched=%v snapshots=%v",
			a.Name, a.API, a.Tag, a.ABI, a.PlayStore, a.State, a.Serial, a.Patched, a.Snapshots)
		if a.Error != "" {
			t.Logf("  ! %s", a.Error)
			continue
		}
		if a.API == 0 {
			t.Errorf("%s: API level not resolved (target=%q)", a.Name, a.Target)
		}
		if a.Path == "" {
			t.Errorf("%s: no .avd path", a.Name)
		}
		if len(a.Commands) == 0 {
			t.Errorf("%s: no command shown (CLAUDE.md §4.1)", a.Name)
		}
	}
}

// The full lifecycle on real hardware: boot headless, confirm the serial we
// predicted is the serial adb reports, then shut down through the console.
func TestHostAVDLifecycle(t *testing.T) {
	name := os.Getenv("ADBQ_PROBE_AVD")
	if name == "" {
		t.Skip("set ADBQ_PROBE_AVD=<avd name> to run the lifecycle test")
	}
	m := hostEmulator(t)
	ctx := context.Background()

	serial, err := m.Start(ctx, name, EmulatorOpts{NoWindow: true, NoBootAnim: true, NoSnapshotSave: true})
	if err != nil {
		t.Fatalf("Start(%s): %v", name, err)
	}
	t.Logf("started %s as %s", name, serial)
	t.Cleanup(func() {
		if err := m.Stop(context.Background(), name); err != nil {
			t.Logf("stop: %v", err)
		}
	})

	if !m.IsManaged(name) {
		t.Error("an emulator adbq started must report as managed")
	}

	bootCtx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	if err := m.WaitForBoot(bootCtx, serial, func(s string) { t.Log(s) }); err != nil {
		t.Fatalf("WaitForBoot: %v", err)
	}

	avds, err := m.ListAVDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found *AVD
	for i := range avds {
		if avds[i].Name == name {
			found = &avds[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("%s vanished from the listing", name)
	}
	// The point of allocating the console port ourselves: the serial is known
	// before the boot finishes, and must match what adb ends up reporting.
	if found.Serial != serial {
		t.Errorf("serial drifted: predicted %s, adb reports %s", serial, found.Serial)
	}
	if found.State != AVDRunning {
		t.Errorf("state = %s, want running", found.State)
	}
	t.Logf("root=%s managed=%v port=%d", found.Root, found.Managed, found.Port)

	if lines := m.LogSince(name, 0); len(lines) == 0 {
		t.Error("the emulator log must contain at least the launch command")
	} else {
		t.Logf("log line 1: %s", lines[0].Text)
	}

	if err := m.Stop(ctx, name); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Give adb a moment to drop the transport before asserting it is gone.
	time.Sleep(6 * time.Second)
	avds, _ = m.ListAVDs(ctx)
	for _, a := range avds {
		if a.Name == name && a.State == AVDRunning {
			t.Errorf("%s still reports running after Stop", name)
		}
	}
}
