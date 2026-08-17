import React, {useEffect, useMemo, useRef, useState} from 'react';
import {adb, main} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {EventsOff, EventsOn} from '../../wailsjs/runtime';
import {Icon} from '../icons';
import {Badge, CommandPreview, IconBtn, SearchInput, showToast} from '../ui';

interface ProcRow {
  pid: number;
  user: string;
  cpu: number;
  mem: number;
  rss: number;
  vsz: number;
  state: string;
  name: string;
  cmdline: string;
}

interface ProcSnapshot {
  time: number;
  total: number;
  rows: ProcRow[];
  root: boolean;
}

type SortKey = 'cpu' | 'mem' | 'rss' | 'pid' | 'name';
const INTERVALS = [1, 2, 5] as const;

export function ProcessesScreen({device}: {device: adb.Device}) {
  const [snap, setSnap] = useState<ProcSnapshot | null>(null);
  const [search, setSearch] = useState('');
  const [sortKey, setSortKey] = useState<SortKey>('cpu');
  const [sortDesc, setSortDesc] = useState(true);
  const [interval, setInterval] = useState<number>(2);
  const [paused, setPaused] = useState(false);
  const [expanded, setExpanded] = useState<Record<number, boolean>>({});
  const [running, setRunning] = useState(false);
  const pausedRef = useRef(paused);
  pausedRef.current = paused;
  // The procfs sweep behind this table, as the user it is being read by: the
  // stream drops to the shell user when su is refused, which is also why a table
  // can come back half-empty (CLAUDE.md §4.1 K3).
  const [cmds, setCmds] = useState<string[]>([]);
  useEffect(() => {
    if (!device?.id) return;
    let live = true;
    API.ProcessCommands(device.id)
      .then(c => { if (live) setCmds(c || []); })
      .catch(() => { if (live) setCmds([]); });
    return () => { live = false; };
  }, [device?.id, snap?.root]);

  useEffect(() => {
    if (!device?.id) return;
    let cancelled = false;
    setSnap(null);
    setRunning(false);
    API.StartProcStream(device.id, interval)
      .then((st: main.ProcStreamStatus) => { if (!cancelled) setRunning(st.running); })
      .catch(e => showToast({title: 'top stream failed', body: String(e), kind: 'err'}));

    const ev = 'procs:' + device.id;
    EventsOn(ev, (s: ProcSnapshot) => {
      if (pausedRef.current) return;
      setSnap(s);
    });
    return () => {
      cancelled = true;
      EventsOff(ev);
      API.StopProcStream(device.id).catch(() => {});
    };
  }, [device?.id, interval]);

  const rows = snap?.rows || [];

  const filteredSorted = useMemo(() => {
    const q = search.toLowerCase().trim();
    let out = rows;
    if (q) {
      out = out.filter(r =>
        String(r.pid).includes(q) ||
        r.user.toLowerCase().includes(q) ||
        r.name.toLowerCase().includes(q));
    }
    const dir = sortDesc ? -1 : 1;
    out = [...out].sort((a, b) => {
      let av: any, bv: any;
      switch (sortKey) {
        case 'cpu':  av = a.cpu;  bv = b.cpu;  break;
        case 'mem':  av = a.mem;  bv = b.mem;  break;
        case 'rss':  av = a.rss;  bv = b.rss;  break;
        case 'pid':  av = a.pid;  bv = b.pid;  break;
        case 'name': av = a.name; bv = b.name; break;
      }
      if (av < bv) return -1 * dir;
      if (av > bv) return  1 * dir;
      return 0;
    });
    return out;
  }, [rows, search, sortKey, sortDesc]);

  const stats = useMemo(() => {
    const total = rows.length;
    const root = rows.filter(r => r.user === 'root').length;
    const topCpu = rows.reduce((m, r) => Math.max(m, r.cpu), 0);
    const totalMem = rows.reduce((m, r) => m + r.rss, 0);
    return {total, root, topCpu, totalMem};
  }, [rows]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
      if (e.key === '/') {
        e.preventDefault();
        (document.querySelector('.proc-search input') as HTMLInputElement)?.focus();
      } else if (e.key.toLowerCase() === 'p') {
        setPaused(p => !p);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  function toggleSort(k: SortKey) {
    if (sortKey === k) setSortDesc(d => !d);
    else { setSortKey(k); setSortDesc(k !== 'pid' && k !== 'name'); }
  }

  return (
    <div className='screen'>
      <div className='screen-header'>
        <h1>
          Processes{' '}
          <span className='subtitle mono'>
            {filteredSorted.length} / {stats.total} · top CPU {stats.topCpu.toFixed(1)}% · RSS {fmtKB(stats.totalMem)} · root {stats.root}
          </span>
        </h1>
        <div className='spacer' style={{flex: 1}}/>
        <span className='muted' style={{fontSize: 11}}>Refresh</span>
        {INTERVALS.map(s => (
          <button key={s} className={`btn sm${interval === s ? ' primary' : ''}`} onClick={() => setInterval(s)}>{s}s</button>
        ))}
        <IconBtn title={paused ? 'Resume (p)' : 'Pause (p)'} active={paused} onClick={() => setPaused(p => !p)}>
          {paused ? <Icon.Play width={14} height={14}/> : <Icon.Pause width={14} height={14}/>}
        </IconBtn>
      </div>

      <div className='logcat-toolbar proc-search'>
        <SearchInput value={search} onChange={setSearch} placeholder='Filter PID / user / name (Press / to focus)'/>
        <div className='spacer' style={{flex: 1}}/>
        <span className='muted' style={{fontSize: 11}}>Sort</span>
        {(['cpu', 'mem', 'rss', 'pid', 'name'] as SortKey[]).map(k => (
          <button key={k} className={`btn sm${sortKey === k ? ' primary' : ''}`} onClick={() => toggleSort(k)}>
            {labelFor(k)}{sortKey === k ? (sortDesc ? ' ▼' : ' ▲') : ''}
          </button>
        ))}
      </div>

      <div style={{padding: '0 14px 8px'}}>
        <CommandPreview commands={cmds} label='Sampling'/>
      </div>

      <div style={{flex: 1, minHeight: 0, overflow: 'auto'}}>
        <table className='table'>
          <thead>
            <tr>
              <th style={{paddingLeft: 14, width: 70}}>PID</th>
              <th style={{width: 100}}>User</th>
              <th style={{width: 70}}>%CPU</th>
              <th style={{width: 70}}>%MEM</th>
              <th style={{width: 90}}>RSS</th>
              <th style={{width: 32}}>S</th>
              <th>Name</th>
            </tr>
          </thead>
          <tbody>
            {filteredSorted.map(r => (
              <tr key={r.pid} onClick={() => setExpanded(e => ({...e, [r.pid]: !e[r.pid]}))} style={{cursor: 'pointer'}}>
                <td style={{paddingLeft: 14}} className='mono'>{r.pid}</td>
                <td className='mono muted'>{r.user}</td>
                <td className='mono' style={{color: r.cpu > 50 ? 'var(--err)' : r.cpu > 10 ? 'var(--warn)' : undefined}}>{r.cpu.toFixed(1)}</td>
                <td className='mono'>{r.mem.toFixed(1)}</td>
                <td className='mono'>{fmtKB(r.rss)}</td>
                <td><StateBadge s={r.state}/></td>
                <td className='mono' style={{wordBreak: 'break-all'}}>
                  {expanded[r.pid] ? r.cmdline : truncate(r.name, 80)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {!snap && (
          <div className='muted' style={{padding: 30, textAlign: 'center'}}>
            {running ? 'Waiting for top snapshot…' : 'Starting top…'}
          </div>
        )}
        {snap && filteredSorted.length === 0 && (
          <div className='muted' style={{padding: 30, textAlign: 'center'}}>
            {rows.length === 0 ? 'No processes readable from this device.' : 'No processes match the filter.'}
          </div>
        )}
        {snap && !snap.root && (
          <div className='muted' style={{padding: '8px 14px', fontSize: 12, borderTop: '1px solid var(--border)'}}>
            Limited view — device isn’t rooted, so Android only exposes processes the shell can see (Android 7+ hides others). Connect as root for the full list.
          </div>
        )}
      </div>

      <div className='logcat-status'>
        <span>{filteredSorted.length} visible</span>
        <span>{paused ? 'Paused' : 'Live'} · refresh {interval}s</span>
        <div style={{flex: 1}}/>
        <span className='subtle'>adb -s {device.id} shell cat /proc/stat /proc/[0-9]*/stat /proc/meminfo · {interval}s</span>
      </div>
    </div>
  );
}

function StateBadge({s}: {s: string}) {
  if (!s) return <span className='muted mono'>—</span>;
  let kind: 'ok' | 'warn' | 'err' | undefined;
  if (s === 'R') kind = 'ok';
  else if (s === 'Z' || s === 'X') kind = 'err';
  else if (s === 'D' || s === 'T') kind = 'warn';
  return <Badge kind={kind}>{s}</Badge>;
}

function labelFor(k: SortKey): string {
  switch (k) {
    case 'cpu':  return 'CPU';
    case 'mem':  return 'MEM';
    case 'rss':  return 'RSS';
    case 'pid':  return 'PID';
    case 'name': return 'Name';
  }
}

function fmtKB(kb: number) {
  if (kb < 1024) return kb + ' K';
  if (kb < 1024 * 1024) return (kb / 1024).toFixed(1) + ' M';
  return (kb / 1024 / 1024).toFixed(2) + ' G';
}

function truncate(s: string, n: number) {
  return s.length > n ? s.slice(0, n - 1) + '…' : s;
}
