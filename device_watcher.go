package main

import (
	"context"
	"sync"
	"time"

	"adbq/internal/adb"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// The device list is pushed to the UI, not polled by it.
//
// The frontend used to run `ListDevices` on a five-second timer for the whole
// life of the app. That forked an `adb` process every five seconds forever,
// enriched every attached device on every tick, and still showed a newly
// plugged phone up to five seconds late. The adb server already knows the
// instant anything changes and will stream it (internal/adb/track.go), so the
// timer only ever asked a question that had already been answered.
//
// The fallback lives here rather than in the frontend on purpose. The tracker
// is a long-lived socket to a server the user can kill from inside this very
// app (`adb kill-server` is a binding), so it will sometimes be down; but a
// device list that silently stops updating is worse than one that updates
// slowly. Keeping both paths behind a single `devices:changed` event means the
// UI has one thing to subscribe to and cannot get the fallback logic wrong.

const (
	// devicesEvent carries the current device list to the UI.
	devicesEvent = "devices:changed"
	// fallbackPollEvery is how often the watcher polls while push is unavailable.
	// The same interval the UI used to use, so the degraded mode is no worse
	// than the old steady state.
	fallbackPollEvery = 5 * time.Second
	// pushIdlePoll is a slow safety net that runs even while push is healthy.
	// The tracker reports transport changes, but a device can change in ways the
	// adb server does not announce — an `adb root` from another terminal, a
	// Magisk grant, an IP move — and this is what eventually notices.
	pushIdlePoll = 60 * time.Second
)

// deviceWatcher owns the device list and is the only thing that emits it.
type deviceWatcher struct {
	app     *App
	tracker *adb.Tracker

	cancel context.CancelFunc
	done   chan struct{}

	mu   sync.Mutex
	last []adb.Device
}

// startDeviceWatcher begins tracking and emitting. Safe to call once, at startup.
func (a *App) startDeviceWatcher(ctx context.Context) *deviceWatcher {
	wctx, cancel := context.WithCancel(ctx)
	w := &deviceWatcher{
		app:     a,
		tracker: a.client.StartTracker(wctx),
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	go w.loop(wctx)
	return w
}

func (w *deviceWatcher) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	<-w.done
	if w.tracker != nil {
		w.tracker.Stop()
	}
}

// Devices returns the most recently published list, for the binding that the UI
// calls once on mount.
func (w *deviceWatcher) Devices() []adb.Device {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]adb.Device, len(w.last))
	copy(out, w.last)
	return out
}

func (w *deviceWatcher) loop(ctx context.Context) {
	defer close(w.done)

	// Publish once immediately so the first paint is not waiting on a timer or
	// on the device changing.
	w.refresh(ctx)

	ticker := time.NewTicker(fallbackPollEvery)
	defer ticker.Stop()
	lastPoll := time.Now()

	for {
		select {
		case <-ctx.Done():
			return

		case list, ok := <-w.tracker.Updates():
			if !ok {
				// The tracker is finished (app shutting down); the ticker keeps
				// the list alive until the context closes.
				w.tracker = nil
				continue
			}
			// The transport set changed. Publish the bare list at once — this is
			// the whole point of push, a device appearing the moment it is
			// plugged in — then fill in the details.
			w.publish(list)
			w.enrichAndPublish(ctx, list)
			lastPoll = time.Now()

		case <-ticker.C:
			// While push is healthy this is the slow safety net; while it is
			// down it is the only source, so it runs at the full rate.
			every := pushIdlePoll
			if !w.tracker.State().Connected {
				every = fallbackPollEvery
			}
			if time.Since(lastPoll) < every {
				continue
			}
			lastPoll = time.Now()
			w.refresh(ctx)
		}
	}
}

// refresh lists devices the ordinary way and publishes the result.
func (w *deviceWatcher) refresh(ctx context.Context) {
	list, err := w.app.client.ListDevices(ctx)
	if err != nil {
		// A missing adb binary or a server that will not start is worth leaving
		// the previous list alone for: blanking the UI on a transient failure
		// makes a hiccup look like every device unplugging at once.
		return
	}
	w.enrichAndPublish(ctx, list)
}

// enrichAndPublish fills in per-device detail and publishes.
func (w *deviceWatcher) enrichAndPublish(ctx context.Context, list []adb.Device) {
	for i := range list {
		if ctx.Err() != nil {
			return
		}
		w.app.client.Enrich(ctx, &list[i])
	}
	w.publish(list)
}

// publish stores the list and tells the UI, skipping the event when nothing a
// user could see has changed. Re-emitting an identical list would re-render the
// device tabs and re-run every effect keyed on the device — at the fallback
// poll rate, for a machine sitting idle.
func (w *deviceWatcher) publish(list []adb.Device) {
	w.mu.Lock()
	same := devicesEqual(w.last, list)
	w.last = list
	w.mu.Unlock()
	if same || w.app.ctx == nil {
		return
	}
	runtime.EventsEmit(w.app.ctx, devicesEvent, list)
}

// devicesEqual compares by value, which is what the UI renders. Device holds
// only comparable fields, so this stays honest as fields are added — a new
// field is included automatically rather than being silently ignored by a
// hand-written comparison that nobody updated.
func devicesEqual(a, b []adb.Device) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ─── Bindings ────────────────────────────────────────────────────────────

// DeviceTracking reports whether the push subscription is live, so Settings can
// say why the spawn counter looks the way it does. A fallback nobody can see is
// a fallback nobody debugs.
func (a *App) DeviceTracking() adb.TrackerState {
	if a.devices == nil {
		return adb.TrackerState{}
	}
	return a.devices.tracker.State()
}
