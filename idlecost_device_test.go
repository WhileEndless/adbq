package main

import (
	"context"
	"testing"
	"time"

	"adbq/internal/adb"
)

// What the whole application costs a device while nobody is touching it.
//
// The per-call budgets in internal/adb measure individual read paths; this
// measures the thing the user actually feels — the app open, a device attached,
// no interaction. It was 4.13 adb processes per second, every second, for as
// long as the app stayed open.
//
// A device test, skipped without hardware: the number depends on a real adb
// server with a real transport, and a fake would measure the fake.
func TestAppIdleCost(t *testing.T) {
	if testing.Short() {
		t.Skip("timed measurement; skipped in -short mode")
	}
	c := adb.NewClient()
	if _, err := c.Binary(); err != nil {
		t.Skipf("adb not available: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.StartServer(ctx); err != nil {
		t.Skipf("cannot start adb server: %v", err)
	}
	devs, err := c.ListDevices(ctx)
	if err != nil || len(devs) == 0 {
		t.Skip("no device attached")
	}

	a := NewApp()
	a.client = c
	w := a.startDeviceWatcher(ctx)
	defer w.Stop()

	// Let the first publish and its enrichment settle; the cold cost belongs to
	// the connect measurement, not to idle.
	time.Sleep(3 * time.Second)

	adb.ResetADBStats()
	const window = 20 * time.Second
	time.Sleep(window)
	st := adb.ADBStatsSnapshot(10)

	t.Logf("idle: %.3f adb processes/second (%d in %.0fs); push tracking=%v",
		st.PerSecond, st.Spawns, st.WindowSeconds, st.TrackingDevices)
	for _, cc := range st.TopCommands {
		t.Logf("    %-28s %d", cc.Command, cc.Count)
	}

	// While push tracking is live an idle app should ask adb nothing at all
	// inside this window; the only scheduled work is a once-a-minute safety net
	// that catches changes the adb server does not announce.
	//
	// Without push (an old or absent server) the watcher falls back to polling,
	// and the budget is the old steady state — degraded, but no worse than
	// before.
	budget := 1.0
	mode := "fallback polling"
	if st.TrackingDevices {
		budget = 0.1
		mode = "push tracking"
	}
	if st.PerSecond > budget {
		t.Errorf("idle costs %.3f adb processes/second under %s, budget %.2f — "+
			"something is polling that should be listening", st.PerSecond, mode, budget)
	}
}
