// Cross-screen state store. Lives at App level so shell sessions, captures,
// Frida sessions and per-device caches survive navigation between screens.
// Logcat deliberately does NOT live here — see logcatStore.ts for why.
import React, {createContext, useCallback, useContext, useEffect, useRef, useState} from 'react';
import {adb} from '../wailsjs/go/models';
import * as API from '../wailsjs/go/main/App';
import {EventsOn, EventsOff} from '../wailsjs/runtime/runtime';

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

// ─── Frida live sessions ─────────────────────────────────────────────────
// Each session's message ring lives at App level so the live console keeps
// filling while the user is on another tab/screen. Wails events are
// fire-and-forget, so on (re)subscribe we backfill via GetFridaSessionLog and
// de-duplicate by the backend's monotonic seq.

export interface FridaSessionSlice {
  info: adb.FridaSessionInfo;
  messages: adb.FridaMsg[];
  lastSeq: number;
  rev: number;
  ended: boolean;
}

const FRIDA_MSG_MAX = 5000;

interface Store {
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

  // frida sessions — keyed by session id
  fridaSessions: Record<string, FridaSessionSlice>;
  startFridaSession: (serial: string, pkg: string, mode: string, runtimeVer: string, scriptIds: string[]) => Promise<adb.FridaSessionInfo>;
  adoptFridaSession: (info: adb.FridaSessionInfo) => void;
  attachFridaSession: (id: string) => void;
  stopFridaSession: (id: string) => Promise<void>;
  removeFridaSession: (id: string) => void;
  clearFridaSession: (id: string) => void;

  // generic cache with TTL
  cached: <T>(key: string, ttlMs: number, fetcher: () => Promise<T>) => Promise<T>;
  invalidate: (prefix: string) => void;

  // cross-screen hand-off: queue a command for the Shell screen to type into a
  // newly-opened session (e.g. from Apps "Open shell here").
  queueShellCmd: (req: QueuedShellCmd) => void;
  consumeShellCmd: (serial: string) => QueuedShellCmd | null;

  // cross-screen hand-off: request the Frida screen open on a specific tab
  // (e.g. Apps "Start with Frida" → land on the Sessions tab).
  requestFridaTab: (tab: string) => void;
  consumeFridaTab: () => string | null;
}

const StoreCtx = createContext<Store | null>(null);

export function useStore(): Store {
  const s = useContext(StoreCtx);
  if (!s) throw new Error('useStore outside provider');
  return s;
}

export function StoreProvider({children}: {children: React.ReactNode}) {
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

  // ── frida sessions (cross-screen) ───────────────────────────────────────
  const [fridaSessions, setFridaSessions] = useState<Record<string, FridaSessionSlice>>({});
  const fridaSessionsRef = useRef(fridaSessions);
  fridaSessionsRef.current = fridaSessions;
  const fridaSubs = useRef<Record<string, () => void>>({});

  // mergeFridaMsgs appends new messages, drops any whose seq we already hold
  // (the subscribe/backfill overlap window), keeps them seq-ordered, and rings.
  const mergeFridaMsgs = useCallback((id: string, incoming: adb.FridaMsg[]) => {
    if (!incoming || incoming.length === 0) return;
    setFridaSessions(prev => {
      const cur = prev[id];
      if (!cur) return prev;
      let maxSeq = cur.lastSeq;
      const fresh = incoming.filter(m => m.seq > cur.lastSeq);
      if (fresh.length === 0) return prev;
      for (const m of fresh) if (m.seq > maxSeq) maxSeq = m.seq;
      let merged = cur.messages.concat(fresh);
      if (merged.length > FRIDA_MSG_MAX) merged = merged.slice(merged.length - FRIDA_MSG_MAX);
      const ended = fresh.some(m => m.kind === 'detached') || cur.ended;
      return {...prev, [id]: {...cur, messages: merged, lastSeq: maxSeq, rev: cur.rev + 1, ended}};
    });
  }, []);

  // attachFridaSession wires the live event subscription and immediately
  // backfills anything emitted before we subscribed (start race) or while we
  // were detached (HMR / remount). Safe to call repeatedly.
  const attachFridaSession = useCallback((id: string) => {
    if (fridaSubs.current[id]) return;
    const ev = `frida-session:${id}`;
    // The backend batches a tick's worth of messages into one event; tolerate a
    // single object too so an older backend build still streams.
    EventsOn(ev, (m: adb.FridaMsg | adb.FridaMsg[]) => mergeFridaMsgs(id, Array.isArray(m) ? m : [m]));
    EventsOn(`${ev}:done`, (info: adb.FridaSessionInfo) => {
      setFridaSessions(prev => prev[id] ? {...prev, [id]: {...prev[id], info, ended: true, rev: prev[id].rev + 1}} : prev);
    });
    fridaSubs.current[id] = () => EventsOff(ev, `${ev}:done`);
    const since = fridaSessionsRef.current[id]?.lastSeq ?? 0;
    API.GetFridaSessionLog(id, since).then(msgs => mergeFridaMsgs(id, msgs || [])).catch(() => {});
  }, [mergeFridaMsgs]);

  // adoptFridaSession registers a session the backend already created (e.g. via
  // StartAppWithFrida's orchestration) so the store subscribes + backfills it.
  const adoptFridaSession = useCallback((info: adb.FridaSessionInfo) => {
    setFridaSessions(prev => prev[info.id] ? prev : ({...prev, [info.id]: {info, messages: [], lastSeq: 0, rev: 0, ended: false}}));
    attachFridaSession(info.id);
  }, [attachFridaSession]);

  const startFridaSession = useCallback(async (serial: string, pkg: string, mode: string, runtimeVer: string, scriptIds: string[]): Promise<adb.FridaSessionInfo> => {
    const info = await API.StartFridaSession(serial, pkg, mode, runtimeVer, scriptIds);
    adoptFridaSession(info);
    return info;
  }, [adoptFridaSession]);

  const stopFridaSession = useCallback(async (id: string) => {
    try { await API.StopFridaSession(id); } catch {}
    setFridaSessions(prev => prev[id] ? {...prev, [id]: {...prev[id], ended: true}} : prev);
  }, []);

  const removeFridaSession = useCallback((id: string) => {
    if (fridaSubs.current[id]) { fridaSubs.current[id](); delete fridaSubs.current[id]; }
    API.RemoveFridaSession(id).catch(() => {});
    setFridaSessions(prev => { const next = {...prev}; delete next[id]; return next; });
  }, []);

  const clearFridaSession = useCallback((id: string) => {
    setFridaSessions(prev => prev[id] ? {...prev, [id]: {...prev[id], messages: [], rev: prev[id].rev + 1}} : prev);
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

  // ── requested Frida tab for cross-screen hand-off ───────────────────────
  const fridaTabReq = useRef<string | null>(null);
  const requestFridaTab = useCallback((tab: string) => { fridaTabReq.current = tab; }, []);
  const consumeFridaTab = useCallback((): string | null => {
    const v = fridaTabReq.current;
    fridaTabReq.current = null;
    return v;
  }, []);

  // ── cleanup on unmount of the entire app ───────────────────────────────
  useEffect(() => {
    return () => {
      Object.values(shellSubs.current).forEach(off => off());
      Object.values(capSubs.current).forEach(off => off());
      Object.values(fridaSubs.current).forEach(off => off());
    };
  }, []);

  const value: Store = {
    shells, openShell, writeShell, closeShell, clearShellBuf,
    getCapture, startCapture, stopCapture, clearCapture,
    setCaptureDisplayFilter, setCaptureMaxPackets, setCaptureState, setCapturePreset, setCaptureIface,
    fridaSessions, startFridaSession, adoptFridaSession, attachFridaSession, stopFridaSession, removeFridaSession, clearFridaSession,
    cached, invalidate,
    queueShellCmd, consumeShellCmd,
    requestFridaTab, consumeFridaTab,
  };
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  void captures;
  return <StoreCtx.Provider value={value}>{children}</StoreCtx.Provider>;
}
