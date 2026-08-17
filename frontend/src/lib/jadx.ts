import {adb} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {confirmDialog, showToast} from '../ui';

// Resolving jadx probes the file system and runs `java -version`, which is too
// much to repeat every time a different app is selected. The answer only changes
// when the user downloads, removes or repoints the tool, so it is cached until
// one of those happens.
let cached: Promise<adb.JadxInfo> | null = null;

export function jadxInfo(force = false): Promise<adb.JadxInfo> {
  if (force || !cached) {
    cached = API.JadxInfo().catch(e => {
      cached = null;
      throw e;
    });
  }
  return cached;
}

export function invalidateJadxInfo() {
  cached = null;
}

// ensureJadx returns an installation that can actually run, asking for consent
// to download one when there is none. Returns null when the user declined or
// when nothing usable could be resolved — the caller then does nothing.
//
// The disclosures come from the backend so this dialog cannot understate what is
// about to be fetched.
export async function ensureJadx(): Promise<adb.JadxInfo | null> {
  let info: adb.JadxInfo;
  try {
    info = await jadxInfo();
  } catch (e) {
    showToast({title: 'Could not check for jadx', body: String(e), kind: 'err'});
    return null;
  }

  if (!info.installed) {
    const ok = await confirmDialog({
      title: 'Download jadx?',
      body: [
        ...(info.disclosures ?? []),
        '',
        `Source: ${info.asset}`,
        `SHA-256: ${info.sha256}`,
      ].join('\n'),
      confirmLabel: 'Download and verify',
    });
    if (!ok) return null;
    try {
      info = await API.DownloadJadx();
      invalidateJadxInfo();
      cached = Promise.resolve(info);
    } catch (e) {
      showToast({title: 'Download failed', body: String(e), kind: 'err'});
      return null;
    }
  }

  if (!info.java) {
    showToast({
      title: 'jadx needs a Java runtime',
      body: info.javaError || 'No Java runtime was found. Set one in Settings.',
      kind: 'err',
      ttl: 9000,
    });
    return null;
  }
  return info;
}

// jadxLabel describes an installation in one line, for status rows.
export function jadxLabel(info: adb.JadxInfo | null): string {
  if (!info?.installed) return 'not installed';
  const parts = ['jadx'];
  if (info.version) parts.push(info.version);
  parts.push(info.kind === 'managed' ? '(downloaded by adbq)' : '(your own install)');
  return parts.join(' ');
}
