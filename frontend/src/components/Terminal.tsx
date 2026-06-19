// TerminalView — xterm.js-backed terminal that survives screen navigation.
//
// Terminal instances live in a module-level Map keyed by sessionId so the
// scrollback isn't lost when the user switches to Logcat and back. Mounting
// this component into the DOM just attaches an existing Terminal (or creates
// one if it's the first time). Unmounting detaches but does NOT dispose.
//
// Input/output:
//   - `term.onData(d)` → API.WriteShell(sessionId, d)  (keystrokes to device)
//   - EventsOn('shell:<id>', d) → term.write(d)        (PTY output to user)
//   - `term.onResize` → API.ResizeShell(id, cols, rows)

import React, {useEffect, useRef} from 'react';
import {Terminal, ITerminalOptions} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import * as API from '../../wailsjs/go/main/App';
import {EventsOn, EventsOff} from '../../wailsjs/runtime/runtime';

interface TermSlot {
  term: Terminal;
  fit: FitAddon;
  detach: () => void;
  // Whether xterm's open() has been called on this terminal yet. Lives at the
  // module level (not on the React component) so that a Shell screen unmount
  // + remount doesn't lose the bound state and force a second open() on the
  // same Terminal — which xterm.js does not support and produces a blank
  // canvas.
  bound: boolean;
  history?: string;   // optional scrollback we replayed at create-time
}

const terminals = new Map<string, TermSlot>();

const defaultOpts: ITerminalOptions = {
  fontFamily: '"JetBrains Mono", ui-monospace, "SFMono-Regular", Menlo, monospace',
  fontSize: 12.5,
  lineHeight: 1.2,
  cursorBlink: true,
  cursorStyle: 'bar',
  scrollback: 5000,
  allowProposedApi: true,
  convertEol: false,
  theme: {
    background: '#0a0a0e',
    foreground: '#e7e7ee',
    cursor: '#a07cf7',
    cursorAccent: '#0a0a0e',
    selectionBackground: 'rgba(160,124,247,0.35)',
    black:   '#3d3d3d', red:     '#ec6a73', green:   '#5ed29a', yellow:  '#e9b454',
    blue:    '#6fb3ff', magenta: '#d472f0', cyan:    '#5ed2c0', white:   '#d0d0d8',
    brightBlack:   '#5f5f6c', brightRed:     '#ff8b94', brightGreen:   '#7fe3b0',
    brightYellow:  '#ffd070', brightBlue:    '#90c0ff', brightMagenta: '#e0a0ff',
    brightCyan:    '#80e0d0', brightWhite:   '#ffffff',
  },
};

/** Returns the persistent Terminal for the session id, creating it on first use. */
export function getOrCreateTerm(sessionId: string, opts?: {root?: boolean; serial?: string}): Terminal {
  const existing = terminals.get(sessionId);
  if (existing) return existing.term;

  const term = new Terminal(defaultOpts);
  const fit = new FitAddon();
  term.loadAddon(fit);
  const ev = `shell:${sessionId}`;
  EventsOn(ev, (data: string) => term.write(data));

  // Forward keystrokes to backend.
  term.onData((data: string) => {
    API.WriteShell(sessionId, data).catch(() => {});
  });

  // Forward resize to backend so the device-side shell redraws.
  term.onResize(({cols, rows}) => {
    API.ResizeShell(sessionId, cols, rows).catch(() => {});
  });

  // Intercept Ctrl+L to emulate bash's "push prompt to top of viewport"
  // without wiping scrollback. xterm's default would forward \x0c to the
  // device shell; mksh on Android ignores it, so nothing visible happens.
  // Writing N-1 newlines locally rolls existing content up into scrollback
  // (preserved), and the next prompt naturally appears at the top.
  term.attachCustomKeyEventHandler((e) => {
    if (e.type === 'keydown' && e.ctrlKey && !e.metaKey && !e.altKey && e.key.toLowerCase() === 'l') {
      // Standard bash Ctrl+L semantics, faithfully reproduced:
      //  1) Push the current visible viewport into scrollback by emitting
      //     `rows-1` blank lines. xterm scrolls existing content up — it is
      //     PRESERVED in scrollback (user can scroll up to see it).
      //  2) Move the cursor to the top-left of the now-empty viewport so the
      //     device's next prompt lands at the top, not the bottom.
      //  3) Nudge the device shell to repaint its prompt + current input
      //     buffer via \x0c. mksh ignores it; bash redraws. Either way the
      //     visible result matches "fresh open at top, history above".
      const rows = term.rows;
      term.write('\r\n'.repeat(Math.max(0, rows - 1)));
      term.write('\x1b[H');
      API.WriteShell(sessionId, '\x0c').catch(() => {});
      return false;
    }
    return true;
  });

  const slot: TermSlot = {
    term,
    fit,
    detach: () => { EventsOff(ev); },
    bound: false,
  };
  terminals.set(sessionId, slot);
  return term;
}

/** Best-effort terminal disposal; called when the user closes the session. */
export function disposeTerm(sessionId: string) {
  const slot = terminals.get(sessionId);
  if (!slot) return;
  slot.detach();
  try { slot.term.dispose(); } catch {}
  terminals.delete(sessionId);
}

/** Returns the live Terminal for a session id, or null. Used by parent screens
 *  to call .clear() / .focus() / read buffer without going through React props. */
export function getTerm(sessionId: string): Terminal | null {
  return terminals.get(sessionId)?.term ?? null;
}

/** Dumps the visible + scrollback buffer of a session as plain text. */
export function dumpTerm(sessionId: string): string {
  const t = getTerm(sessionId);
  if (!t) return '';
  const lines: string[] = [];
  const b = t.buffer.active;
  for (let i = 0; i < b.length; i++) lines.push(b.getLine(i)?.translateToString(true) ?? '');
  return lines.join('\n');
}

/** Pre-fill the terminal with persisted scrollback (one-shot per session). */
export function replayScrollbackInto(sessionId: string, history: string) {
  const slot = terminals.get(sessionId);
  if (!slot || slot.history) return; // already replayed
  slot.history = history;
  if (history) slot.term.write(history);
}

interface Props {
  sessionId: string;
  root?: boolean;
  serial?: string;
  /** Whether this terminal is the currently-visible tab. When it flips from
   *  false to true we refit (the host had zero size while hidden). */
  visible?: boolean;
}

export function TerminalView({sessionId, root, serial, visible}: Props) {
  const hostRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!hostRef.current) return;
    const term = getOrCreateTerm(sessionId, {root, serial});
    const slot = terminals.get(sessionId)!;

    // xterm.js Terminal.open() is one-shot: it creates internal DOM children
    // and is not designed to be called twice on the same terminal. For the
    // FIRST mount we call it normally. For SUBSEQUENT mounts (after the user
    // navigated away from Shell and back, which fully unmounts this component
    // and creates a new host div), we instead re-parent xterm's existing DOM
    // tree (`term.element`) into the new host with appendChild — this is a
    // standard DOM move that preserves xterm's internal references AND its
    // rendered scrollback content.
    if (!slot.bound) {
      term.open(hostRef.current);
      slot.bound = true;
    } else if (term.element && term.element.parentElement !== hostRef.current) {
      hostRef.current.appendChild(term.element);
    }
    if (visible !== false) term.focus();

    // Fit to the host element using xterm's official FitAddon. It reads the
    // actual rendered char metrics from xterm's CharSizeService, so cols/rows
    // are pixel-accurate and don't drift from the canvas. Two rAFs give the
    // renderer time to lay itself out before measurement.
    const fit = () => {
      try { slot.fit.fit(); } catch {}
    };
    requestAnimationFrame(() => requestAnimationFrame(fit));
    const ro = new ResizeObserver(() => fit());
    ro.observe(hostRef.current);

    return () => {
      ro.disconnect();
      // Don't dispose the terminal — keep it alive in the Map for tab switches.
    };
  }, [sessionId]);

  // When the tab becomes visible after being hidden, the host went from 0×0
  // to its real size. Force-refit and refocus on that transition.
  useEffect(() => {
    if (visible === false) return;
    const slot = terminals.get(sessionId);
    if (!slot) return;
    requestAnimationFrame(() => requestAnimationFrame(() => {
      try { slot.fit.fit(); } catch {}
      slot.term.focus();
    }));
  }, [visible, sessionId]);

  return (
    <div
      ref={hostRef}
      style={{
        flex: 1,
        minHeight: 0,
        minWidth: 0,
        // No internal padding here: FitAddon measures the parent element
        // directly. The xterm.css gives the inner .xterm-screen its own gutter.
        padding: 0,
        background: '#0a0a0e',
        overflow: 'hidden',
      }}
    />
  );
}
