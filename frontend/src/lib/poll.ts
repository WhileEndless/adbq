// Shared polling primitives.
//
// adbq is a wrapper around a CLI, so every poll costs a process. Nine separate
// `setInterval`s were spread across the screens, none of them gated on anything
// — they ran while the window was minimised, while the user was on another
// screen, and while the thing they were watching was not moving. The Emulators
// screen had already worked out the right shape (poll only while something is
// mid-transition, and refresh on the task event instead); this generalises it
// and adds the gate the original lacked.
import {useEffect, useRef, useState} from 'react';
import {adb} from '../../wailsjs/go/models';
import {EventsOn} from '../../wailsjs/runtime/runtime';

/**
 * Whether the window is currently visible.
 *
 * A desktop app that keeps asking a phone questions while minimised is burning
 * the user's battery to compute something nobody is looking at. The Page
 * Visibility API reports hidden when the window is minimised or fully occluded,
 * which is the case worth catching; a window that is merely unfocused but on
 * screen still counts as visible, and should keep updating.
 */
export function useVisible(): boolean {
  const [visible, setVisible] = useState(() =>
    typeof document === 'undefined' ? true : !document.hidden);
  useEffect(() => {
    const onChange = () => {
      const v = !document.hidden;
      setVisible(v);
      // Mirror the state onto the body so CSS can pause the indefinite
      // animations too — an infinite keyframe keeps the compositor ticking
      // even when there is nothing to see.
      document.body.classList.toggle('hidden-window', !v);
    };
    onChange();
    document.addEventListener('visibilitychange', onChange);
    return () => document.removeEventListener('visibilitychange', onChange);
  }, []);
  return visible;
}

/**
 * Calls `fn` every `ms` while `active` and the window is visible.
 *
 * The callback is held in a ref, so a caller may pass a fresh closure on every
 * render without restarting the timer — which would otherwise reset the
 * interval continuously and, for a poll slower than the render rate, mean it
 * never fires at all.
 *
 * Nothing is called on mount: a poll is for keeping a value fresh, and the
 * initial read belongs to whatever loads the screen. Callers that want both
 * should do their load in an effect and let this take over.
 */
export function usePoll(fn: () => void, ms: number, active = true) {
  const saved = useRef(fn);
  saved.current = fn;
  const visible = useVisible();
  useEffect(() => {
    if (!active || !visible) return;
    const t = setInterval(() => saved.current(), ms);
    return () => clearInterval(t);
  }, [active, visible, ms]);
}

/**
 * Runs `onDone` when a background task of one of these kinds stops running.
 *
 * Installing an image, rooting an AVD or exporting an APK happens in the task
 * tray, so without this the screen that started the work keeps showing the
 * state from before it. Cheaper and more immediate than polling for the
 * outcome, which is the point.
 *
 * The unsubscribe function EventsOn returns is what gets called on unmount —
 * EventsOff would take the task tray's own listener down with it.
 */
export function useTaskDone(kinds: string, onDone: () => void) {
  const saved = useRef(onDone);
  saved.current = onDone;
  useEffect(() => {
    const wanted = new Set(kinds.split(' ').filter(Boolean));
    return EventsOn('task:update', (t: adb.TaskState) => {
      if (wanted.has(t.kind) && t.status !== 'running') saved.current();
    });
  }, [kinds]);
}
