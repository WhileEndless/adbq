// Cross-screen state store. Lives at App level so logcat lines, shell
// sessions, and per-device caches survive navigation between screens.
import React, {createContext, useCallback, useContext, useEffect, useRef, useState} from 'react';
import {adb} from '../wailsjs/go/models';
import * as API from '../wailsjs/go/main/App';
import {EventsOn, EventsOff} from '../wailsjs/runtime/runtime';
import {LogEntry} from './types';

// ─── Per-device logcat ───────────────────────────────────────────────────

interface LogcatSlice {
  lines: LogEntry[];
  paused: boolean;
  pkgFilter: string;
  running: boolean;
}

const LOGCAT_MAX = 5000;

// ─── Per-device shell sessions (persistent across navigation) ────────────

export interface ShellSession {
  id: string;
  serial: string;
  label: string;
  root: boolean;
  buf: string;
}

// ─── Generic TTL cache for cheap reloads ─────────────────────────────────

interface CacheEntry<T> {
  data: T;
  ts: number;
}

// ─── Store shape ─────────────────────────────────────────────────────────

// QueuedShellCmd lets other screens (Apps "Open shell here") hand off a
// command that should be typed into a Shell session when the user lands on
// the Shell screen. Single-slot per device; consumed once.
export interface QueuedShellCmd {
  serial: string;
  cmd: string;
  root?: boolean;
}

// ─── Per-device live capture slice ───────────────────────────────────────
// Holds the running capture's packet ring + UI state at App level so it
// survives navigation away from the Capture screen. The Wails event
// subscription is owned by the store, not the screen component — when the
// user wanders off the packets keep arriving.

export interface CapturePacket {
  no: number; ts: string; length: number;
  srcIP: string; dstIP: string; srcPort: number; dstPort: number;
  proto: string; info: string; layers?: string[];
}

export interface CaptureSlice {
  active: boolean;
  packets: CapturePacket[];
  // Settings the user picked for the running session — kept so the screen
  // can re-display them after a remount.
  iface: string;
  bpf: string;
  preset: number;
  displayFilter: string;
  maxPackets: number;
  // Result of the last poll for the on-device tcpdump session (size, byte
  // count, rotation counter, link type).
  state: any | null;
  // Tag we bump every time a packet batch lands so React re-renders without
  // having to keep `packets` itself in component state.
  rev: number;
}

interface Store {
  // logcat per device
  getLogcat: (serial: string) => LogcatSlice;
  startLogcat: (serial: string, pkgFilter: string) => void;
  stopLogcat: (serial: string) => void;
  setPaused: (serial: string, paused: boolean) => void;
  clearLogcat: (serial: string) => void;

  // shells per device — keyed by sessionId
  shells: Record<string, ShellSession>;
  openShell: (serial: string, root: boolean) => Promise<string>;
  writeShell: (id: string, data: string) => Promise<void>;
  closeShell: (id: string) => void;
  clearShellBuf: (id: string) => void;

  // captures per device
  getCapture: (serial: string) => CaptureSlice;
  startCapture: (serial: string, iface: string, bpf: string, preset: number, maxPackets: number, mirrorMaxBytes: number) => Promise<void>;
  stopCapture: (serial: string) => Promise<void>;
  clearCapture: (serial: string) => void;
  setCaptureDisplayFilter: (serial: string, f: string) => void;
  setCaptureMaxPackets: (serial: string, n: number) => void;
  setCaptureState: (serial: string, st: any) => void;
  setCapturePreset: (serial: string, p: number, bpf: string) => void;
  setCaptureIface: (serial: string, iface: string) => void;

  // generic cache with TTL
  cached: <T>(key: string, ttlMs: number, fetcher: () => Promise<T>) => Promise<T>;
  invalidate: (prefix: string) => void;

  // cross-screen hand-off: queue a command for the Shell screen to type into a
  // newly-opened session (e.g. from Apps "Open shell here").
  queueShellCmd: (req: QueuedShellCmd) => void;
  consumeShellCmd: (serial: string) => QueuedShellCmd | null;
}

const StoreCtx = createContext<Store | null>(null);

export function useStore(): Store {
  const s = useContext(StoreCtx);
  if (!s) throw new Error('useStore outside provider');
  return s;
}

export function StoreProvider({children}: {children: React.ReactNode}) {
  // ── logcat slices ──────────────────────────────────────────────────────
  const [logcats, setLogcats] = useState<Record<string, LogcatSlice>>({});
  const logcatsRef = useRef(logcats);
  logcatsRef.current = logcats;
  const lcSubs = useRef<Record<string, () => void>>({});

  const getLogcat = useCallback((serial: string): LogcatSlice => {
    return logcatsRef.current[serial] || {lines: [], paused: false, pkgFilter: '', running: false};
  }, []);

  const startLogcat = useCallback((serial: string, pkgFilter: string) => {
    const cur = logcatsRef.current[serial];
    const sameFilter = cur?.running && cur.pkgFilter === pkgFilter;
    // Already running with same filter AND we still hold the subscription
    // handle — true no-op. The handle check matters across Vite hot-reloads
    // and dev-server restarts: state may say "running" while the live
    // EventsOn was torn down by the module replacement.
    if (sameFilter && lcSubs.current[serial]) return;
    // Tear down stale sub (if any) and start fresh.
    if (lcSubs.current[serial]) {
      lcSubs.current[serial]();
      delete lcSubs.current[serial];
    }
    const ev = `logcat:${serial}`;
    EventsOn(ev, (entry: LogEntry) => {
      const slice = logcatsRef.current[serial];
      if (!slice || slice.paused) return;
      setLogcats(prev => {
        const s = prev[serial];
        if (!s) return prev;
        const next = s.lines.length >= LOGCAT_MAX ? s.lines.slice(s.lines.length - LOGCAT_MAX + 1) : s.lines.slice();
        next.push(entry);
        return {...prev, [serial]: {...s, lines: next}};
      });
    });
    lcSubs.current[serial] = () => EventsOff(ev);
    // Only re-spawn the backend stream when the filter actually changed.
    // Otherwise the existing tcpdump-style adb process keeps emitting into
    // the freshly-subscribed channel.
    if (!sameFilter) {
      API.StopLogcat(serial).catch(() => {});
      setLogcats(prev => ({...prev, [serial]: {lines: [], paused: false, pkgFilter, running: true}}));
      API.StartLogcat(serial, pkgFilter).catch(() => {
        setLogcats(prev => ({...prev, [serial]: {...prev[serial], running: false}}));
      });
    }
  }, []);

  const stopLogcat = useCallback((serial: string) => {
    if (lcSubs.current[serial]) { lcSubs.current[serial](); delete lcSubs.current[serial]; }
    API.StopLogcat(serial).catch(() => {});
    setLogcats(prev => ({...prev, [serial]: {...(prev[serial] || {lines: [], paused: false, pkgFilter: '', running: false}), running: false}}));
  }, []);

  const setPaused = useCallback((serial: string, paused: boolean) => {
    setLogcats(prev => ({...prev, [serial]: {...(prev[serial] || {lines: [], paused: false, pkgFilter: '', running: false}), paused}}));
  }, []);

  const clearLogcat = useCallback((serial: string) => {
    setLogcats(prev => ({...prev, [serial]: {...(prev[serial] || {lines: [], paused: false, pkgFilter: '', running: false}), lines: []}}));
    API.ClearLogcat(serial).catch(() => {});
  }, []);

  // ── shells ────────────────────────────────────────────────────────────
  const [shells, setShells] = useState<Record<string, ShellSession>>({});
  const shellSubs = useRef<Record<string, () => void>>({});

  const openShell = useCallback(async (serial: string, root: boolean): Promise<string> => {
    const id = await API.OpenShell(serial, root);
    const label = root ? `root ${id}` : id;
    // xterm.js (components/Terminal.tsx) owns the live `shell:<id>` event
    // subscription and the scrollback. Store only tracks session existence so
    // tabs can be enumerated; we no longer accumulate `buf` here.
    setShells(prev => ({...prev, [id]: {id, serial, label, root, buf: ''}}));
    return id;
  }, []);

  const writeShell = useCallback(async (id: string, data: string) => {
    await API.WriteShell(id, data);
  }, []);

  const closeShell = useCallback((id: string) => {
    if (shellSubs.current[id]) { shellSubs.current[id](); delete shellSubs.current[id]; }
    API.CloseShell(id).catch(() => {});
    setShells(prev => {
      const next = {...prev};
      delete next[id];
      return next;
    });
  }, []);

  const clearShellBuf = useCallback((id: string) => {
    setShells(prev => prev[id] ? {...prev, [id]: {...prev[id], buf: ''}} : prev);
  }, []);

  // ── captures (cross-screen) ────────────────────────────────────────────
  const [captures, setCaptures] = useState<Record<string, CaptureSlice>>({});
  const capturesRef = useRef(captures);
  capturesRef.current = captures;
  const capSubs = useRef<Record<string, () => void>>({});

  const getCapture = useCallback((serial: string): CaptureSlice => {
    return capturesRef.current[serial] || {
      active: false, packets: [], iface: 'any', bpf: '', preset: 0,
      displayFilter: '', maxPackets: 10000, state: null, rev: 0,
    };
  }, []);

  const setCaptureState = useCallback((serial: string, st: any) => {
    setCaptures(prev => ({...prev, [serial]: {...(prev[serial] || getCapture(serial)), state: st, active: !!st?.active}}));
  }, [getCapture]);

  const setCaptureDisplayFilter = useCallback((serial: string, f: string) => {
    setCaptures(prev => ({...prev, [serial]: {...(prev[serial] || getCapture(serial)), displayFilter: f}}));
  }, [getCapture]);

  const setCaptureMaxPackets = useCallback((serial: string, n: number) => {
    setCaptures(prev => {
      const cur = prev[serial] || getCapture(serial);
      const trimmed = cur.packets.length > n ? cur.packets.slice(cur.packets.length - n) : cur.packets;
      return {...prev, [serial]: {...cur, maxPackets: n, packets: trimmed}};
    });
  }, [getCapture]);

  const setCapturePreset = useCallback((serial: string, p: number, bpf: string) => {
    setCaptures(prev => ({...prev, [serial]: {...(prev[serial] || getCapture(serial)), preset: p, bpf}}));
  }, [getCapture]);

  const setCaptureIface = useCallback((serial: string, iface: string) => {
    setCaptures(prev => ({...prev, [serial]: {...(prev[serial] || getCapture(serial)), iface}}));
  }, [getCapture]);

  const startCapture = useCallback(async (serial: string, iface: string, bpf: string, preset: number, maxPackets: number, mirrorMaxBytes: number) => {
    // Tear down any prior subscription for the same serial so we don't double-
    // append packets after a "stop → start" cycle from the UI.
    if (capSubs.current[serial]) { capSubs.current[serial](); delete capSubs.current[serial]; }
    setCaptures(prev => ({...prev, [serial]: {
      ...(prev[serial] || getCapture(serial)),
      iface, bpf, preset, maxPackets,
      packets: [], rev: 0, active: true, state: null,
    }}));
    const st = await API.StartLiveCapture(serial, iface, bpf, {maxPackets, maxPcapBytes: mirrorMaxBytes});
    setCaptures(prev => ({...prev, [serial]: {...(prev[serial] || getCapture(serial)), state: st, active: !!st?.active}}));
    const ev = 'pcap:' + serial;
    EventsOn(ev, (batch: CapturePacket[]) => {
      if (!batch || !batch.length) return;
      setCaptures(prev => {
        const cur = prev[serial]; if (!cur) return prev;
        const merged = cur.packets.concat(batch);
        const cap = cur.maxPackets || 10000;
        const next = merged.length > cap ? merged.slice(merged.length - cap) : merged;
        return {...prev, [serial]: {...cur, packets: next, rev: cur.rev + 1}};
      });
    });
    capSubs.current[serial] = () => EventsOff(ev);
  }, [getCapture]);

  const stopCapture = useCallback(async (serial: string) => {
    if (capSubs.current[serial]) { capSubs.current[serial](); delete capSubs.current[serial]; }
    try { await API.StopLiveCapture(serial); } catch {}
    setCaptures(prev => prev[serial] ? {...prev, [serial]: {...prev[serial], active: false}} : prev);
  }, []);

  const clearCapture = useCallback((serial: string) => {
    setCaptures(prev => prev[serial] ? {...prev, [serial]: {...prev[serial], packets: [], rev: 0}} : prev);
  }, []);

  // ── cache ──────────────────────────────────────────────────────────────
  const cache = useRef<Record<string, CacheEntry<unknown>>>({});
  const inflight = useRef<Record<string, Promise<unknown>>>({});

  const cached = useCallback(async <T,>(key: string, ttlMs: number, fetcher: () => Promise<T>): Promise<T> => {
    const e = cache.current[key];
    if (e && (Date.now() - e.ts) < ttlMs) return e.data as T;
    const pending = inflight.current[key];
    if (pending) return pending as Promise<T>;
    const p = fetcher().then((d) => {
      cache.current[key] = {data: d, ts: Date.now()};
      delete inflight.current[key];
      return d;
    }).catch((err) => {
      delete inflight.current[key];
      throw err;
    });
    inflight.current[key] = p as Promise<unknown>;
    return p;
  }, []);

  const invalidate = useCallback((prefix: string) => {
    for (const k of Object.keys(cache.current)) {
      if (k.startsWith(prefix)) delete cache.current[k];
    }
  }, []);

  // ── queued shell command for cross-screen hand-off ──────────────────────
  const queuedCmds = useRef<Record<string, QueuedShellCmd>>({});
  const queueShellCmd = useCallback((req: QueuedShellCmd) => {
    queuedCmds.current[req.serial] = req;
  }, []);
  const consumeShellCmd = useCallback((serial: string): QueuedShellCmd | null => {
    const v = queuedCmds.current[serial];
    if (v) delete queuedCmds.current[serial];
    return v || null;
  }, []);

  // ── cleanup on unmount of the entire app ───────────────────────────────
  useEffect(() => {
    return () => {
      Object.values(lcSubs.current).forEach(off => off());
      Object.values(shellSubs.current).forEach(off => off());
      Object.values(capSubs.current).forEach(off => off());
    };
  }, []);

  const value: Store = {
    getLogcat, startLogcat, stopLogcat, setPaused, clearLogcat,
    shells, openShell, writeShell, closeShell, clearShellBuf,
    getCapture, startCapture, stopCapture, clearCapture,
    setCaptureDisplayFilter, setCaptureMaxPackets, setCaptureState, setCapturePreset, setCaptureIface,
    cached, invalidate,
    queueShellCmd, consumeShellCmd,
  };
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  void captures;
  return <StoreCtx.Provider value={value}>{children}</StoreCtx.Provider>;
}

// Convenience hooks ──────────────────────────────────────────────────────

export function useLogcat(serial: string): LogcatSlice {
  const s = useStore();
  // Subscribe via re-read; the underlying state lives in StoreProvider.
  const slice = s.getLogcat(serial);
  return slice;
}

// Force re-render when logcat lines update — accessed via a state subscription.
// Provider already triggers setLogcats which propagates via getLogcat closure
// returning fresh state on every render. To make components re-render, we
// expose a tick state derived from the store's logcat slice.
export function useLogcatLines(serial: string): LogEntry[] {
  const s = useStore();
  return s.getLogcat(serial).lines;
}
