// Stale-while-revalidate cache for request/response device data. A module-level
// store (survives screen navigation/unmount) lets a screen render its last
// cached value instantly, then quietly revalidate in the background and update
// if anything changed. The `refreshing` flag drives a spinner on Reload buttons
// so the user can see a background refresh is happening. `prefetchData` warms
// the cache when a device connects, before any screen is opened.
import {useCallback, useEffect, useRef, useState} from 'react';

interface Entry<T> {
  data?: T;
  ts: number;     // last successful fetch time
  loading: boolean;
  error?: unknown;
}

const store = new Map<string, Entry<unknown>>();
const listeners = new Map<string, Set<() => void>>();
const inflight = new Map<string, Promise<unknown>>();

function notify(key: string) {
  listeners.get(key)?.forEach(cb => cb());
}

function subscribe(key: string, cb: () => void): () => void {
  let set = listeners.get(key);
  if (!set) { set = new Set(); listeners.set(key, set); }
  set.add(cb);
  return () => { set!.delete(cb); if (set!.size === 0) listeners.delete(key); };
}

// revalidate fetches fresh data, updates the store, and notifies subscribers.
// Concurrent calls for the same key share one in-flight request.
function revalidate<T>(key: string, fetcher: () => Promise<T>): Promise<T | undefined> {
  const existing = inflight.get(key);
  if (existing) return existing as Promise<T | undefined>;
  const prev = store.get(key);
  store.set(key, {...(prev || {ts: 0}), loading: true, error: undefined});
  notify(key);
  const p = fetcher()
    .then(data => {
      store.set(key, {data, ts: Date.now(), loading: false});
      inflight.delete(key);
      notify(key);
      return data;
    })
    .catch(err => {
      const cur = store.get(key);
      store.set(key, {...(cur || {ts: 0}), loading: false, error: err});
      inflight.delete(key);
      notify(key);
      return undefined;
    });
  inflight.set(key, p);
  return p;
}

// prefetchData warms a cache entry if it's missing or older than staleMs. Safe
// to call eagerly (e.g. on device connect) — it never throws and dedups.
export function prefetchData<T>(key: string, fetcher: () => Promise<T>, staleMs = 8000): void {
  const e = store.get(key);
  if (e && !e.error && Date.now() - e.ts < staleMs) return;
  void revalidate(key, fetcher);
}

// mutateData writes a known-fresh value into the cache and notifies subscribers
// — used after a mutation that already returns the updated data, to avoid a
// redundant re-fetch while still keeping the cache (and every subscriber) in sync.
export function mutateData<T>(key: string, data: T): void {
  store.set(key, {data, ts: Date.now(), loading: false});
  notify(key);
}

// getCached reads a cached value without subscribing — handy to seed a
// component's initial state (e.g. a polling dashboard) for an instant first paint.
export function getCached<T>(key: string): T | undefined {
  return store.get(key)?.data as T | undefined;
}

// invalidateData drops cached entries whose key starts with prefix.
export function invalidateData(prefix: string): void {
  for (const k of Array.from(store.keys())) {
    if (k.startsWith(prefix)) store.delete(k);
  }
}

export interface DeviceData<T> {
  data?: T;
  loading: boolean;     // first load, no cached value to show yet
  refreshing: boolean;  // background revalidation while cached data is shown
  error?: unknown;
  refresh: () => void;  // force a revalidation now
}

// useDeviceData returns the cached value immediately (if any) and revalidates in
// the background when it's stale. Pass key=null to disable (e.g. no device).
export function useDeviceData<T>(key: string | null, fetcher: () => Promise<T>, opts?: {staleMs?: number}): DeviceData<T> {
  const staleMs = opts?.staleMs ?? 4000;
  const [, force] = useState(0);
  const rerender = useCallback(() => force(n => n + 1), []);
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;

  useEffect(() => {
    if (!key) return;
    const unsub = subscribe(key, rerender);
    const e = store.get(key);
    if (!e || Date.now() - e.ts >= staleMs) {
      void revalidate(key, () => fetcherRef.current());
    }
    return unsub;
  }, [key, staleMs, rerender]);

  const refresh = useCallback(() => {
    if (key) void revalidate(key, () => fetcherRef.current());
  }, [key]);

  const e = key ? (store.get(key) as Entry<T> | undefined) : undefined;
  const hasData = e?.data !== undefined;
  return {
    data: e?.data,
    loading: !!e?.loading && !hasData,
    refreshing: !!e?.loading && hasData,
    error: e?.error,
    refresh,
  };
}
