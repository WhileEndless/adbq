// Mirror state is pushed, not polled.
//
// Two separate components each ran their own `ScrcpyActive` timer — one in the
// device tab, one on Overview — asking every two seconds whether a process this
// very application had started was still running. adbq witnesses the mirror
// opening and closing directly, so the backend now says so (`scrcpy:changed`)
// and both call sites just listen.
import {useEffect, useState} from 'react';
import * as API from '../../wailsjs/go/main/App';
import {EventsOn} from '../../wailsjs/runtime/runtime';

/**
 * Whether a mirror is running for this device.
 *
 * Reads the current value once on mount — the mirror may already be up from
 * before this component existed — then follows the event.
 */
export function useScrcpyActive(serial: string): boolean {
  const [active, setActive] = useState(false);
  useEffect(() => {
    if (!serial) { setActive(false); return; }
    let live = true;
    API.ScrcpyActive(serial).then(v => { if (live) setActive(v); }).catch(() => {});
    const off = EventsOn('scrcpy:changed', (s: {serial: string; active: boolean}) => {
      if (s?.serial === serial) setActive(!!s.active);
    });
    return () => { live = false; off(); };
  }, [serial]);
  return active;
}

/** Whether scrcpy is installed on this computer. Fixed for the session. */
let availablePromise: Promise<boolean> | null = null;
export function useScrcpyAvailable(): boolean | null {
  const [available, setAvailable] = useState<boolean | null>(null);
  useEffect(() => {
    // One probe per session shared by every caller: whether a binary exists on
    // this computer does not change while the app is open, and two components
    // ask.
    availablePromise ??= API.ScrcpyAvailable().catch(() => false);
    let live = true;
    availablePromise.then(v => { if (live) setAvailable(v); });
    return () => { live = false; };
  }, []);
  return available;
}
