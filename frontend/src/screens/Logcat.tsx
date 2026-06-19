import React, {useEffect, useMemo, useRef, useState} from 'react';
import {adb} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {Icon} from '../icons';
import {Combobox, IconBtn, SearchInput, showToast} from '../ui';
import {useStore} from '../store';

const LEVELS = ['V', 'D', 'I', 'W', 'E', 'F'] as const;
const LEVEL_ORDER: Record<string, number> = {V: 0, D: 1, I: 2, W: 3, E: 4, F: 5};

export function LogcatScreen({device}: {device: adb.Device}) {
  const store = useStore();
  const slice = store.getLogcat(device.id);
  const [search, setSearch] = useState('');
  const [levelMin, setLevelMin] = useState<string>('V');
  const [tail, setTail] = useState(true);
  const [apps, setApps] = useState<adb.App[]>([]);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const wrap = useRef<HTMLDivElement>(null);

  // Start the stream when the device changes OR when the slice thinks it's
  // running but the event subscription was dropped (after a hot-reload or a
  // backend restart). startLogcat is idempotent — same filter while
  // `running` is true is a no-op; anything else tears down the old sub
  // and starts a fresh one.
  useEffect(() => {
    if (!device?.id) return;
    store.startLogcat(device.id, slice.pkgFilter);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [device?.id]);

  useEffect(() => {
    if (!device?.id) return;
    store.cached(`apps:${device.id}:user`, 30_000, () => API.ListApps(device.id, true)).then(setApps).catch(() => {});
  }, [device?.id]);

  useEffect(() => {
    if (!wrap.current || !tail || slice.paused) return;
    wrap.current.scrollTop = wrap.current.scrollHeight;
  }, [slice.lines, tail, slice.paused]);

  const filtered = useMemo(() => {
    const q = search.toLowerCase();
    const min = LEVEL_ORDER[levelMin];
    return slice.lines.filter(l =>
      (LEVEL_ORDER[l.lvl] ?? 0) >= min
      && (!q || l.tag.toLowerCase().includes(q) || l.msg.toLowerCase().includes(q))
    );
  }, [slice.lines, levelMin, search]);

  function setPkgFilter(pkg: string) {
    store.startLogcat(device.id, pkg);
    setExpanded({});
  }

  return (
    <div className='screen'>
      <div className='screen-header'>
        <h1>Logcat <span className='subtitle mono'>{filtered.length} / {slice.lines.length} lines</span></h1>
        <div className='spacer' style={{flex: 1}}/>
        <Combobox value={slice.pkgFilter} onChange={setPkgFilter} width={240}
                  items={[{value: '', label: 'All processes', sub: 'no PID filter'},
                          {value: 'system_server', label: 'system_server', sub: 'core OS pid'},
                          ...apps.map(a => ({
                            value: a.pkg,
                            label: a.name || a.pkg,
                            sub: a.pkg,
                            icon: <div className='icon' style={{background: '#5f6368'}}>{(a.name || a.pkg)[0].toUpperCase()}</div>,
                          }))]}
                  placeholder='All processes'/>
        <IconBtn title={slice.paused ? 'Resume' : 'Pause'} active={slice.paused} onClick={() => store.setPaused(device.id, !slice.paused)}>
          {slice.paused ? <Icon.Play width={14} height={14}/> : <Icon.Pause width={14} height={14}/>}
        </IconBtn>
        <IconBtn title={tail ? 'Auto-scroll on' : 'Auto-scroll off'} active={tail} onClick={() => setTail(!tail)}>
          <Icon.Activity width={14} height={14}/>
        </IconBtn>
        <IconBtn title='Clear' onClick={() => { store.clearLogcat(device.id); setExpanded({}); }}>
          <Icon.Trash width={14} height={14}/>
        </IconBtn>
        <button className='btn sm' onClick={() => exportLines(filtered, device.id)}><Icon.Download width={12} height={12}/>Export</button>
      </div>

      <div className='logcat-toolbar'>
        <SearchInput value={search} onChange={setSearch} placeholder='Search tag / message'/>
        <div className='spacer' style={{flex: 1}}/>
        <span className='muted' style={{fontSize: 11}}>Show ≥</span>
        {LEVELS.map(L => (
          <button key={L} className={`btn sm${levelMin === L ? ' primary' : ''}`} style={{minWidth: 28}} onClick={() => setLevelMin(L)}>{L}</button>
        ))}
      </div>

      <div className='logcat-rows' ref={wrap}>
        {filtered.map((l, i) => {
          // Stable key: tag+pid+tid+time+msg-prefix is unique enough for rollover-resilient expand state.
          const key = `${l.time}|${l.pid}|${l.tid}|${l.tag}|${l.msg.slice(0, 24)}|${i}`;
          return (
            <div key={key} className={`logrow ${l.lvl}${expanded[key] ? ' expanded' : ''}`}
                 onClick={() => setExpanded(e => ({...e, [key]: !e[key]}))}>
              <span className='time'>{l.time}</span>
              <span className='pid'>{l.pid}-{l.tid}</span>
              <span className='lvl'>{l.lvl}</span>
              <span className='tag-msg'><span className='tag'>{l.tag}</span><span className='msg'>{highlight(l.msg, search)}</span></span>
            </div>
          );
        })}
        {filtered.length === 0 && slice.lines.length === 0 && (
          <div className='muted' style={{padding: 30, textAlign: 'center'}}>{slice.running ? 'Waiting for log lines…' : 'Logcat stopped.'}</div>
        )}
        {filtered.length === 0 && slice.lines.length > 0 && (
          <div className='muted' style={{padding: 30, textAlign: 'center'}}>No matching lines. Try lowering the level threshold or clearing the search.</div>
        )}
      </div>

      <div className='logcat-status'>
        <span>{filtered.length} visible</span>
        <span>{slice.paused ? 'Paused' : 'Live'} · ≥{levelMin} · {slice.pkgFilter ? `pkg=${slice.pkgFilter}` : 'all processes'}</span>
        <div style={{flex: 1}}/>
        <span className='subtle'>adb -s {device.id} logcat -v threadtime{slice.pkgFilter ? ` --pid=$(pidof ${slice.pkgFilter})` : ''}</span>
      </div>
    </div>
  );
}

function highlight(msg: string, q: string) {
  if (!q) return msg;
  try {
    // Use case-insensitive plain substring scan to avoid RegExp.lastIndex state.
    const ql = q.toLowerCase();
    const ml = msg.toLowerCase();
    const out: React.ReactNode[] = [];
    let i = 0;
    while (i < msg.length) {
      const found = ml.indexOf(ql, i);
      if (found < 0) { out.push(msg.slice(i)); break; }
      if (found > i) out.push(msg.slice(i, found));
      out.push(<mark key={found}>{msg.slice(found, found + q.length)}</mark>);
      i = found + q.length;
    }
    return out;
  } catch { return msg; }
}

function exportLines(lines: any[], serial: string) {
  const header = `# adbq logcat export — ${serial} — ${new Date().toISOString()}\n# ${lines.length} lines\n\n`;
  const text = header + lines.map(l => `${l.time}  ${l.pid}-${l.tid} ${l.lvl} ${l.tag}: ${l.msg}`).join('\n');
  const blob = new Blob([text], {type: 'text/plain'});
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url; a.download = `logcat-${serial}-${Date.now()}.txt`; a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
  showToast({title: 'Logcat exported', body: `${lines.length} lines`, kind: 'ok'});
}
