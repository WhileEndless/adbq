// Stale-while-revalidate cache for request/response device data. A module-level
// store (survives screen navigation/unmount) lets a screen render its last
// cached value instantly, then quietly revalidate in the background and update
// if anything changed. The `refreshing` flag drives a spinner on Reload buttons
// so the user can see a background refresh is happening. `prefetchData` warms
// the cache when a device connects, before any screen is opened.
import {useCallback, useEffect, useRef, useState} from 'react';
import {EventsOn} from '../wailsjs/runtime/runtime';

interface Entry<T> {
  data?: T;
  ts: number;     // last successful fetch time
  loading: boolean;
  error?: unknown;
}

// ─── Cache domains ────────────────────────────────────────────────────────
//
// Only the backend knows that an install finished or a forward was removed, so
// it is the backend that decides this cache is stale: every mutating binding
// declares the domains it dirties and emits `cache:invalidate` (see
// app_invalidate.go and internal/adb/cachedomain.go). Without that the
// frontend would have to guess, which in practice means it never invalidates —
// which is exactly the state this replaced.
//
// The contract that makes it work is the key shape:
//
//     <domain>:<serial>[:...anything]
//
// Build keys with deviceKey()/hostKey() rather than by hand, so a screen cannot
// invent a key no invalidation can reach.

export type Domain =
  | 'apps' | 'storage' | 'files' | 'net' | 'forwards' | 'iptables'
  | 'certs' | 'hosts' | 'frida' | 'tcpdump' | 'props' | 'root' | 'proxy'
  | 'sdk' | 'jadx' | 'scrcpy' | 'avd';

/** Key for per-device state. Parts are joined with ':' after the serial. */
export function deviceKey(domain: Domain, serial: string, ...parts: string[]): string {
  return [domain, serial, ...parts].join(':');
}

/** Key for host-scoped state (SDK, jadx, AVDs) — no serial, so an empty one. */
export function hostKey(domain: Domain, ...parts: string[]): string {
  return [domain, '', ...parts].join(':');
}

const store = new Map<string, Entry<unknown>>();
const listeners = new Map<string, Set<() => void>>();
const inflight = new Map<string, Promise<unknown>>();
// The last fetcher seen for a key, so an invalidation arriving from the backend
// can refresh a key that a component is watching without that component having
// to notice and re-ask. Without this, "invalidate" could only ever mean
// "delete", and a mounted screen would go on showing the value it already had.
const fetchers = new Map<string, () => Promise<unknown>>();

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
  fetchers.set(key, fetcher as () => Promise<unknown>);
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

/**
 * Promise-shaped read for callers that cannot use the hook — an effect keyed on
 * something other than the component's identity, say, or a one-off fetch inside
 * an event handler. Returns the cached value when it is fresher than staleMs,
 * otherwise fetches (deduplicating concurrent callers).
 *
 * This is what the old `store.cached` did, moved here so there is exactly one
 * cache. Two caches meant two TTLs for one key and no way for the backend to
 * invalidate either reliably.
 */
export async function getOrFetch<T>(key: string, fetcher: () => Promise<T>, staleMs: number): Promise<T | undefined> {
  const e = store.get(key);
  if (e && !e.error && e.data !== undefined && Date.now() - e.ts < staleMs) {
    return e.data as T;
  }
  return revalidate(key, fetcher);
}

// invalidateData drops cached entries whose key starts with prefix.
export function invalidateData(prefix: string): void {
  for (const k of Array.from(store.keys())) {
    if (k.startsWith(prefix)) store.delete(k);
  }
}

/**
 * Drops every cached entry in `domains` for `serial`, and re-fetches the ones a
 * component is currently showing.
 *
 * Deleting alone is not enough. A mounted screen holds no subscription to a key
 * that no longer exists, so nothing would tell it to refetch and it would keep
 * rendering the value it captured — the user uninstalls an app and the list sits
 * there looking correct. So live keys are revalidated rather than dropped;
 * unobserved ones are simply deleted and will be fetched fresh on next use.
 */
export function invalidateDomains(serial: string, domains: readonly Domain[]): void {
  if (!domains?.length) return;
  const prefixes = domains.map(d => `${d}:${serial}`);
  for (const key of Array.from(store.keys())) {
    // Match `<domain>:<serial>` exactly or as a `<domain>:<serial>:…` prefix, so
    // invalidating serial "R58" cannot also clear "R58M12".
    const hit = prefixes.some(p => key === p || key.startsWith(p + ':'));
    if (!hit) continue;
    if (listeners.get(key)?.size) {
      const fetcher = fetchers.get(key);
      if (fetcher) {
        void revalidate(key, fetcher);
        continue;
      }
    }
    store.delete(key);
    notify(key);
  }
}

// The backend is the authority on staleness; this is the wire it speaks over.
// Subscribed at module scope rather than from a component: an invalidation that
// arrives while the relevant screen is unmounted still has to land, or the
// screen shows a stale value the moment it mounts again.
EventsOn('cache:invalidate', (payload: {serial?: string; domains?: Domain[]}) => {
  invalidateDomains(payload?.serial ?? '', payload?.domains ?? []);
});

/**
 * Fallback staleness for callers that do not specify one.
 *
 * Deliberately short. The savings in this app come from long TTLs on facts that
 * genuinely cannot change (see the volatility classes in docs/performance.md),
 * each declared at its call site alongside the backend invalidation that keeps
 * it honest. Making the *default* long would extend that trust to keys nobody
 * reasoned about.
 */
export const DEFAULT_STALE_MS = 4000;

export interface DeviceData<T> {
  data?: T;
  loading: boolean;     // first load, no cached value to show yet
  refreshing: boolean;  // background revalidation while cached data is shown
  error?: unknown;
  refresh: () => void;  // force a revalidation now
  /**
   * When the shown value was fetched, or 0 if there is none yet.
   *
   * Exposed so screens can say how old their data is. Values here are cached
   * for minutes now, not seconds, and that is only reasonable while the user
   * can see it: visible age plus a Refresh button turns a long TTL from
   * something done to them into something they can override. Without it, a
   * stale screen is indistinguishable from a wrong one.
   */
  fetchedAt: number;
}

// useDeviceData returns the cached value immediately (if any) and revalidates in
// the background when it's stale. Pass key=null to disable (e.g. no device).
export function useDeviceData<T>(key: string | null, fetcher: () => Promise<T>, opts?: {staleMs?: number}): DeviceData<T> {
  // A caller that does not say how volatile its data is gets a conservative
  // default. Long TTLs belong to keys whose owner has thought about what makes
  // them stale and declared it on the backend — not to everything by accident.
  const staleMs = opts?.staleMs ?? DEFAULT_STALE_MS;
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
    // "key set but no entry yet" counts as loading so screens show a spinner on
    // the first frame (before the effect kicks off the fetch) rather than a
    // one-render flash of the empty state.
    loading: (!!key && !e) || (!!e?.loading && !hasData),
    refreshing: !!e?.loading && hasData,
    error: e?.error,
    refresh,
    fetchedAt: hasData ? (e?.ts ?? 0) : 0,
  };
}
