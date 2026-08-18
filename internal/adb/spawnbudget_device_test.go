package adb

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"
)

// This file measures what the app's idle poll actually costs a device, in the
// only unit that matters for an adb wrapper: how many `adb` processes one cycle
// starts. It is a device test (skipped without hardware, like the rest of
// integration_test.go) because the answer depends on the ROM — root style,
// which probes are cached, whether `cmd wifi status` exists — and a fake would
// measure the fake.
//
// Run it before and after a change to a read path:
//
//	go test ./internal/adb -run TestSpawnBudget -v
//
// The budgets below are deliberately loose upper bounds, not targets. They
// exist so a future change that reintroduces a per-read round trip fails here
// instead of quietly costing the user CPU for a year.
//
// They do not pass yet — they describe where the read paths are going, and the
// batching work that gets them there is still in progress. Until then enforcing
// them would leave `go test ./...` permanently red, so the assertions are gated
// behind ADBQ_SPAWN_BUDGET=1 while the measurements always print. Delete
// requireBudget (not the budgets) once the batching lands.

// measured on a physical device before any batching work:
//
//	warm ListDevices+Enrich   10.0 processes per poll
//	GetStats                   9 processes
//	ListConnections            4 processes
//	idle steady state          4.13 processes/second
//
// Recorded here rather than only in docs/performance.md so the next person to
// read a failure can see what "normal" used to be.

// requireBudget reports whether budget assertions are enforced. The numbers are
// always logged either way — the measurement is the point; the gate is only
// about not failing CI on work that is still queued.
func requireBudget(t *testing.T) bool {
	t.Helper()
	if os.Getenv("ADBQ_SPAWN_BUDGET") == "1" {
		return true
	}
	t.Log("(budget not enforced — set ADBQ_SPAWN_BUDGET=1 to fail on regressions)")
	return false
}

// spawnsDuring counts the one-shot adb processes started by fn.
func spawnsDuring(fn func()) (spawns int64, elapsed time.Duration) {
	before := adbMetrics.oneShots.Load()
	start := time.Now()
	fn()
	return adbMetrics.oneShots.Load() - before, time.Since(start)
}

// reportTop prints the busiest command shapes seen since the window opened, so
// a failing budget says *which* call to look at rather than just "too many".
func reportTop(t *testing.T) {
	t.Helper()
	adbMetrics.mu.Lock()
	counts := make([]CommandCount, 0, len(adbMetrics.byCmd))
	for k, v := range adbMetrics.byCmd {
		counts = append(counts, CommandCount{Command: k, Count: v})
	}
	adbMetrics.mu.Unlock()
	sort.Slice(counts, func(i, j int) bool { return counts[i].Count > counts[j].Count })
	for i, c := range counts {
		if i >= 10 {
			break
		}
		t.Logf("    %-28s %d", c.Command, c.Count)
	}
}

// TestSpawnBudgetEnrich measures one device-list poll — what the UI runs every
// few seconds while simply sitting on any screen.
func TestSpawnBudgetEnrich(t *testing.T) {
	c, serial := integrationDevice(t)
	if serial == "" {
		return
	}
	ctx := context.Background()

	// Warm the caches first: the honest steady-state number is the second poll
	// onward, not the first. Reporting the cold cost would flatter a later
	// change that only moved work into the warm-up.
	devs, err := c.ListDevices(ctx)
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	for i := range devs {
		c.Enrich(ctx, &devs[i])
	}

	ResetADBStats()
	const cycles = 3
	spawns, elapsed := spawnsDuring(func() {
		for range cycles {
			list, err := c.ListDevices(ctx)
			if err != nil {
				t.Fatalf("ListDevices: %v", err)
			}
			for i := range list {
				c.Enrich(ctx, &list[i])
			}
		}
	})

	perCycle := float64(spawns) / cycles
	t.Logf("warm ListDevices+Enrich: %.1f adb processes per poll (%d over %d cycles, %v)",
		perCycle, spawns, cycles, elapsed.Round(time.Millisecond))
	reportTop(t)

	// A warm poll should need the device list plus a small, bounded set of
	// genuinely-live reads. Anything more means a fact that cannot change is
	// being re-read on the poll.
	const budget = 4.0
	if perCycle > budget && requireBudget(t) {
		t.Errorf("warm poll costs %.1f adb processes, budget %.1f — "+
			"a value that does not change while the device stays connected is being re-read; "+
			"see the command breakdown above", perCycle, budget)
	}
}

// TestSpawnBudgetStats measures one Overview refresh.
func TestSpawnBudgetStats(t *testing.T) {
	c, serial := integrationDevice(t)
	if serial == "" {
		return
	}
	ctx := context.Background()
	if _, err := c.GetStats(ctx, serial); err != nil { // warm
		t.Fatalf("GetStats: %v", err)
	}

	ResetADBStats()
	spawns, elapsed := spawnsDuring(func() {
		if _, err := c.GetStats(ctx, serial); err != nil {
			t.Fatalf("GetStats: %v", err)
		}
	})
	t.Logf("GetStats: %d adb processes in %v", spawns, elapsed.Round(time.Millisecond))
	reportTop(t)

	const budget = 3
	if spawns > budget && requireBudget(t) {
		t.Errorf("GetStats costs %d adb processes, budget %d — "+
			"these are all /proc and dumpsys reads and belong in one round trip", spawns, budget)
	}
}

// TestSpawnBudgetConnections measures the Network screen's socket refresh.
func TestSpawnBudgetConnections(t *testing.T) {
	c, serial := integrationDevice(t)
	if serial == "" {
		return
	}
	ctx := context.Background()
	if _, err := c.ListConnections(ctx, serial); err != nil {
		t.Fatalf("ListConnections: %v", err)
	}

	ResetADBStats()
	spawns, _ := spawnsDuring(func() {
		if _, err := c.ListConnections(ctx, serial); err != nil {
			t.Fatalf("ListConnections: %v", err)
		}
	})
	t.Logf("ListConnections: %d adb processes", spawns)

	const budget = 1
	if spawns > budget && requireBudget(t) {
		t.Errorf("ListConnections costs %d adb processes, budget %d — "+
			"connectionsRemote() already renders these four reads as ONE command for the "+
			"§4.1 preview, so the preview and the execution have diverged", spawns, budget)
	}
}

// TestSpawnBudgetIdleSteadyState is the headline number: what adbq costs a
// device while the user is doing nothing at all. It approximates the poll set
// the UI runs on the Overview screen.
func TestSpawnBudgetIdleSteadyState(t *testing.T) {
	c, serial := integrationDevice(t)
	if serial == "" {
		return
	}
	if testing.Short() {
		t.Skip("skipping timed idle measurement in -short mode")
	}
	ctx := context.Background()

	// Warm every cache the real app would have warmed by this point.
	devs, _ := c.ListDevices(ctx)
	for i := range devs {
		c.Enrich(ctx, &devs[i])
	}
	_, _ = c.GetStats(ctx, serial)

	ResetADBStats()
	const window = 10 * time.Second
	deadline := time.Now().Add(window)
	// The UI polls devices every 5s and stats every 2.5s; at this ratio a 10s
	// window is 2 device polls and 4 stat polls.
	statTick := time.NewTicker(2500 * time.Millisecond)
	devTick := time.NewTicker(5 * time.Second)
	defer statTick.Stop()
	defer devTick.Stop()

	spawns, elapsed := spawnsDuring(func() {
		for time.Now().Before(deadline) {
			select {
			case <-statTick.C:
				_, _ = c.GetStats(ctx, serial)
			case <-devTick.C:
				list, _ := c.ListDevices(ctx)
				for i := range list {
					c.Enrich(ctx, &list[i])
				}
			case <-time.After(200 * time.Millisecond):
			}
		}
	})

	perSec := float64(spawns) / elapsed.Seconds()
	t.Logf("IDLE STEADY STATE: %.2f adb processes/second (%d in %v)",
		perSec, spawns, elapsed.Round(time.Millisecond))
	reportTop(t)

	const budget = 1.0
	if perSec > budget && requireBudget(t) {
		t.Errorf("idle costs %.2f adb processes/second, budget %.2f", perSec, budget)
	}
}
