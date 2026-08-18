// Logcat lives in its own external store rather than in the React context that
// backs every other screen. The reason is throughput: a chatty device produces
// tens of thousands of lines a minute, and routing that through the shared
// provider re-rendered *every* component that calls useStore() — Files, Frida,
// the device tabs — several times a second, whether or not the user was even
// looking at the log. Here, only the components that subscribe re-render, and
// the lines themselves never enter React state at all.
import {useSyncExternalStore} from 'react';
import * as API from '../wailsjs/go/main/App';
import {EventsOn, EventsOff} from '../wailsjs/runtime/runtime';
import {LogEntry} from './types';

/** Ring capacity per device. ~200 B per entry bounds history at a few MB. */
const LOGCAT_MAX = 5000;

/**
 * Lines arrive from the backend already batched (~10 events/s). Repainting on
 * every one of those still costs more than it buys, so repaints are coalesced
 * to this interval; the ring is updated immediately either way.
 */
const RENDER_MS = 120;

/**
 * How long an identical line stays "already seen". Applied as a rolling
 * window, so a line repeating every 200ms — Samsung's keyboard is a good
 * example — shows once and then stays quiet until it actually stops for a
 * while.
 */
const REPEAT_WINDOW_MS = 10_000;

/** Cap on the repeat-detection table before stale keys are pruned. */
const REPEAT_KEYS_MAX = 4000;

export interface LogcatState {
  /** Bumped whenever the visible content changes; use it as a memo dependency. */
  version: number;
  paused: boolean;
  pkgFilter: string;
  /** Include OS-owned lines. Off by default: app logs are the signal. */
  showSystem: boolean;
  running: boolean;
  /** Set when the backend refused to start; shown instead of "waiting". */
  error: string;
}

const IDLE: LogcatState = {version: 0, paused: false, pkgFilter: '', showSystem: false, running: false, error: ''};
const NO_LINES: LogEntry[] = [];

class LogcatStore {
  private states: Record<string, LogcatState> = {};
  private rings: Record<string, LogEntry[]> = {};
  /**
   * While paused the screen renders this snapshot instead of the live ring.
   * The ring keeps filling underneath — pausing freezes the *view*, it does
   * not throw lines away — and without the snapshot any unrelated re-render
   * would let the mutating ring show through and scroll the frozen view out
   * from under the reader.
   */
  private frozen: Record<string, LogEntry[] | undefined> = {};
  /** key → timestamp of the last occurrence, for repeat detection. */
  private recent: Record<string, Map<string, number>> = {};
  private unsubs: Record<string, () => void> = {};
  private repaints: Record<string, ReturnType<typeof setTimeout> | undefined> = {};
  private listeners = new Set<() => void>();

  // ── React glue ─────────────────────────────────────────────────────────

  subscribe = (fn: () => void) => {
    this.listeners.add(fn);
    return () => { this.listeners.delete(fn); };
  };

  /** Snapshot identity is stable until something actually changes. */
  getState = (serial: string): LogcatState => this.states[serial] || IDLE;

  getLines = (serial: string): LogEntry[] => this.frozen[serial] || this.rings[serial] || NO_LINES;

  private emitChange() {
    for (const fn of this.listeners) fn();
  }

  private patch(serial: string, patch: Partial<LogcatState>) {
    const prev = this.states[serial] || IDLE;
    this.states[serial] = {...prev, ...patch, version: prev.version + 1};
    this.emitChange();
  }

  /** Coalesces "the ring changed" into one repaint per RENDER_MS. */
  private scheduleRepaint(serial: string) {
    if (this.repaints[serial]) return;
    this.repaints[serial] = setTimeout(() => {
      this.repaints[serial] = undefined;
      this.patch(serial, {});
    }, RENDER_MS);
  }

  // ── Commands ───────────────────────────────────────────────────────────

  /**
   * Called on every mount and on every filter change. The backend decides
   * whether a restart is actually needed and says so, which is the only
   * reliable signal for when the buffer must be dropped: a restarted feed
   * re-delivers a tail, while a healthy one keeps streaming into the buffer we
   * already have. React StrictMode calls this twice on mount; the second call
   * is a no-op for exactly that reason.
   */
  ensure(serial: string, pkgFilter: string) {
    if (!serial) return;
    const prev = this.states[serial];
    const showSystem = prev?.showSystem ?? false;
    this.listen(serial);
    this.patch(serial, {pkgFilter, running: true, error: ''});
    API.EnsureLogcat(serial, pkgFilter, showSystem)
      .then(restarted => {
        if (restarted) {
          this.rings[serial] = [];
          delete this.frozen[serial];
          delete this.recent[serial];
          this.patch(serial, {paused: false});
        }
      })
      .catch(e => this.patch(serial, {running: false, error: String(e)}));
  }

  setFilter(serial: string, pkgFilter: string) {
    this.ensure(serial, pkgFilter);
  }

  /**
   * Tells the backend whether anything is displaying this device's log.
   *
   * The feed used to keep delivering for the rest of the session after the
   * screen was visited once — ten events a second, JSON-encoded and pushed
   * across the bridge into a ring nobody was rendering. The device stream stays
   * up while quiet, so returning to the screen still shows the history; only
   * the delivery stops.
   */
  setVisible(serial: string, visible: boolean) {
    if (!serial) return;
    API.SetLogcatQuiet(serial, !visible).catch(() => {});
  }

  setPaused(serial: string, paused: boolean) {
    if (paused) this.frozen[serial] = (this.rings[serial] || []).slice();
    else delete this.frozen[serial];
    this.patch(serial, {paused});
  }

  setShowSystem(serial: string, showSystem: boolean) {
    // Flipped on the live feed — no adb restart, so nothing already on screen
    // is lost. Only lines arriving from here on are affected.
    API.SetLogcatSystem(serial, showSystem).catch(() => {});
    this.patch(serial, {showSystem});
  }

  clear(serial: string) {
    this.rings[serial] = [];
    delete this.recent[serial];
    if (this.frozen[serial]) this.frozen[serial] = [];
    this.patch(serial, {});
    API.ClearLogcat(serial).catch(() => {});
  }

  stop(serial: string) {
    this.drop(serial);
    API.StopLogcat(serial).catch(() => {});
    this.patch(serial, {running: false});
  }

  /**
   * Forgets a device entirely. Called when a device disappears from the device
   * list, so an unplugged phone does not keep its ring (and its backend feed,
   * with the adb process and periodic device polling behind it) alive for the
   * rest of the session.
   */
  release(serial: string) {
    if (!this.states[serial] && !this.rings[serial]) return;
    this.drop(serial);
    API.StopLogcat(serial).catch(() => {});
    delete this.states[serial];
    delete this.rings[serial];
    delete this.frozen[serial];
    delete this.recent[serial];
    this.emitChange();
  }

  /** Tears down this device's event subscription and pending repaint. */
  private drop(serial: string) {
    this.unsubs[serial]?.();
    delete this.unsubs[serial];
    clearTimeout(this.repaints[serial]);
    this.repaints[serial] = undefined;
  }

  /**
   * Flags each entry that repeats a line seen within REPEAT_WINDOW_MS. Done
   * once at ingest rather than during rendering: the ring is append-only, so
   * the answer never changes, and recomputing it for 5000 rows several times
   * a second would cost more than the collapsing saves.
   *
   * Only ever called while a package filter is active — that is the mode where
   * one app's own chatter is the whole picture and its repeats are pure noise.
   */
  private markRepeats(serial: string, entries: LogEntry[]) {
    const seen = this.recent[serial] || (this.recent[serial] = new Map());
    for (const e of entries) {
      const t = stampMs(e);
      e.t = t;
      // The thread id is left out on purpose: the same message from two
      // threads of one process is the same noise to a reader.
      const key = `${e.pid}\x00${e.lvl}\x00${e.tag}\x00${e.msg}`;
      const prev = seen.get(key);
      // A negative delta means the clock wrapped (midnight) — treat as fresh.
      if (prev !== undefined && t - prev >= 0 && t - prev < REPEAT_WINDOW_MS) e.dup = true;
      seen.set(key, t);
    }
    if (seen.size > REPEAT_KEYS_MAX) {
      const cutoff = (entries[entries.length - 1]?.t ?? Date.now()) - REPEAT_WINDOW_MS;
      for (const [k, ts] of seen) if (ts < cutoff) seen.delete(k);
    }
  }

  private listen(serial: string) {
    if (this.unsubs[serial]) return;
    const ev = `logcat:${serial}`;
    // The backend emits arrays; a lone entry is still accepted so a mismatched
    // frontend/backend pair degrades instead of showing nothing.
    EventsOn(ev, (batch: LogEntry[] | LogEntry) => {
      const entries = Array.isArray(batch) ? batch : [batch];
      if (!entries.length) return;
      const ring = this.rings[serial] || (this.rings[serial] = []);
      // Repeats are marked, never discarded: the user can switch collapsing
      // off and see them, which would be impossible if we dropped them here.
      if (this.states[serial]?.pkgFilter) this.markRepeats(serial, entries);
      ring.push(...entries);
      if (ring.length > LOGCAT_MAX) ring.splice(0, ring.length - LOGCAT_MAX);
      // Recording continues while paused — pause freezes the view only.
      if (!this.states[serial]?.paused) this.scheduleRepaint(serial);
    });
    this.unsubs[serial] = () => EventsOff(ev);
  }
}

/**
 * Timestamp used for repeat detection, in milliseconds.
 *
 * The device's own clock is preferred so the `-T` tail dump — a hundred lines
 * that all arrive in the same instant but were minutes apart on the phone — is
 * judged by when the lines actually happened. threadtime stamps come in
 * several shapes; only the "HH:MM:SS.mmm" token is needed here, and anything
 * unrecognised (a monotonic stamp, say) falls back to arrival time, which is
 * accurate enough for a live stream.
 */
function stampMs(e: LogEntry): number {
  const m = /(\d{1,2}):(\d{2}):(\d{2})[.,](\d{1,3})/.exec(e.time || '');
  if (!m) return Date.now();
  return ((+m[1] * 60 + +m[2]) * 60 + +m[3]) * 1000 + +m[4].padEnd(3, '0');
}

export const logcatStore = new LogcatStore();

/** Subscribes a component to one device's logcat state. */
export function useLogcat(serial: string): LogcatState {
  return useSyncExternalStore(
    logcatStore.subscribe,
    () => logcatStore.getState(serial),
  );
}
