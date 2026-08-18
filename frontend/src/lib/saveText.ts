// Saving a text buffer to disk goes through the backend, not the DOM.
//
// The Logcat, Shell and Frida panes each used to build a Blob URL and click a
// synthetic `<a download>`. That is a browser idiom; the webview adbq runs in is
// not a browser. WKWebView ignores the download attribute, so those buttons
// produced no file, no error, and a cheerful "exported" toast — the worst
// possible combination, because the user only finds out later that nothing was
// written.
//
// The backend opens a real save dialog and writes the bytes (App.SaveTextAs),
// which is what every other export in this app already does.
import * as API from '../../wailsjs/go/main/App';
import {showToast} from '../ui';

/**
 * Prompts for a location and writes `content` there.
 *
 * A cancelled dialog is silent: it is the user changing their mind, not a
 * failure, and a toast for it would train people to ignore toasts.
 */
export async function saveTextAs(opts: {
  title: string;
  suggestedName: string;
  content: string;
  /** Shown in the success toast, e.g. "1,204 lines". */
  detail?: string;
}): Promise<void> {
  try {
    const path = await API.SaveTextAs(opts.title, opts.suggestedName, opts.content);
    if (!path) return; // cancelled
    showToast({title: 'Saved', body: path, kind: 'ok', mono: true});
  } catch (e) {
    showToast({title: 'Save failed', body: String(e), kind: 'err'});
  }
}

/** `2026-08-18_141233` — sorts chronologically and is safe in a filename. */
export function fileStamp(d = new Date()): string {
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}_${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}`;
}
