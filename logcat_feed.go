package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"adbq/internal/adb"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// A chatty device emits well over a hundred log lines per second. Emitting one
// Wails event per line made the UI spend all its time in the JS bridge and in
// React re-renders, so the feed coalesces lines into arrays: at most one event
// per flushEvery, or earlier once maxBatch lines have piled up.
const (
	flushEvery = 100 * time.Millisecond
	maxBatch   = 400
	// feedBuffer absorbs a burst (e.g. the initial `-T` tail dump) while the
	// batcher is between flushes.
	feedBuffer = 8192
)

// procRefreshEvery is how often the pid→owner table is re-read. Processes come
// and go constantly; anything longer and a freshly launched app's lines stay
// unattributed for too long.
const procRefreshEvery = 4 * time.Second

// procNudgeGap throttles the out-of-band refreshes triggered by an unknown pid,
// so a stream full of short-lived pids cannot turn into a shell-call storm.
const procNudgeGap = time.Second

// probeTimeout bounds a single device round-trip made on the feed's behalf.
const probeTimeout = 10 * time.Second

// logcatFeed owns one device's logcat subscription: the underlying adb stream,
// the pid→owner snapshot used to tell app lines from OS ones, and the goroutine
// that batches entries out to the UI as `logcat:<serial>` events.
//
// It replaces the older one-event-per-line plumbing and folds in what used to
// be a separate pid supervisor: the periodic proc-table refresh already knows
// every pid, so a package filter can follow an app restart without a second
// round-trip to the device.
// logcatTailLines is the backfill a fresh feed asks for, so the pane has
// something on it immediately instead of waiting for the device to speak.
const logcatTailLines = 100

type logcatFeed struct {
	app    *App
	serial string
	ev     string
	pkg    string // package filter; "" = whole device

	// sys is the live "show OS-owned lines" switch. Atomic so the UI can flip
	// it without tearing down and restarting the adb process.
	sys atomic.Bool

	// dead is set when the adb process behind the current stream exits on its
	// own (device unplugged, adb server restarted, logd killed). The feed object
	// survives, but it will never emit again — EnsureLogcat uses this to tell a
	// healthy subscription from a hollow one.
	dead atomic.Bool

	lines  chan adb.LogEntry
	cancel context.CancelFunc
	done   chan struct{}
	nudge  chan struct{}
	clear  chan struct{}

	// dropped counts lines the forwarder had to discard because the UI fell
	// behind. Reported to the user on the next flush instead of leaving an
	// unexplained hole in the log.
	dropped atomic.Int64

	// emit is the sink for a finished batch. A field rather than a direct
	// runtime.EventsEmit call so the batching and filtering logic can be tested
	// without a live Wails context.
	emit func(event string, data any)

	mu      sync.Mutex
	stream  *adb.LogcatStream
	procs   map[int]adb.ProcOwner
	nudged  map[int]bool // pids we already asked the device about
	lastPID int          // pid the package filter is currently pinned to
	// lastPoll is stamped on every refresh ATTEMPT, not only on success: a
	// device whose `ps` keeps failing must not be re-probed in a tight loop.
	lastPoll time.Time
}

// startLogcatFeed spawns the adb stream plus the batching and proc-refresh
// goroutines. tailLines is passed through to `logcat -T`.
func (a *App) startLogcatFeed(serial, pkg string, showSystem bool, tailLines int) (*logcatFeed, error) {
	ctx, cancel := context.WithCancel(a.ctx)
	f := &logcatFeed{
		app:    a,
		serial: serial,
		ev:     "logcat:" + serial,
		pkg:    pkg,
		lines:  make(chan adb.LogEntry, feedBuffer),
		cancel: cancel,
		done:   make(chan struct{}),
		nudge:  make(chan struct{}, 1),
		clear:  make(chan struct{}, 1),
		nudged: map[int]bool{},
		emit:   func(event string, data any) { runtime.EventsEmit(a.ctx, event, data) },
	}
	f.sys.Store(showSystem)

	// Snapshot processes *before* the stream starts so the initial `-T` tail is
	// classified correctly instead of streaming past unattributed.
	f.refreshProcs(ctx)

	go f.batch(ctx)
	go f.pollProcs(ctx)

	if pkg != "" {
		pid := f.pidForPackage(ctx, pkg)
		if pid == 0 {
			// The app is not running. Starting adb without --pid would quietly
			// stream the WHOLE device while the UI claims to be filtered, so
			// instead we start nothing and let pollProcs attach the moment the
			// package appears.
			f.emit(f.ev, []adb.LogEntry{{
				Level: "I", Tag: "adbq",
				Msg: fmt.Sprintf("%s is not running — waiting for it to start", pkg),
			}})
			return f, nil
		}
		if err := f.attach(ctx, pid, tailLines); err != nil {
			cancel()
			return nil, err
		}
		return f, nil
	}

	if err := f.attach(ctx, 0, tailLines); err != nil {
		cancel()
		return nil, err
	}
	return f, nil
}

// pid reports which process the current stream is filtered to, or 0 for the
// whole device. Used to render the command that is actually running.
func (f *logcatFeed) pid() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastPID
}

// attach starts an adb stream (optionally pid-filtered) and installs it as the
// feed's current one, retiring whatever it replaces. It is the single place a
// stream is swapped, so the "did someone stop us mid-start" check lives here.
func (f *logcatFeed) attach(ctx context.Context, pid, tailLines int) error {
	s, err := f.app.client.StartLogcat(ctx, f.serial, pid, feedBuffer, tailLines)
	if err != nil {
		return err
	}
	// StartLogcat talks to the device, so the feed may have been stopped while
	// we were away. Installing the stream then would leak the adb process and
	// resurrect a feed the UI has already discarded.
	if ctx.Err() != nil {
		s.Stop()
		return ctx.Err()
	}
	f.mu.Lock()
	old := f.stream
	f.stream, f.lastPID = s, pid
	f.mu.Unlock()
	if old != nil {
		old.Stop()
	}
	go f.forward(s)
	return nil
}

// SetShowSystem flips the OS-line filter on a running feed. Lines already
// dropped are not recovered — only what arrives from here on is affected.
func (f *logcatFeed) SetShowSystem(v bool) { f.sys.Store(v) }

func (f *logcatFeed) Stop() {
	f.cancel()
	f.mu.Lock()
	s := f.stream
	f.stream = nil
	f.mu.Unlock()
	if s != nil {
		s.Stop()
	}
	<-f.done
}

// forward copies one adb stream's entries into the feed's shared channel, so a
// stream swap (app restarted under a package filter) is invisible to the
// batcher. Entries are dropped rather than blocking the reader when the buffer
// is full: falling behind must not stall the adb pipe and back up logd.
func (f *logcatFeed) forward(s *adb.LogcatStream) {
	for e := range s.Lines() {
		select {
		case f.lines <- e:
		default:
			f.dropped.Add(1)
		}
	}
	// The stream ended. If it is still the feed's current one this was not a
	// deliberate swap, so the feed is finished — mark it so a later
	// EnsureLogcat revives it instead of trusting a subscription that can
	// never produce another line.
	f.mu.Lock()
	current := f.stream == s
	f.mu.Unlock()
	if current {
		f.dead.Store(true)
	}
}

// Alive reports whether the feed can still deliver lines.
func (f *logcatFeed) Alive() bool { return !f.dead.Load() }

// batch coalesces entries into arrays and emits one event per flush.
func (f *logcatFeed) batch(ctx context.Context) {
	defer close(f.done)
	t := time.NewTicker(flushEvery)
	defer t.Stop()
	buf := make([]adb.LogEntry, 0, maxBatch)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		f.emit(f.ev, buf)
		buf = make([]adb.LogEntry, 0, maxBatch)
	}
	for {
		select {
		case <-ctx.Done():
			// Deliberately no final flush: a cancelled feed is one the UI has
			// already replaced, and emitting its leftovers would drop lines from
			// the previous filter into the freshly cleared new view.
			return
		case <-f.clear:
			// Everything in flight predates the user's Clear.
			buf = buf[:0]
			for {
				select {
				case <-f.lines:
					continue
				default:
				}
				break
			}
		case <-t.C:
			flush()
			if n := f.dropped.Swap(0); n > 0 {
				// Say so rather than leaving an unexplained gap in the log.
				f.emit(f.ev, []adb.LogEntry{{
					Level: "W", Tag: "adbq",
					Msg: fmt.Sprintf("dropped %d line(s): the UI could not keep up with the device", n),
				}})
			}
		case e := <-f.lines:
			if !f.classify(&e) {
				continue
			}
			buf = append(buf, e)
			if len(buf) >= maxBatch {
				flush()
			}
		}
	}
}

// Clear discards everything buffered between the device and the UI, so a user
// who hits Clear does not watch pre-Clear lines reappear a moment later.
func (f *logcatFeed) Clear() {
	select {
	case f.clear <- struct{}{}:
	default: // a clear is already queued; one is enough
	}
}

// classify annotates an entry with its owning process and reports whether the
// UI should see it at all.
func (f *logcatFeed) classify(e *adb.LogEntry) bool {
	f.mu.Lock()
	own, known := f.procs[e.PID]
	if known {
		e.Proc = own.Name
		e.App = own.IsApp()
	} else if !f.nudged[e.PID] {
		// First time we see this pid. Ask for one refresh and remember that we
		// did: most unresolvable pids belong to processes that already exited,
		// and re-asking for each of their lines would turn a chatty device into
		// a stream of `ps` invocations.
		f.nudged[e.PID] = true
		f.requestRefresh()
	}
	f.mu.Unlock()

	// A package filter is enforced by adb itself via --pid, so every line here
	// already belongs to the chosen app. Applying the OS filter on top of it
	// would blank the pane for any platform-signed app (shared system uid),
	// which is precisely the kind of app people filter for.
	if f.pkg != "" || f.sys.Load() {
		return true
	}
	// An unattributed line is usually a process that started since the last
	// snapshot — very often the app the user just launched. Show it: hiding
	// what we cannot attribute would swallow exactly the startup logs people
	// come here to read.
	if !known {
		return true
	}
	return own.IsApp()
}

func (f *logcatFeed) requestRefresh() {
	select {
	case f.nudge <- struct{}{}:
	default: // a refresh is already queued
	}
}

// pollProcs keeps the pid→owner table fresh and, when a package filter is
// active, re-points the adb stream at the app's new pid after a restart.
func (f *logcatFeed) pollProcs(ctx context.Context) {
	t := time.NewTicker(procRefreshEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-f.nudge:
			// Rate-limit out-of-band refreshes; the ticker covers the rest.
			f.mu.Lock()
			tooSoon := time.Since(f.lastPoll) < procNudgeGap
			f.mu.Unlock()
			if tooSoon {
				continue
			}
		}
		// A feed whose adb stream died will never speak again; keep shelling
		// into the device every few seconds and an unplugged phone would be
		// polled for the rest of the session.
		if f.dead.Load() {
			return
		}
		f.refreshProcs(ctx)
		if f.pkg != "" {
			f.followPackage(ctx)
		}
	}
}

func (f *logcatFeed) refreshProcs(ctx context.Context) {
	tbl, err := f.app.client.ProcTable(f.probeCtx(ctx), f.serial)
	// Stamp the attempt before deciding whether it was useful. Stamping only
	// successes would leave lastPoll at its zero value on a device whose `ps`
	// always fails, defeating the nudge throttle and spinning up adb processes
	// as fast as the log arrives.
	f.mu.Lock()
	f.lastPoll = time.Now()
	if err == nil && len(tbl) > 0 {
		f.procs = tbl
		// Pids can be asked about again now that the picture changed.
		f.nudged = map[int]bool{}
	}
	f.mu.Unlock()
}

// probeCtx bounds a device round-trip. adb inherits the app context, which
// never expires, so a wedged device would otherwise hang a refresh forever —
// and with it every StartLogcat queued behind lcMu.
func (f *logcatFeed) probeCtx(ctx context.Context) context.Context {
	c, cancel := context.WithTimeout(ctx, probeTimeout)
	// The caller returns long before the deadline in the normal case; the
	// cancel is only here so the timer is released.
	go func() {
		<-c.Done()
		cancel()
	}()
	return c
}

// pidForPackage resolves a package to its main pid from the current snapshot,
// falling back to the device's own lookup when the table has not caught up.
func (f *logcatFeed) pidForPackage(ctx context.Context, pkg string) int {
	f.mu.Lock()
	for pid, own := range f.procs {
		if own.Name == pkg {
			f.mu.Unlock()
			return pid
		}
	}
	f.mu.Unlock()
	pid, err := f.app.client.PidOf(f.probeCtx(ctx), f.serial, pkg)
	if err != nil {
		return 0
	}
	return pid
}

// followPackage points the adb stream at the filtered app's pid whenever it
// changes, so the UI keeps streaming across an app restart — and starts
// streaming at all for an app that was not running when the feed was created.
func (f *logcatFeed) followPackage(ctx context.Context) {
	pid := f.pidForPackage(ctx, f.pkg)
	f.mu.Lock()
	unchanged := pid == 0 || pid == f.lastPID
	f.mu.Unlock()
	if unchanged {
		return
	}
	// `-T 1` rather than no tail at all: an untailed logcat dumps the pid's
	// whole ring buffer, and after pid reuse that means a recycled process's
	// history spliced into the middle of the stream.
	if err := f.attach(ctx, pid, 1); err != nil {
		return
	}
	f.emit(f.ev+":pid", pid)
}
