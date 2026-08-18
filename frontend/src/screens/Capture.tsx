import React, {useEffect, useMemo, useRef, useState} from 'react';
import {adb} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {Icon} from '../icons';
import {Badge, CommandChip, SearchInput, confirmDialog, showToast} from '../ui';
import {installTcpdumpAuto} from '../lib/tcpdump';
import {parseFilter} from '../lib/captureFilter';
import {usePoll} from '../lib/poll';
import {CapturePacket, useStore} from '../store';

interface LiveLayer { name: string; bytes: number; offset: number; fields: {k: string; v: string}[] }
interface LiveDetail extends CapturePacket { layersFull: LiveLayer[]; rawHex: string }

// Fixed packet-row height (must match .capture-row in styles.css) so the list
// can be virtualized — rendering every row froze the UI on large buffers.
const CAPTURE_ROW_H = 22;
const IFACES = ['any', 'wlan0', 'rmnet0', 'rmnet_data0', 'eth0', 'lo'];
const MAX_PACKETS_OPTIONS = [
  {label: '5k packets',   value: 5000,   mirror: 50 * 1024 * 1024},
  {label: '10k packets',  value: 10000,  mirror: 100 * 1024 * 1024},
  {label: '25k packets',  value: 25000,  mirror: 250 * 1024 * 1024},
  {label: '50k packets',  value: 50000,  mirror: 500 * 1024 * 1024},
  {label: '100k packets', value: 100000, mirror: 1024 * 1024 * 1024},
];

const PRESETS: {label: string; bpf: string; hint: string}[] = [
  {label: 'All traffic',         bpf: '',                                                              hint: 'No filter'},
  {label: 'DNS only',            bpf: 'udp port 53 or tcp port 53',                                    hint: 'Queries + replies'},
  {label: 'TLS / HTTPS',         bpf: 'tcp port 443',                                                  hint: 'Includes ClientHello SNI'},
  {label: 'TLS handshakes only', bpf: 'tcp port 443 and (tcp[tcpflags] & tcp-syn != 0)',               hint: 'SYNs to :443'},
  {label: 'HTTP (cleartext)',    bpf: 'tcp port 80',                                                   hint: 'Request lines visible'},
  {label: 'QUIC / HTTP/3',       bpf: 'udp port 443',                                                  hint: 'UDP-based HTTPS'},
  {label: 'Ping / ICMP',         bpf: 'icmp or icmp6',                                                 hint: 'Echo request/reply'},
  {label: 'ARP',                 bpf: 'arp',                                                           hint: 'Layer 2 address resolution'},
  {label: 'mDNS / Bonjour',      bpf: 'udp port 5353',                                                 hint: 'Service discovery'},
  {label: 'NTP',                 bpf: 'udp port 123',                                                  hint: 'Clock sync'},
];

export function CaptureScreen({device}: {device: adb.Device}) {
  const store = useStore();
  const slice = store.getCapture(device.id);
  // The tcpdump invocation this capture runs, rendered by Go for the interface
  // and filter currently selected, and kept on screen while packets arrive
  // (CLAUDE.md §4.1 K3).
  const [cmd, setCmd] = useState<string[]>([]);
  const {iface, bpf, preset, displayFilter, maxPackets, packets, active, state} = slice;

  const [selected, setSelected] = useState<CapturePacket | null>(null);
  const [detail, setDetail] = useState<LiveDetail | null>(null);
  const [openLayers, setOpenLayers] = useState<Set<number>>(new Set([0, 1, 2, 3]));
  const [tail, setTail] = useState(true);
  const [tdAvailable, setTdAvailable] = useState<boolean | null>(null);
  const listRef = useRef<HTMLDivElement>(null);
  // Virtualization window: render only the visible packet rows.
  const [scrollTop, setScrollTop] = useState(0);
  const [viewH, setViewH] = useState(0);

  // Status poll + tcpdump probe. The packet stream itself is owned by the
  // store, so we don't subscribe here — packets keep arriving across screen
  // switches.
  useEffect(() => {
    if (!device?.id) return;
    let cancelled = false;
    const tick = async () => {
      try {
        const st = await API.LiveCaptureStatus(device.id);
        if (!cancelled) store.setCaptureState(device.id, st);
      } catch {}
    };
    tick();
    API.ProbeTcpdump(device.id).then(td => { if (!cancelled) setTdAvailable(!!td?.available); }).catch(() => { if (!cancelled) setTdAvailable(false); });
    return () => { cancelled = true; };
  }, [device?.id, store]);
  // Only while a capture is actually running: a stopped capture's status does
  // not change on its own, and this used to poll regardless.
  usePoll(() => { void API.LiveCaptureStatus(device.id).then(st => store.setCaptureState(device.id, st)); },
    1500, !!device?.id && active);

  // Re-render the command as the interface or filter changes. Resolving
  // tcpdump's path is a device call, so it is debounced rather than run per
  // keystroke.
  useEffect(() => {
    if (!device?.id) { setCmd([]); return; }
    let live = true;
    const t = setTimeout(() => {
      API.LiveCaptureCommand(device.id, iface, bpf)
        .then(c => { if (live) setCmd(c || []); })
        .catch(() => { if (live) setCmd([]); });
    }, 300);
    return () => { live = false; clearTimeout(t); };
  }, [device?.id, iface, bpf, tdAvailable]);

  useEffect(() => {
    if (!tail || !listRef.current) return;
    listRef.current.scrollTop = listRef.current.scrollHeight;
  }, [packets, tail]);

  // Track the list viewport height so the virtual window covers it.
  useEffect(() => {
    const el = listRef.current;
    if (!el) return;
    const measure = () => setViewH(el.clientHeight);
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  useEffect(() => {
    if (!selected) { setDetail(null); return; }
    let cancelled = false;
    API.DescribeLivePacket(device.id, selected.no).then(d => {
      if (!cancelled) setDetail((d as unknown as LiveDetail) || null);
    }).catch(() => { if (!cancelled) setDetail(null); });
    return () => { cancelled = true; };
  }, [selected?.no, device?.id]);

  // Compile the display filter once per change. Bad syntax is reported but
  // doesn't block — we degrade to "match everything" so typing mid-expression
  // doesn't hide the whole list.
  const parsed = useMemo(() => parseFilter(displayFilter), [displayFilter]);
  // `packets` is a ring mutated in place, so its identity never changes — `rev`
  // is what signals new data (the same arrangement the logcat pane uses). An
  // unfiltered view skips the walk entirely: at the 100k setting, filtering to
  // "everything" was copying a hundred thousand references several times a
  // second to produce a list identical to the one it started from.
  const filtered = useMemo(
    () => (parsed.isEmpty ? packets : packets.filter(parsed.pred)),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [packets, parsed, slice.rev],
  );

  // Virtual window over `filtered`. The sticky header occupies the first row, so
  // offset the inner scroll position by one row height.
  const total = filtered.length;
  const innerTop = Math.max(0, scrollTop - CAPTURE_ROW_H);
  const winStart = Math.max(0, Math.floor(innerTop / CAPTURE_ROW_H) - 16);
  const winEnd = Math.min(total, Math.ceil((innerTop + (viewH || 600)) / CAPTURE_ROW_H) + 16);
  const visible = filtered.slice(winStart, winEnd);

  const mirrorOpt = MAX_PACKETS_OPTIONS.find(o => o.value === maxPackets) || MAX_PACKETS_OPTIONS[1];

  async function start() {
    if (tdAvailable === false) {
      showToast({title: 'tcpdump missing', body: 'Install it first', kind: 'err'});
      return;
    }
    // If a previous run left packets behind, offer to save them before nuking.
    if (packets.length > 0) {
      const ok = await confirmDialog({
        title: `Discard ${packets.length.toLocaleString()} captured packets?`,
        body: 'Starting a new capture clears the existing list. Save the current .pcap first if you might want it later.',
        confirmLabel: 'Discard & start',
        danger: true,
      });
      if (!ok) return;
    }
    try {
      await store.startCapture(device.id, iface, bpf, preset, maxPackets, mirrorOpt.mirror);
      showToast({title: 'Live capture started', body: `${iface}${bpf ? ' · ' + bpf : ''}`, kind: 'ok', mono: true});
    } catch (e) {
      showToast({title: 'Start failed', body: String(e), kind: 'err'});
    }
  }
  async function stop() {
    try {
      await store.stopCapture(device.id);
      const st = await API.LiveCaptureStatus(device.id);
      store.setCaptureState(device.id, st);
      showToast({title: 'Capture stopped', body: `${packets.length} packets in memory`, kind: 'ok'});
    } catch (e) {
      showToast({title: 'Stop failed', body: String(e), kind: 'err'});
    }
  }
  async function save() {
    try {
      const path = await API.SaveLivePcap(device.id);
      if (path) showToast({title: 'Saved', body: path, kind: 'ok', mono: true});
    } catch (e) {
      showToast({title: 'Save failed', body: String(e), kind: 'err'});
    }
  }
  async function clearList() {
    if (packets.length === 0) return;
    const ok = await confirmDialog({
      title: `Clear ${packets.length.toLocaleString()} packets from memory?`,
      body: 'This only empties the on-screen list. The on-device tcpdump (if running) keeps capturing, and the on-disk .pcap mirror is unchanged.',
      confirmLabel: 'Clear',
      danger: true,
    });
    if (ok) { store.clearCapture(device.id); setSelected(null); }
  }
  async function installTd() {
    const ok = await installTcpdumpAuto(device.id);
    if (ok) setTdAvailable(true);
  }
  function applyPreset(i: number) { store.setCapturePreset(device.id, i, PRESETS[i].bpf); }
  function setBpf(v: string) { store.setCapturePreset(device.id, preset, v); }

  function toggleLayer(i: number) {
    setOpenLayers(prev => {
      const next = new Set(prev);
      if (next.has(i)) next.delete(i); else next.add(i);
      return next;
    });
  }

  return (
    <div className='screen'>
      <div className='screen-header'>
        <h1>Capture <span className='subtitle mono'>
          {packets.length.toLocaleString()}{filtered.length !== packets.length ? `/${filtered.length.toLocaleString()}` : ''} pkts
          {state?.bytes ? ` · ${fmtBytes(state.bytes)} captured` : ''}
          {state?.pcapBytes != null ? ` · disk ${fmtBytes(state.pcapBytes)}/${fmtBytes(state.maxPcapBytes || mirrorOpt.mirror)}` : ''}
          {state?.pcapRotations ? ` · rot ${state.pcapRotations}` : ''}
          {state?.linkType ? ` · DLT ${state.linkType}` : ''}
        </span></h1>
        <div className='spacer' style={{flex: 1}}/>
        {active && <Badge kind='accent'>recording</Badge>}
        {!active && packets.length > 0 && <Badge kind='warn'>buffered</Badge>}
      </div>

      {state?.error && !active && (
        <div className='card' style={{margin: '0 18px 12px', padding: 10, borderColor: 'var(--err)'}}>
          <div style={{fontSize: 12}}>
            <strong style={{color: 'var(--err)'}}>Capture failed</strong> — the device produced no packet stream.
            <div className='mono' style={{marginTop: 4, fontSize: 11, wordBreak: 'break-all', color: 'var(--text-muted)'}}>{state.error}</div>
          </div>
        </div>
      )}

      {tdAvailable === false && (
        <div className='card' style={{margin: '0 18px 12px', padding: 10, borderColor: 'var(--warn)'}}>
          <div className='spread' style={{alignItems: 'center', gap: 8}}>
            <span style={{fontSize: 12}}>
              <strong>tcpdump not installed</strong> — live capture needs a tcpdump binary on the device.
            </span>
            <button className='btn sm primary' onClick={installTd}>Install automatically</button>
          </div>
        </div>
      )}

      <div className='capture-toolbar'>
        <label className='muted' style={{fontSize: 11}}>preset</label>
        <select className='btn sm' value={preset} onChange={e => applyPreset(parseInt(e.target.value, 10))} disabled={active}>
          {PRESETS.map((p, i) => <option key={i} value={i}>{p.label}</option>)}
        </select>
        <label className='muted' style={{fontSize: 11}}>iface</label>
        <select className='btn sm' value={iface} onChange={e => store.setCaptureIface(device.id, e.target.value)} disabled={active}>
          {IFACES.map(i => <option key={i} value={i}>{i}</option>)}
        </select>
        <label className='muted' style={{fontSize: 11}}>memory</label>
        <select className='btn sm' value={maxPackets} onChange={e => store.setCaptureMaxPackets(device.id, parseInt(e.target.value, 10))}>
          {MAX_PACKETS_OPTIONS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
        </select>
        <label className='muted' style={{fontSize: 11}}>BPF</label>
        <input className='btn sm mono' style={{minWidth: 220, flex: 1}} value={bpf} onChange={e => setBpf(e.target.value)}
               placeholder="capture filter — e.g. tcp port 443" disabled={active}/>
        {active
          ? <button className='btn danger' onClick={stop}><Icon.Stop/>Stop</button>
          : <button className='btn primary' onClick={start} disabled={!device.root || tdAvailable === false}><Icon.Play/>Start</button>}
        <CommandChip label='Live capture' commands={cmd}/>
        <button className='btn' onClick={save} disabled={!packets.length}><Icon.Download/>Save .pcap</button>
        <button className='btn sm' onClick={clearList} disabled={!packets.length}>Clear</button>
        <label style={{display: 'flex', alignItems: 'center', gap: 4, fontSize: 11, color: 'var(--text-subtle)'}}>
          <input type='checkbox' checked={tail} onChange={e => setTail(e.target.checked)}/>tail
        </label>
      </div>
      <div className='capture-toolbar' style={{paddingTop: 0, paddingBottom: 8}}>
        <span className='muted' style={{fontSize: 11}}>{PRESETS[preset]?.hint || ''}</span>
        <div className='spacer' style={{flex: 1}}/>
        <SearchInput
          value={displayFilter}
          onChange={v => store.setCaptureDisplayFilter(device.id, v)}
          placeholder='Display filter — e.g. proto:tls and port:443 and not ip:127.0.0.1'
        />
      </div>
      {parsed.error && (
        <div className='muted' style={{padding: '0 18px 4px', fontSize: 11, color: 'var(--err)'}}>
          filter: {parsed.error}
        </div>
      )}

      <div className='capture-layout'>
        <div className='capture-list' ref={listRef}
             onScroll={e => { setScrollTop(e.currentTarget.scrollTop); setViewH(e.currentTarget.clientHeight); }}>
          <div className='capture-row capture-head'>
            <span>No</span><span>Time</span><span>Source</span><span>Destination</span><span>Proto</span><span>Len</span><span>Info</span>
          </div>
          <div style={{position: 'relative', height: total * CAPTURE_ROW_H}}>
            {visible.map((p, idx) => {
              const i = winStart + idx;
              return (
                <div key={p.no} className={`capture-row${selected?.no === p.no ? ' selected' : ''}`}
                     style={{position: 'absolute', top: i * CAPTURE_ROW_H, left: 0, right: 0}}
                     onClick={() => setSelected(p)}>
                  <span className='mono'>{p.no}</span>
                  <span className='mono'>{fmtTime(p.ts)}</span>
                  <span className='mono' title={`${p.srcIP}:${p.srcPort}`}>{p.srcIP}{p.srcPort ? ':' + p.srcPort : ''}</span>
                  <span className='mono' title={`${p.dstIP}:${p.dstPort}`}>{p.dstIP}{p.dstPort ? ':' + p.dstPort : ''}</span>
                  <span><Badge kind={protoBadge(p.proto)}>{p.proto}</Badge></span>
                  <span className='mono'>{p.length}</span>
                  <span className='truncate' title={p.info}>{p.info}</span>
                </div>
              );
            })}
          </div>
          {filtered.length === 0 && (
            <div className='muted' style={{padding: 16, fontSize: 12}}>
              {active ? 'Waiting for packets…' : packets.length > 0 ? `Display filter matches 0 of ${packets.length.toLocaleString()} packets.` : 'Press Start to begin capturing.'}
            </div>
          )}
        </div>

        <div className='capture-detail'>
          {detail ? (
            <>
              <div className='capture-detail-head'>
                <div><Badge kind={protoBadge(detail.proto)}>{detail.proto}</Badge> <span className='mono'>#{detail.no}</span> · {detail.length} bytes</div>
                <div className='mono muted' style={{fontSize: 11, marginTop: 4}}>
                  {detail.srcIP}{detail.srcPort ? ':' + detail.srcPort : ''} → {detail.dstIP}{detail.dstPort ? ':' + detail.dstPort : ''}
                </div>
                <div className='mono' style={{fontSize: 11, marginTop: 4, wordBreak: 'break-all'}}>{detail.info}</div>
              </div>

              <div className='capture-tree'>
                {detail.layersFull?.map((l, i) => (
                  <div key={i} className='capture-tree-layer'>
                    <button className='capture-tree-header' onClick={() => toggleLayer(i)}>
                      <span className='caret'>{openLayers.has(i) ? '▾' : '▸'}</span>
                      <strong>{l.name}</strong>
                      <span className='muted' style={{fontSize: 10, marginLeft: 'auto'}}>{l.bytes}B @ off {l.offset}</span>
                    </button>
                    {openLayers.has(i) && (
                      <div className='capture-tree-fields'>
                        {l.fields?.map((f, j) => (
                          <div key={j} className='capture-tree-row'>
                            <span className='k'>{f.k}</span>
                            <span className='v mono'>{f.v}</span>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                ))}
              </div>

              {detail.rawHex && (
                <div style={{marginTop: 14}}>
                  <div className='muted' style={{fontSize: 11, marginBottom: 4, textTransform: 'uppercase', letterSpacing: '0.05em'}}>Hex dump</div>
                  <pre className='capture-hex'>{detail.rawHex}</pre>
                </div>
              )}
            </>
          ) : <div className='muted' style={{padding: 16, fontSize: 12}}>Select a packet to inspect</div>}
        </div>
      </div>
    </div>
  );
}

function fmtTime(ts: string): string {
  if (!ts) return '';
  try {
    const d = new Date(ts);
    const h = String(d.getHours()).padStart(2, '0');
    const m = String(d.getMinutes()).padStart(2, '0');
    const s = String(d.getSeconds()).padStart(2, '0');
    const ms = String(d.getMilliseconds()).padStart(3, '0');
    return `${h}:${m}:${s}.${ms}`;
  } catch { return ts; }
}

function fmtBytes(n: number): string {
  if (n < 1024) return n + 'B';
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + 'KB';
  if (n < 1024 * 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + 'MB';
  return (n / 1024 / 1024 / 1024).toFixed(2) + 'GB';
}

function protoBadge(p: string): 'ok' | 'err' | 'info' | 'accent' | undefined {
  switch (p) {
    case 'TLS': case 'QUIC': return 'accent';
    case 'HTTP': return 'info';
    case 'DNS': return 'ok';
    case 'ICMP': case 'ICMPv6': return 'err';
    default: return undefined;
  }
}
