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
          this.patch(serial, {paused: false});
        }
      })
      .catch(e => this.patch(serial, {running: false, error: String(e)}));
  }

  setFilter(serial: string, pkgFilter: string) {
    this.ensure(serial, pkgFilter);
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
    this.emitChange();
  }

  /** Tears down this device's event subscription and pending repaint. */
  private drop(serial: string) {
    this.unsubs[serial]?.();
    delete this.unsubs[serial];
    clearTimeout(this.repaints[serial]);
    this.repaints[serial] = undefined;
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
      ring.push(...entries);
      if (ring.length > LOGCAT_MAX) ring.splice(0, ring.length - LOGCAT_MAX);
      // Recording continues while paused — pause freezes the view only.
      if (!this.states[serial]?.paused) this.scheduleRepaint(serial);
    });
    this.unsubs[serial] = () => EventsOff(ev);
  }
}

export const logcatStore = new LogcatStore();

/** Subscribes a component to one device's logcat state. */
export function useLogcat(serial: string): LogcatState {
  return useSyncExternalStore(
    logcatStore.subscribe,
    () => logcatStore.getState(serial),
  );
}
