import * as API from '../../wailsjs/go/main/App';

// In-memory cache of the SDK level → "Android X (Codename)" map. Lazily filled
// from the Go side on first use; the table is small and rarely changes.
let cache: Record<string, string> | null = null;
let inflight: Promise<Record<string, string>> | null = null;

async function ensureMap(): Promise<Record<string, string>> {
  if (cache) return cache;
  if (inflight) return inflight;
  inflight = API.AndroidVersionMap().then(m => {
    cache = m || {};
    inflight = null;
    return cache;
  }).catch(() => {
    inflight = null;
    cache = {};
    return cache;
  });
  return inflight;
}

// Prefetch — call once at app start so labels render synchronously.
export function prefetchAndroidVersionMap() { void ensureMap(); }

// androidVersionForSdk returns the human label for an SDK level string like
// "33". Returns the input verbatim when unknown or the map hasn't loaded yet.
export function androidVersionForSdk(level: string | undefined): string {
  if (!level) return '';
  if (cache && cache[level]) return cache[level];
  // kick off load for next render
  void ensureMap();
  return '';
}

// sdkLabel composes "33 — Android 13 (Tiramisu)" when the label is known,
// or just "33" otherwise. Designed for inline display in detail panels.
export function sdkLabel(level: string | undefined): string {
  if (!level) return '';
  const name = androidVersionForSdk(level);
  return name ? `${level} — ${name}` : level;
}
