// Shell screen — xterm.js-backed real terminal.
//
// The heavy lifting lives in components/Terminal.tsx. This file orchestrates
// session tabs (open/close root vs non-root), the snippet sidebar, save-buffer,
// and replays persisted scrollback on first mount.

import React, {useEffect, useMemo, useState} from 'react';
import {adb} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {Icon} from '../icons';
import {showToast} from '../ui';
import {useStore} from '../store';
import {TerminalView, disposeTerm, replayScrollbackInto, getOrCreateTerm, getTerm, dumpTerm} from '../components/Terminal';
import {fileStamp, saveTextAs} from '../lib/saveText';

const SNIPPETS: {label: string; cmd: string; desc?: string}[] = [
  {label: 'whoami',          cmd: 'whoami; id',                       desc: 'Identity'},
  {label: 'top',             cmd: 'top -n 1 -b -m 20',                desc: 'Top procs (one shot)'},
  {label: 'getprop',         cmd: 'getprop ro.build.version.release', desc: 'Android version'},
  {label: 'pm list -3',      cmd: 'pm list packages -3',              desc: 'User apps'},
  {label: 'logcat -d 50',    cmd: 'logcat -d -t 50',                  desc: 'Recent log (50 lines)'},
  {label: 'dumpsys battery', cmd: 'dumpsys battery',                  desc: 'Battery state'},
  {label: 'ip addr',         cmd: 'ip addr show wlan0',               desc: 'Wi-Fi address'},
  {label: 'http_proxy',      cmd: 'settings get global http_proxy',   desc: 'Read proxy'},
  {label: 'magisk -V',       cmd: 'magisk -V 2>/dev/null || which su',desc: 'Root check'},
  {label: 'mount',           cmd: 'mount | head -20',                 desc: 'Mount table'},
  {label: 'df -h',           cmd: 'df -h /data /sdcard /system',      desc: 'Disk usage'},
  {label: 'ps -A | grep',    cmd: 'ps -A | grep -i ',                 desc: 'Find process'},
];

const uiState = new Map<string, {activeId: string}>();

export function ShellScreen({device}: {device: adb.Device}) {
  const store = useStore();
  const sessions = useMemo(
    () => Object.values(store.shells).filter(s => s.serial === device.id),
    [store.shells, device.id]
  );

  const persisted = uiState.get(device.id) || {activeId: ''};
  const [activeId, setActiveId] = useState(persisted.activeId);
  const [showSnippets, setShowSnippets] = useState(true);
  // (apiRef removed — we now look up the live xterm Terminal directly via
  //  getTerm(activeId) so tab switches don't strand stale references.)

  useEffect(() => {
    uiState.set(device.id, {activeId});
  }, [device.id, activeId]);

  useEffect(() => {
    if (!activeId && sessions.length > 0) setActiveId(sessions[0].id);
    if (activeId && !sessions.some(s => s.id === activeId)) setActiveId(sessions[0]?.id || '');
  }, [sessions, activeId]);

  // Auto-open a session on mount + consume any queued cross-screen handoff
  // (e.g. Apps "Open shell here" wants us to cd into /data/data/PKG).
  useEffect(() => {
    if (!device?.id) return;
    const queued = store.consumeShellCmd(device.id);
    if (queued) {
      // Always spawn a fresh session for the handoff so the new context
      // doesn't pollute an existing tab the user may have running.
      open(!!queued.root).then(id => {
        // Wait a bit for the device prompt to render before typing.
        setTimeout(() => API.WriteShell(id, queued.cmd + '\n').catch(() => {}), 600);
      }).catch(() => {});
      return;
    }
    if (sessions.length === 0) open(!!device.root).catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [device?.id]);

  const cur = sessions.find(s => s.id === activeId);

  async function open(root: boolean): Promise<string> {
    if (!device?.id) throw new Error('no device');
    try {
      const id = await store.openShell(device.id, root);
      // Pre-create the Terminal so EventsOn is wired before the first byte
      // arrives. (PTY echoes the device prompt almost immediately.)
      getOrCreateTerm(id, {root, serial: device.id});
      setActiveId(id);
      return id;
    } catch (e) {
      showToast({title: 'Shell failed', body: String(e), kind: 'err'});
      throw e;
    }
  }

  function closeSession(id: string) {
    store.closeShell(id);
    disposeTerm(id);
  }

  function insertSnippet(cmd: string, runImmediately: boolean) {
    if (!cur) return;
    if (runImmediately) {
      // Send the line with \n so the device runs it.
      API.WriteShell(cur.id, cmd + '\n').catch(() => {});
    } else {
      // Paste into the device PTY; remote echo updates the buffer.
      API.WriteShell(cur.id, cmd).catch(() => {});
    }
    getTerm(cur.id)?.focus();
  }

  function clearTerm() {
    if (!cur) return;
    getTerm(cur.id)?.clear();
  }

  function saveBuffer() {
    if (!cur) return;
    const text = dumpTerm(cur.id);
    if (!text.trim()) {
      showToast({title: 'Nothing to save', body: 'This session has no output yet.', kind: 'info'});
      return;
    }
    void saveTextAs({
      title: 'Save shell buffer',
      suggestedName: `shell-${device.id}-${fileStamp()}.txt`,
      content: text,
    });
  }

  return (
    <div className='screen'>
      <div className='screen-header'>
        <h1>Shell <span className='subtitle mono'>{device.id} · {sessions.length} session{sessions.length === 1 ? '' : 's'}</span></h1>
        <div className='spacer' style={{flex: 1}}/>
        <PastSessionsButton serial={device.id}/>
        <button className={`btn sm${showSnippets ? ' primary' : ''}`} onClick={() => setShowSnippets(s => !s)}>
          Snippets
        </button>
        <button className='btn' onClick={() => open(!!device.root)}>
          {device.root ? <><Icon.Shield/>New root shell</> : <><Icon.Plus/>New shell</>}
        </button>
        {device.root && <button className='btn' onClick={() => open(false)}><Icon.Plus/>Plain shell</button>}
        {cur && <button className='btn' title='Wipe scrollback (destructive). Ctrl+L just pushes prompt to top.' onClick={clearTerm}><Icon.Trash/>Wipe</button>}
        {cur && <button className='btn' title='Save visible buffer to file' onClick={saveBuffer}><Icon.Download/>Save</button>}
      </div>

      <div className='shell-tabs'>
        {sessions.map(s => (
          <div key={s.id}
               className={`shell-tab${activeId === s.id ? ' active' : ''}${s.root ? ' root' : ''}`}
               onClick={() => setActiveId(s.id)}>
            <span className='dot'/>
            <span className='mono'>{s.root ? '#' : '$'} {s.label}</span>
            <span onClick={(e) => { e.stopPropagation(); closeSession(s.id); }}
                  style={{marginLeft: 4, cursor: 'pointer', display: 'inline-flex'}} className='muted'>
              <Icon.X width={11} height={11}/>
            </span>
          </div>
        ))}
        {sessions.length === 0 && <span className='muted' style={{fontSize: 12, padding: '6px 10px'}}>Opening session…</span>}
        <div style={{flex: 1}}/>
        {cur && (
          <span className='muted mono' style={{fontSize: 10.5, padding: '0 8px'}}>
            {cur.root ? 'root' : 'shell'}@{device.product || 'device'}{cur.root ? ' #' : ' $'}
          </span>
        )}
      </div>

      <div style={{flex: 1, display: 'grid', gridTemplateColumns: showSnippets ? '1fr 220px' : '1fr', minHeight: 0}}>
        {/* All sessions are rendered into the same stacking area at once and we
            toggle visibility via display:none. xterm.js Terminal.open() can
            only bind to one DOM node for its lifetime, so we must NOT unmount
            inactive tabs — otherwise switching back gives a blank pane. */}
        <div style={{position: 'relative', minHeight: 0, minWidth: 0}}>
          {sessions.length === 0 && (
            <div style={{padding: 24, color: 'var(--text-subtle)', fontFamily: 'var(--font-mono)', fontSize: 12}}>
              # adbq shell — opening session…
            </div>
          )}
          {sessions.map(s => (
            <div key={s.id}
                 style={{
                   position: 'absolute', inset: 0,
                   display: s.id === activeId ? 'flex' : 'none',
                   flexDirection: 'column',
                 }}>
              <TerminalView
                sessionId={s.id}
                root={s.root}
                serial={device.id}
                visible={s.id === activeId}
              />
            </div>
          ))}
        </div>

        {showSnippets && (
          <div style={{borderLeft: '1px solid var(--border)', overflow: 'auto', background: 'var(--panel-2)', display: 'flex', flexDirection: 'column'}}>
            <div style={{padding: '10px 12px', borderBottom: '1px solid var(--border)', fontWeight: 600, fontSize: 11.5, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em'}}>
              Snippets
            </div>
            <div className='subtle' style={{padding: '6px 12px', fontSize: 10.5, borderBottom: '1px solid var(--border)'}}>
              Tap: insert into terminal · Double-tap: run instantly
            </div>
            <div style={{display: 'flex', flexDirection: 'column', gap: 2, padding: 6}}>
              {SNIPPETS.map(s => (
                <button key={s.label}
                        onClick={() => insertSnippet(s.cmd, false)}
                        onDoubleClick={() => insertSnippet(s.cmd, true)}
                        title={s.cmd}
                        className='mono'
                        style={{
                          textAlign: 'left', padding: '7px 9px', borderRadius: 5,
                          background: 'transparent', border: '1px solid transparent',
                          cursor: 'pointer', fontSize: 11.5, color: 'var(--text)',
                        }}
                        onMouseEnter={e => (e.currentTarget.style.background = 'var(--hover)')}
                        onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}>
                  <div style={{fontWeight: 500}}>{s.label}</div>
                  <div className='subtle' style={{fontSize: 10.5, marginTop: 2, wordBreak: 'break-all'}}>
                    {s.desc || s.cmd}
                  </div>
                </button>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

// Replay persisted scrollback (best-effort) when the page loads. We expose the
// helper here for future "History" UI; the openShell flow doesn't replay by
// default since the device prompt re-renders anyway.
export {replayScrollbackInto};

// ─── Past sessions dropdown ──────────────────────────────────────────────
//
// Shows every persisted scrollback file (~/.adbq/scrollback/<serial>-<label>.log)
// so the user can re-open previous interactive shells across app restarts.
// Click → opens the log file in the default text viewer (Preview / Notepad);
// each entry also offers Reveal in Finder and a Delete affordance.

function PastSessionsButton({serial}: {serial: string}) {
  const [open, setOpen] = useState(false);
  const [entries, setEntries] = useState<adb.ScrollbackEntry[]>([]);
  const [busy, setBusy] = useState(false);

  function load() {
    setBusy(true);
    API.ListShellHistory()
      .then(es => setEntries((es || []).filter(e => !serial || e.serial.startsWith(serial.slice(0, 8)) || true)))
      .finally(() => setBusy(false));
  }

  function toggle() {
    setOpen(o => {
      const next = !o;
      if (next) load();
      return next;
    });
  }

  return (
    <div style={{position: 'relative'}}>
      <button className='btn sm' onClick={toggle} title='Past shell sessions saved across app restarts'>
        <Icon.Activity width={12} height={12}/>History
        {entries.length > 0 && <span className='subtle mono' style={{marginLeft: 4, fontSize: 10}}>{entries.length}</span>}
      </button>
      {open && (
        <div style={{
          position: 'absolute', top: 'calc(100% + 6px)', right: 0, zIndex: 50,
          width: 360, maxHeight: 400, overflow: 'auto',
          background: 'var(--panel)', border: '1px solid var(--border)',
          borderRadius: 8, boxShadow: 'var(--shadow-lg)',
        }}>
          <div className='spread' style={{padding: '8px 12px', borderBottom: '1px solid var(--border)'}}>
            <span className='muted' style={{fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.06em', fontWeight: 600}}>
              Past sessions
            </span>
            <button className='btn sm ghost' onClick={() => setOpen(false)}><Icon.X width={11} height={11}/></button>
          </div>
          {busy && <div className='muted' style={{padding: 14, fontSize: 12}}>Loading…</div>}
          {!busy && entries.length === 0 && (
            <div className='muted' style={{padding: 14, fontSize: 12}}>
              No persisted shell history yet.
              <div className='subtle' style={{fontSize: 11, marginTop: 4}}>
                Every shell you open writes to <span className='mono'>~/.adbq/scrollback/</span> automatically.
              </div>
            </div>
          )}
          {entries.map(e => (
            <div key={e.path} style={{padding: '10px 12px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'flex-start', gap: 8}}>
              <div style={{flex: 1, minWidth: 0}}>
                <div className='mono' style={{fontSize: 11, wordBreak: 'break-all', fontWeight: 500}}>{e.label}</div>
                <div className='subtle' style={{fontSize: 10.5, marginTop: 2}}>
                  {e.serial} · {(e.bytes / 1024).toFixed(1)} KB · {new Date(e.updatedAt * 1000).toLocaleString()}
                </div>
              </div>
              <button className='btn sm' title='Open in default text viewer' onClick={() => API.OpenPath(e.path).catch(() => {})}>
                <Icon.Play width={10} height={10}/>Open
              </button>
              <button className='btn sm ghost' title='Reveal in Finder' onClick={() => API.RevealPath(e.path).catch(() => {})}>
                <Icon.Folder width={10} height={10}/>
              </button>
              <button className='btn sm danger' title='Delete this log' onClick={() => {
                API.ClearShellHistory(e.serial, e.label).then(load);
              }}><Icon.Trash width={10} height={10}/></button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
