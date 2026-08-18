import React, {useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState} from 'react';
import {adb} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {Icon} from '../icons';
import {Combobox, CommandChip, IconBtn, SearchInput, showToast} from '../ui';
import {useStore} from '../store';
import {logcatStore, useLogcat} from '../logcatStore';
import {SEARCH_DEBOUNCE_MS, highlight} from '../lib/logSearch';
import {fileStamp, saveTextAs} from '../lib/saveText';
import {deviceKey as cacheKey, getOrFetch} from '../cache';
import {APPS_STALE_MS} from '../App';
import {LogEntry} from '../types';

const LEVELS = ['V', 'D', 'I', 'W', 'E', 'F'] as const;
const LEVEL_ORDER: Record<string, number> = {V: 0, D: 1, I: 2, W: 3, E: 4, F: 5};
const LEVEL_NAMES: Record<string, string> = {
  V: 'Verbose', D: 'Debug', I: 'Info', W: 'Warn', E: 'Error', F: 'Fatal',
};

// Row geometry, kept in sync with `.logrow { height: 22px }` in styles.css.
// The list is windowed: only the rows intersecting the viewport (plus a small
// overscan) exist in the DOM, so a full 5000-line buffer costs the same to
// render as a screenful.
const ROW_H = 22;
const OVERSCAN = 12;

export function LogcatScreen({device}: {device: adb.Device}) {
  const store = useStore();
  const state = useLogcat(device.id);
  const lines = logcatStore.getLines(device.id);
  const [searchInput, setSearchInput] = useState('');
  const [search, setSearch] = useState('');
  const [levelMin, setLevelMin] = useState<string>('V');
  const [tail, setTail] = useState(true);
  // Hide lines an app repeats within seconds of each other. On by default; the
  // repeats are kept in the buffer either way, so switching it off brings them
  // straight back.
  const [collapse, setCollapse] = useState(true);
  const [apps, setApps] = useState<adb.App[]>([]);
  // At most one row is expanded at a time. It is tracked by object identity,
  // not by a content key: chatty apps repeat the same line verbatim, and any
  // content-derived key would expand every copy at once and throw off the
  // height arithmetic below.
  const [expanded, setExpanded] = useState<LogEntry | null>(null);
  const [expandedH, setExpandedH] = useState(0);
  const [scrollTop, setScrollTop] = useState(0);
  const [viewportH, setViewportH] = useState(0);
  const wrap = useRef<HTMLDivElement>(null);
  const expandedRow = useRef<HTMLDivElement>(null);
  // Whether the list is parked at its newest line. Starts true: a fresh mount
  // renders the tail.
  const atBottom = useRef(true);
  // Line count at the moment auto-scroll was switched off, so the jump button
  // can report how much has arrived since.
  const missedFrom = useRef(0);

  // Make sure a feed exists for this device and filter. The backend is the one
  // that decides whether anything needs (re)starting, so this is safe to call
  // on every mount — including StrictMode's double mount.
  useEffect(() => {
    logcatStore.ensure(device.id, logcatStore.getState(device.id).pkgFilter);
    // While this screen is mounted the backend delivers; when it is not, the
    // stream keeps running but stops emitting. Also covers the window being
    // minimised — there is nothing to render into either way.
    logcatStore.setVisible(device.id, true);
    return () => logcatStore.setVisible(device.id, false);
  }, [device.id]);

  // The logcat invocation actually feeding this pane, pid filter and all. Only
  // the backend knows which PID it attached to, so it renders the line
  // (CLAUDE.md §4.1 K3) — re-read when the filter changes, and once shortly
  // after, because attaching to a package that has just started takes a moment.
  const [cmds, setCmds] = useState<adb.StreamCommands | null>(null);
  useEffect(() => {
    if (!device?.id) return;
    let live = true;
    const read = () => API.LogcatCommands(device.id)
      .then(c => { if (live) setCmds(c); })
      .catch(() => { if (live) setCmds(null); });
    read();
    const t = setTimeout(read, 1500);
    return () => { live = false; clearTimeout(t); };
  }, [device.id, state.pkgFilter]);

  useEffect(() => {
    if (!device?.id) return;
    // Same key and TTL as the Apps screen and the sidebar badge. This used to
    // be a second TTL over the same key, so whichever screen loaded first won.
    getOrFetch(cacheKey('apps', device.id, 'user'), () => API.ListApps(device.id, true), APPS_STALE_MS)
      .then(list => setApps(list || [])).catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [device?.id]);

  useEffect(() => {
    const t = setTimeout(() => setSearch(searchInput), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(t);
  }, [searchInput]);

  // Track the scroll viewport so the window can be computed without reading
  // layout during render.
  useEffect(() => {
    const el = wrap.current;
    if (!el) return;
    const measure = () => setViewportH(el.clientHeight);
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // Repeat collapsing only applies to a single app's log — see the store.
  const collapsible = !!state.pkgFilter;
  const collapsing = collapsible && collapse;

  const {filtered, collapsed} = useMemo(() => {
    const q = search.toLowerCase();
    const min = LEVEL_ORDER[levelMin];
    const out: LogEntry[] = [];
    let hidden = 0;
    for (const l of lines) {
      if (collapsing && l.dup) { hidden++; continue; }
      // An unrecognised priority letter is shown at every threshold rather
      // than silently dropped: the level filter exists to hide noise the user
      // understands, not lines we failed to classify.
      const order = LEVEL_ORDER[l.lvl];
      if (order !== undefined && order < min) continue;
      if (q && !l.tag.toLowerCase().includes(q) && !l.msg.toLowerCase().includes(q)) continue;
      out.push(l);
    }
    return {filtered: out, collapsed: hidden};
    // `lines` is a ring buffer mutated in place — `state.version` is what
    // actually signals a change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lines, state.version, levelMin, search, collapsing]);

  const expandedIdx = useMemo(
    () => (expanded ? filtered.indexOf(expanded) : -1),
    [expanded, filtered],
  );
  const extra = expandedIdx >= 0 ? expandedH : 0;
  const totalH = filtered.length * ROW_H + extra;

  // Window bounds.
  //
  // While tailing and parked at the bottom, the window is pinned to the END of
  // the list rather than derived from scrollTop. That is not just an
  // optimisation: on a fresh mount (returning to this screen) the viewport has
  // not been measured and no scroll event has fired yet, so a scroll-derived
  // window renders rows 0..60 while the container is programmatically scrolled
  // to the bottom — a blank pane until something else nudges it.
  const rowsPerScreen = Math.max(20, Math.ceil((viewportH || 600) / ROW_H));
  const {first, last, padTop} = useMemo(() => {
    // offsetOf: rows below the expanded one are pushed down by its extra
    // height, so the padding that positions the window has to account for it —
    // otherwise expanding a long stack trace paints the whole window too high.
    const offsetOf = (i: number) => i * ROW_H + (expandedIdx >= 0 && expandedIdx < i ? extra : 0);
    if (tail && !state.paused && atBottom.current) {
      const l = filtered.length;
      const f = Math.max(0, l - rowsPerScreen - OVERSCAN * 2);
      return {first: f, last: l, padTop: offsetOf(f)};
    }
    let top = scrollTop;
    if (expandedIdx >= 0 && top > expandedIdx * ROW_H) top = Math.max(expandedIdx * ROW_H, top - extra);
    const f = Math.max(0, Math.floor(top / ROW_H) - OVERSCAN);
    const l = Math.min(filtered.length, Math.ceil(top / ROW_H) + rowsPerScreen + OVERSCAN);
    return {first: f, last: l, padTop: offsetOf(f)};
  }, [tail, state.paused, scrollTop, rowsPerScreen, filtered.length, expandedIdx, extra]);

  // Auto-scroll to the newest line. useLayoutEffect so the jump happens in the
  // same frame the rows are painted — with useEffect the list visibly flickers
  // at its old offset first.
  useLayoutEffect(() => {
    if (!wrap.current || !tail || state.paused) return;
    wrap.current.scrollTop = wrap.current.scrollHeight;
  }, [state.version, filtered.length, tail, state.paused, padTop]);

  // Measure the expanded row. A ResizeObserver rather than a one-shot read:
  // the row wraps, so its height changes when the window is resized, and a
  // stale measurement corrupts every offset below it.
  useLayoutEffect(() => {
    const el = expandedRow.current;
    if (expandedIdx < 0 || !el) { setExpandedH(0); return; }
    const measure = () => setExpandedH(Math.max(0, el.offsetHeight - ROW_H));
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [expandedIdx, expanded, search]);

  function setPkgFilter(pkg: string) {
    logcatStore.setFilter(device.id, pkg);
    setExpanded(null);
  }

  // How much arrived while the reader was scrolled away, so the jump button can
  // say whether it is worth pressing.
  const missed = tail ? 0 : Math.max(0, filtered.length - missedFrom.current);

  const resumeTail = useCallback(() => {
    atBottom.current = true;
    setTail(true);
    if (wrap.current) wrap.current.scrollTop = wrap.current.scrollHeight;
  }, []);

  const visible = filtered.slice(first, last);

  return (
    <div className='screen'>
      <div className='screen-header'>
        <h1>Logcat <span className='subtitle mono'>{filtered.length} / {lines.length} lines</span></h1>
        <div className='spacer' style={{flex: 1}}/>
        <Combobox value={state.pkgFilter} onChange={setPkgFilter} width={240} clearable
                  items={[{value: '', label: 'All processes', sub: 'no PID filter'},
                          {value: 'system_server', label: 'system_server', sub: 'core OS pid'},
                          ...(apps || []).map(a => ({
                            value: a.pkg,
                            label: a.name || a.pkg || '?',
                            sub: a.pkg,
                            icon: <div className='icon' style={{background: '#5f6368'}}>{(a.name || a.pkg || '?')[0].toUpperCase()}</div>,
                          }))]}
                  placeholder='All processes'/>
        <IconBtn title={state.paused ? 'Resume' : 'Pause'} active={state.paused} onClick={() => logcatStore.setPaused(device.id, !state.paused)}>
          {state.paused ? <Icon.Play width={14} height={14}/> : <Icon.Pause width={14} height={14}/>}
        </IconBtn>
        <IconBtn title={tail ? 'Auto-scroll on' : 'Auto-scroll off'} active={tail}
                 onClick={() => { if (tail) { setTail(false); missedFrom.current = filtered.length; } else resumeTail(); }}>
          <Icon.Activity width={14} height={14}/>
        </IconBtn>
        <IconBtn title='Clear' onClick={() => { logcatStore.clear(device.id); setExpanded(null); }}>
          <Icon.Trash width={14} height={14}/>
        </IconBtn>
        <CommandChip label='Logcat' groups={[
          {label: 'Streaming', commands: cmds?.stream, note: 'The PID filter is whichever process the feed attached to.'},
          {label: 'Clear on device', commands: cmds?.clear},
        ]}/>
        <button className='btn sm' onClick={() => exportLines(filtered, device.id)}><Icon.Download width={12} height={12}/>Export</button>
      </div>

      <div className='logcat-toolbar'>
        <SearchInput value={searchInput} onChange={setSearchInput} placeholder='Search tag / message'/>
        <div className='spacer' style={{flex: 1}}/>
        <button className={`btn sm${state.showSystem ? ' primary' : ''}`}
                title='OS-owned log lines (kernel, system_server, daemons). Hidden by default so app logs stay readable.'
                onClick={() => logcatStore.setShowSystem(device.id, !state.showSystem)}>
          <Icon.Cpu width={12} height={12}/>System logs
        </button>
        <button className={`btn sm${collapsing ? ' primary' : ''}`} disabled={!collapsible}
                title={collapsible
                  ? 'Hide lines this app repeats within 10 seconds. The first one is always shown; hidden lines are left out of Export too.'
                  : 'Pick an app above to collapse its repeated lines'}
                onClick={() => setCollapse(c => !c)}>
          <Icon.Layers width={12} height={12}/>Collapse repeats
        </button>
        <LevelMenu value={levelMin} onChange={setLevelMin}/>
      </div>

      <div className='logcat-viewport'>
      <div className='logcat-rows' ref={wrap} onScroll={e => {
        const el = e.currentTarget;
        const bottom = el.scrollHeight - el.scrollTop - el.clientHeight < ROW_H * 2;
        // Scrolling up is an explicit "let me read this", so it takes over from
        // auto-scroll instead of fighting it — otherwise the next batch yanks
        // the reader straight back to the newest line. Our own scroll-to-bottom
        // lands at the bottom, so it never trips this.
        if (!bottom && atBottom.current && tail) {
          setTail(false);
          missedFrom.current = filtered.length;
        }
        // Scrolling the whole way back down means "I'm caught up" — same
        // intent as pressing the jump pill, so it resumes following.
        if (bottom && !tail) setTail(true);
        atBottom.current = bottom;
        setScrollTop(el.scrollTop);
      }}>
        {filtered.length > 0 && (
          <div style={{height: totalH, position: 'relative'}}>
            <div style={{transform: `translateY(${padTop}px)`}}>
              {visible.map((l, i) => {
                const isExpanded = l === expanded;
                return (
                  <div key={first + i} ref={isExpanded ? expandedRow : undefined}
                       className={`logrow ${l.lvl}${isExpanded ? ' expanded' : ''}`}
                       onClick={() => setExpanded(isExpanded ? null : l)}>
                    <span className='time'>{l.time}</span>
                    <span className='pid' title={l.proc || ''}>{l.pid}-{l.tid}</span>
                    <span className='lvl'>{l.lvl}</span>
                    <span className='tag-msg'><span className='tag'>{l.tag}</span><span className='msg'>{highlight(l.msg, search)}</span></span>
                  </div>
                );
              })}
            </div>
          </div>
        )}
        {filtered.length === 0 && lines.length === 0 && (
          <div className='muted' style={{padding: 30, textAlign: 'center'}}>
            {state.error ? `Logcat failed: ${state.error}` : state.running ? 'Waiting for log lines…' : 'Logcat stopped.'}
          </div>
        )}
        {filtered.length === 0 && lines.length > 0 && (
          <div className='muted' style={{padding: 30, textAlign: 'center'}}>No matching lines. Try lowering the level threshold or clearing the search.</div>
        )}
      </div>
      {!tail && (
        <button className='logcat-jump' onClick={resumeTail}>
          <Icon.ChevronDown width={13} height={13}/>
          {missed > 0 ? `${missed} new line${missed === 1 ? '' : 's'}` : 'Jump to latest'}
        </button>
      )}
      </div>

      <div className='logcat-status'>
        <span>{filtered.length} visible</span>
        <span>{state.paused ? 'Paused' : 'Live'} · ≥{levelMin} · {state.pkgFilter ? `pkg=${state.pkgFilter}` : 'all processes'} · {state.showSystem ? 'apps + system' : 'apps only'}</span>
        {collapsed > 0 && <span title='Identical lines repeated within 10 seconds'>{collapsed} repeats hidden</span>}
        <div style={{flex: 1}}/>
        {/* The command lives in the toolbar's chip, which renders what actually
            runs; a second, hand-written copy here disagreed with it (the feed
            resolves a PID, it does not run pidof). */}
        <span className='subtle'>{device.id}</span>
      </div>
    </div>
  );
}

/**
 * Minimum-level picker. The six bare letters used to sit loose in the toolbar
 * where they were easy to misread and easy to mis-click; they now live in a
 * labelled panel that opens on hover (and on click, so keyboard and touch work
 * too) and spells each level out.
 */
function LevelMenu({value, onChange}: {value: string; onChange: (v: string) => void}) {
  const [open, setOpen] = useState(false);
  const closeTimer = useRef<ReturnType<typeof setTimeout>>();

  // A small grace period on leave: the pointer has to cross a gap between the
  // trigger and the panel, and closing instantly there makes the menu feel
  // broken.
  const scheduleClose = () => {
    clearTimeout(closeTimer.current);
    closeTimer.current = setTimeout(() => setOpen(false), 140);
  };
  const cancelClose = () => clearTimeout(closeTimer.current);
  useEffect(() => () => clearTimeout(closeTimer.current), []);

  return (
    <div className='level-menu' onMouseEnter={() => { cancelClose(); setOpen(true); }} onMouseLeave={scheduleClose}>
      <button className='btn sm' aria-haspopup='menu' aria-expanded={open} onClick={() => setOpen(o => !o)}>
        Show ≥ <strong style={{marginLeft: 4}}>{value}</strong>
      </button>
      {open && (
        <div className='dropdown' role='menu' onMouseEnter={cancelClose} onMouseLeave={scheduleClose}>
          <div className='dropdown-header'>Minimum level</div>
          {LEVELS.map(L => (
            <div key={L} role='menuitemradio' aria-checked={value === L}
                 className={`item${value === L ? ' active' : ''}`}
                 onClick={() => { onChange(L); setOpen(false); }}>
              <span className='lvl' style={{color: `var(--log-${L.toLowerCase()})`}}>{L}</span>
              <span className='dd-label'>{LEVEL_NAMES[L]} and above</span>
              {value === L && <Icon.Check width={12} height={12} className='dd-check'/>}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function exportLines(lines: LogEntry[], serial: string) {
  if (lines.length === 0) {
    showToast({title: 'Nothing to export', body: 'No lines match the current filter.', kind: 'info'});
    return;
  }
  const header = `# adbq logcat export — ${serial} — ${new Date().toISOString()}\n# ${lines.length} lines\n\n`;
  const text = header + lines.map(l => `${l.time}  ${l.pid}-${l.tid} ${l.lvl} ${l.tag}: ${l.msg}`).join('\n');
  void saveTextAs({
    title: 'Export logcat',
    suggestedName: `logcat-${serial}-${fileStamp()}.txt`,
    content: text,
  });
}
