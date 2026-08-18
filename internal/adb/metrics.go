package adb

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Process creation is what a poll-heavy adb wrapper actually spends its time
// on: every logical device read used to fork an `adb` client, which connects to
// the adb server, runs one command and exits. Parsing and rendering are noise
// next to that. So the work of batching reads into fewer round trips is judged
// by one number — how many adb processes we start per second — and this file
// is where that number comes from. Guessing at it is how the problem got here.
//
// Counting is unconditional: the atomics cost nothing next to a fork+exec, and
// a counter that has to be switched on is a counter nobody reads.

// spawnKind separates the two ways adb processes come into being, because they
// mean opposite things. A one-shot is a round trip that a cache or a batched
// command could have avoided; a stream is a long-lived subscription that is
// supposed to exist. Summing them would hide the first behind the second.
type spawnKind int

const (
	spawnOneShot spawnKind = iota // exec'd, waited on, gone — see Run
	spawnStream                   // exec'd and left running (logcat, pcap, shell, …)
)

// durationBucketBounds are the upper edges, in milliseconds, of the latency
// histogram. A bucket list rather than an average: adb latency is bimodal (a
// local emulator answers in single-digit ms, a sleeping USB phone in hundreds)
// and a mean of the two describes neither.
// It is an array, not a slice, so len() is a constant and the histogram can be
// a fixed-size array of atomics (no allocation, no bounds bookkeeping).
var durationBucketBounds = [6]int64{1, 5, 25, 100, 500, 2000}

// ADBStats is the snapshot the UI renders. It is a plain value type so it
// crosses the Wails bridge without the caller holding any lock.
type ADBStats struct {
	// Spawns counts one-shot adb processes since the window opened; Streams
	// counts long-lived ones. PerSecond is derived from Spawns alone.
	Spawns    int64   `json:"spawns"`
	Streams   int64   `json:"streams"`
	PerSecond float64 `json:"perSecond"`

	// WindowSeconds is how long Spawns covers — since process start, or since
	// the last ResetADBStats.
	WindowSeconds float64 `json:"windowSeconds"`

	// TotalMillis is the cumulative wall time spent inside one-shot runs. It
	// exceeds WindowSeconds×1000 when calls overlap, which is itself the point:
	// a large ratio means adb work is running concurrently, not serially.
	TotalMillis int64 `json:"totalMillis"`

	// Buckets is the latency histogram, coarsest-last.
	Buckets []DurationBucket `json:"buckets"`

	// TopCommands names the busiest command shapes, most frequent first. This
	// is the field that says *what* to fix: "shell getprop ×1180" points at a
	// caller that should have read a cache.
	TopCommands []CommandCount `json:"topCommands"`

	// TrackingDevices reports whether the push-based device tracker is live.
	// When false the app has fallen back to polling and the spawn count will
	// reflect that — without this flag a fallback looks like a regression with
	// no explanation.
	TrackingDevices bool `json:"trackingDevices"`
}

// DurationBucket is one histogram bar. UpToMillis is 0 for the overflow bucket,
// which holds everything slower than the last bound.
type DurationBucket struct {
	UpToMillis int64 `json:"upToMillis"`
	Count      int64 `json:"count"`
}

// CommandCount is how often one command shape was spawned. Shape, not the full
// command line: see spawnKeyFor.
type CommandCount struct {
	Command string `json:"command"`
	Count   int64  `json:"count"`
}

// metrics accumulates spawn counts. The atomics carry the hot path; the mutex
// only guards the per-command map, which is written once per spawn and read
// only when the user opens Settings.
type metrics struct {
	oneShots atomic.Int64
	streams  atomic.Int64
	nanos    atomic.Int64
	buckets  [len(durationBucketBounds) + 1]atomic.Int64

	mu     sync.Mutex
	byCmd  map[string]int64
	window time.Time

	tracking atomic.Bool
}

// adbMetrics is process-wide rather than per-Client: the thing being measured
// is this application's total load on the adb server, and tests construct
// Clients freely. ResetADBStats exists so a measurement run can start clean.
var adbMetrics = &metrics{byCmd: map[string]int64{}, window: time.Now()}

// recordSpawn notes one adb process. args is the full argv (including argv[0]);
// d is how long a one-shot took, and is ignored for streams, which have not
// finished yet by definition.
func (m *metrics) recordSpawn(kind spawnKind, args []string, d time.Duration) {
	if kind == spawnStream {
		m.streams.Add(1)
	} else {
		m.oneShots.Add(1)
		m.nanos.Add(int64(d))
		m.buckets[bucketIndex(d)].Add(1)
	}
	key := spawnKeyFor(args)
	if kind == spawnStream {
		key = "(stream) " + key
	}
	m.mu.Lock()
	m.byCmd[key]++
	m.mu.Unlock()
}

// bucketIndex maps a duration to its histogram slot; the last slot is overflow.
func bucketIndex(d time.Duration) int {
	ms := d.Milliseconds()
	for i, bound := range durationBucketBounds {
		if ms < bound {
			return i
		}
	}
	return len(durationBucketBounds)
}

// spawnKeyFor reduces an argv to a low-cardinality shape.
//
// The serial is dropped (it would split one call site across every attached
// device) and, for `adb shell`, only the first word of the remote command is
// kept — "shell getprop", not "shell getprop ro.build.id". Keeping the whole
// remote command would produce a key per package name, per pid, per path, and
// the top-N list would be all noise and no signal. The first word is exactly
// the grain that answers "which caller is spawning all these processes".
//
// The key is derived from argv the app itself built, and is truncated to a
// single word, so it carries no user data or secrets — it is safe to display.
func spawnKeyFor(args []string) string {
	if len(args) == 0 {
		return "adb"
	}
	rest := args[1:] // drop the resolved binary path
	// Skip the `-s <serial>` target if present.
	if len(rest) >= 2 && rest[0] == "-s" {
		rest = rest[2:]
	}
	if len(rest) == 0 {
		return "adb"
	}
	sub := rest[0]
	if sub != "shell" || len(rest) < 2 {
		return sub
	}
	remote := strings.TrimSpace(rest[1])
	// The remote command may be a whole script ("cat /proc/stat; echo '@@@'; …").
	// Cut at the first separator so a batched read still reports as one shape.
	if i := strings.IndexAny(remote, " \t\n;|&"); i > 0 {
		remote = remote[:i]
	}
	if remote == "" {
		return "shell"
	}
	return "shell " + remote
}

// snapshot renders the current counts. topN bounds the command list.
func (m *metrics) snapshot(topN int) ADBStats {
	m.mu.Lock()
	window := m.window
	cmds := make([]CommandCount, 0, len(m.byCmd))
	for k, v := range m.byCmd {
		cmds = append(cmds, CommandCount{Command: k, Count: v})
	}
	m.mu.Unlock()

	sort.Slice(cmds, func(i, j int) bool {
		if cmds[i].Count != cmds[j].Count {
			return cmds[i].Count > cmds[j].Count
		}
		return cmds[i].Command < cmds[j].Command
	})
	if len(cmds) > topN {
		cmds = cmds[:topN]
	}

	elapsed := time.Since(window).Seconds()
	spawns := m.oneShots.Load()
	perSec := 0.0
	if elapsed > 0 {
		perSec = float64(spawns) / elapsed
	}

	buckets := make([]DurationBucket, 0, len(m.buckets))
	for i := range m.buckets {
		up := int64(0)
		if i < len(durationBucketBounds) {
			up = durationBucketBounds[i]
		}
		buckets = append(buckets, DurationBucket{UpToMillis: up, Count: m.buckets[i].Load()})
	}

	return ADBStats{
		Spawns:          spawns,
		Streams:         m.streams.Load(),
		PerSecond:       perSec,
		WindowSeconds:   elapsed,
		TotalMillis:     m.nanos.Load() / int64(time.Millisecond),
		Buckets:         buckets,
		TopCommands:     cmds,
		TrackingDevices: m.tracking.Load(),
	}
}

func (m *metrics) reset() {
	m.oneShots.Store(0)
	m.streams.Store(0)
	m.nanos.Store(0)
	for i := range m.buckets {
		m.buckets[i].Store(0)
	}
	m.mu.Lock()
	m.byCmd = map[string]int64{}
	m.window = time.Now()
	m.mu.Unlock()
}

// ADBStatsSnapshot returns the current counters. topN bounds TopCommands.
func ADBStatsSnapshot(topN int) ADBStats {
	if topN <= 0 {
		topN = 15
	}
	return adbMetrics.snapshot(topN)
}

// ResetADBStats reopens the measurement window, so a before/after run can be
// taken without restarting the app.
func ResetADBStats() { adbMetrics.reset() }

// SetTrackingDevices records whether the push-based device tracker is live, so
// a snapshot can say why the spawn rate looks the way it does.
func SetTrackingDevices(on bool) { adbMetrics.tracking.Store(on) }

// countStreamSpawn records a long-lived adb process. Call it right after a
// successful cmd.Start() on a stream.
func countStreamSpawn(args []string) {
	adbMetrics.recordSpawn(spawnStream, args, 0)
}
