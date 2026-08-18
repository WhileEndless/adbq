package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"adbq/internal/adb"
)

// newTestFeed builds a feed with no adb stream behind it: entries are pushed
// straight onto f.lines, and batches land in a slice instead of the Wails
// event bus. That exercises the batching, classification and filtering logic —
// everything between the parser and the UI — without a device.
func newTestFeed(t *testing.T, procs map[int]adb.ProcOwner, showSystem bool) (*logcatFeed, func() [][]adb.LogEntry) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var got [][]adb.LogEntry

	f := &logcatFeed{
		serial: "test",
		ev:     "logcat:test",
		lines:  make(chan adb.LogEntry, 64),
		cancel: cancel,
		done:   make(chan struct{}),
		nudge:  make(chan struct{}, 1),
		clear:  make(chan struct{}, 1),
		procs:  procs,
		nudged: map[int]bool{},
		emit: func(_ string, data any) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, data.([]adb.LogEntry))
		},
	}
	f.sys.Store(showSystem)
	go f.batch(ctx)
	t.Cleanup(func() { cancel(); <-f.done })

	return f, func() [][]adb.LogEntry {
		mu.Lock()
		defer mu.Unlock()
		out := make([][]adb.LogEntry, len(got))
		copy(out, got)
		return out
	}
}

// drain waits for the batcher to flush, then returns every entry emitted so far.
func drain(batches func() [][]adb.LogEntry) []adb.LogEntry {
	deadline := time.Now().Add(2 * time.Second)
	var flat []adb.LogEntry
	for time.Now().Before(deadline) {
		time.Sleep(flushEvery + 20*time.Millisecond)
		flat = flat[:0]
		for _, b := range batches() {
			flat = append(flat, b...)
		}
		if len(flat) > 0 {
			return flat
		}
	}
	return flat
}

var testProcs = map[int]adb.ProcOwner{
	915:  {Name: "com.example.app", UID: 10076},
	1000: {Name: "system_server", UID: 1000},
	3213: {Name: "auditd", UID: 0},
}

func TestFeedHidesSystemLinesByDefault(t *testing.T) {
	f, batches := newTestFeed(t, testProcs, false)
	f.lines <- adb.LogEntry{PID: 915, Tag: "App", Msg: "hello"}
	f.lines <- adb.LogEntry{PID: 1000, Tag: "AMS", Msg: "noise"}
	f.lines <- adb.LogEntry{PID: 3213, Tag: "audit", Msg: "avc denied"}

	got := drain(batches)
	if len(got) != 1 {
		t.Fatalf("delivered %d entries, want only the app one: %+v", len(got), got)
	}
	if got[0].Tag != "App" || got[0].Proc != "com.example.app" || !got[0].App {
		t.Errorf("entry = %+v, want the app line annotated with its process", got[0])
	}
}

func TestFeedShowsSystemLinesWhenEnabled(t *testing.T) {
	f, batches := newTestFeed(t, testProcs, true)
	f.lines <- adb.LogEntry{PID: 915, Tag: "App"}
	f.lines <- adb.LogEntry{PID: 1000, Tag: "AMS"}

	got := drain(batches)
	if len(got) != 2 {
		t.Fatalf("delivered %d entries, want both: %+v", len(got), got)
	}
	if got[1].Proc != "system_server" || got[1].App {
		t.Errorf("system entry = %+v, want it annotated and marked non-app", got[1])
	}
}

func TestFeedLiveSystemToggleNeedsNoRestart(t *testing.T) {
	f, batches := newTestFeed(t, testProcs, false)
	f.lines <- adb.LogEntry{PID: 1000, Tag: "before"}
	// Let the batcher consume the first entry under the old setting.
	time.Sleep(flushEvery + 20*time.Millisecond)
	f.SetShowSystem(true)
	f.lines <- adb.LogEntry{PID: 1000, Tag: "after"}

	got := drain(batches)
	if len(got) != 1 || got[0].Tag != "after" {
		t.Fatalf("delivered %+v, want only the line that arrived after the toggle", got)
	}
}

func TestFeedKeepsLinesFromUnknownPids(t *testing.T) {
	// A pid absent from the snapshot is usually a process that just started —
	// very often the app under test. Those lines must not be swallowed.
	f, batches := newTestFeed(t, testProcs, false)
	f.lines <- adb.LogEntry{PID: 31337, Tag: "FreshlyLaunched"}

	got := drain(batches)
	if len(got) != 1 || got[0].Tag != "FreshlyLaunched" {
		t.Fatalf("delivered %+v, want the unknown-pid line kept", got)
	}
	select {
	case <-f.nudge:
	default:
		t.Errorf("an unknown pid should have queued a proc-table refresh")
	}
}

func TestFeedCoalescesIntoBatches(t *testing.T) {
	// The whole point of the feed: many lines must arrive as few events.
	f, batches := newTestFeed(t, testProcs, true)
	const n = 50
	for i := 0; i < n; i++ {
		f.lines <- adb.LogEntry{PID: 915, Tag: "spam"}
	}
	got := drain(batches)
	if len(got) != n {
		t.Fatalf("delivered %d entries, want all %d", len(got), n)
	}
	if evs := len(batches()); evs > 3 {
		t.Errorf("%d events for %d lines — batching is not coalescing", evs, n)
	}
}

func TestFeedDropsPendingLinesOnStop(t *testing.T) {
	// A cancelled feed has been replaced by another one on the same event, so
	// its leftovers must not spill into the new, freshly cleared view.
	f, batches := newTestFeed(t, testProcs, true)
	f.lines <- adb.LogEntry{PID: 915, Tag: "stale"}
	time.Sleep(10 * time.Millisecond)
	f.cancel()
	<-f.done
	for _, b := range batches() {
		for _, e := range b {
			if e.Tag == "stale" {
				t.Fatalf("a cancelled feed emitted %+v after teardown", e)
			}
		}
	}
}

func TestFeedClearDropsInFlightLines(t *testing.T) {
	f, batches := newTestFeed(t, testProcs, true)
	f.lines <- adb.LogEntry{PID: 915, Tag: "before clear"}
	f.Clear()
	// Give the batcher a moment to observe the clear, then send a keeper.
	time.Sleep(2 * flushEvery)
	f.lines <- adb.LogEntry{PID: 915, Tag: "after clear"}

	got := drain(batches)
	for _, e := range got {
		if e.Tag == "before clear" {
			t.Fatalf("Clear left a pre-clear line in the stream: %+v", got)
		}
	}
	if len(got) != 1 || got[0].Tag != "after clear" {
		t.Fatalf("delivered %+v, want just the post-clear line", got)
	}
}

func TestFeedIgnoresSystemFilterUnderPackageFilter(t *testing.T) {
	// A platform-signed app runs below the app-uid line. With a --pid filter
	// already in force, applying the OS filter on top would blank the pane.
	f, batches := newTestFeed(t, testProcs, false)
	f.pkg = "com.android.systemui"
	f.lines <- adb.LogEntry{PID: 1000, Tag: "PlatformApp"}

	got := drain(batches)
	if len(got) != 1 || got[0].Tag != "PlatformApp" {
		t.Fatalf("delivered %+v, want the filtered package's line kept", got)
	}
}

func TestFeedAsksAboutEachUnknownPidOnlyOnce(t *testing.T) {
	// Lines from a process that already exited can never resolve; re-probing
	// the device for every one of them is how a chatty log turns into an adb
	// process storm.
	f, batches := newTestFeed(t, testProcs, false)
	for range 5 {
		f.lines <- adb.LogEntry{PID: 31337, Tag: "ghost"}
	}
	drain(batches)

	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.nudged[31337] {
		t.Fatalf("the unknown pid should be recorded as already probed")
	}
	if len(f.nudged) != 1 {
		t.Errorf("nudged = %v, want exactly the one unknown pid", f.nudged)
	}
}

// A dropped adb stream used to be terminal: the feed marked itself dead, the
// pane froze, and the only way back was to leave the screen and return. Over
// USB a drop is routine, so that made the log unreliable in exactly the case it
// exists for. These cover the classification that decides retry vs report.
func TestTransientStreamEndClassification(t *testing.T) {
	transient := []string{
		"",                      // silent exit: adb client torn down
		"read: unexpected EOF",  // the one users kept seeing
		"error: device offline", // phone dozing / renegotiating USB
		"error: protocol fault (couldn't read status): Connection reset by peer",
		"error: device '0123456789abcdef' not found", // transport gone, may return
		"adb: device still authorizing",
	}
	for _, r := range transient {
		if !adb.IsTransientStreamEnd(r) {
			t.Errorf("IsTransientStreamEnd(%q) = false, want true — a recoverable "+
				"drop reported as permanent freezes the pane", r)
		}
	}

	permanent := []string{
		"logcat: Unrecognized Option --pid", // pre-API-24 logcat
		"Unable to open log device '/dev/log/main': No such file or directory",
		"logcat: invalid filter expression",
	}
	for _, r := range permanent {
		if adb.IsTransientStreamEnd(r) {
			t.Errorf("IsTransientStreamEnd(%q) = true, want false — retrying a "+
				"rejected invocation spins forever on something retry cannot fix", r)
		}
	}
}

// A permanent failure must stop the feed AND say so. Reporting without marking
// dead would leave EnsureLogcat believing the subscription is healthy; marking
// dead without reporting leaves the user staring at a pane that simply stopped.
func TestFeedFailMarksDeadAndExplains(t *testing.T) {
	f, batches := newTestFeed(t, map[int]adb.ProcOwner{}, true)

	if !f.Alive() {
		t.Fatal("a fresh feed should be alive")
	}
	f.fail("logcat: Unrecognized Option --pid")

	if f.Alive() {
		t.Error("feed still reports Alive after fail(); EnsureLogcat would not rebuild it")
	}
	var msg string
	for _, b := range batches() {
		for _, e := range b {
			if e.Tag == "adbq" {
				msg = e.Msg
			}
		}
	}
	if msg == "" {
		t.Fatal("fail() emitted nothing; the pane would just stop with no explanation")
	}
	// The user needs the cause and the next step, not just adb's words.
	if !strings.Contains(msg, "Unrecognized Option") {
		t.Errorf("message drops the cause: %q", msg)
	}
	if !strings.Contains(msg, "reopen this screen") {
		t.Errorf("message names no way forward: %q", msg)
	}
}
