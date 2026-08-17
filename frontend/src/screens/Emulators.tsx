// Emulators: the host-side half of adbq. Everything here describes this
// computer's Android SDK — AVDs, system images, the rooting tool — so the screen
// stays usable with no device attached (see HOST_SCREENS in App.tsx).
import React, {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import {adb} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {EventsOn} from '../../wailsjs/runtime/runtime';
import {Icon} from '../icons';
import {Badge, CodeBlock, CommandChip, CommandPreview, FeatureNotice, IconBtn, Modal, SearchInput, Switch, confirmDialog, showToast} from '../ui';

type Tab = 'avds' | 'images' | 'root' | 'host';

export function EmulatorsScreen() {
  const [tab, setTab] = useState<Tab>('avds');
  const [sdk, setSdk] = useState<adb.AndroidSDKInfo | null>(null);
  const [checking, setChecking] = useState(false);

  const loadSdk = useCallback((recheck = false) => {
    setChecking(true);
    (recheck ? API.RecheckAndroidSDK() : API.AndroidSDK())
      .then(setSdk)
      .catch(e => showToast({title: 'SDK check failed', body: String(e), kind: 'err'}))
      .finally(() => setChecking(false));
  }, []);
  useEffect(() => { loadSdk(); }, [loadSdk]);

  const tabs: {id: Tab; label: string; icon: React.ReactNode}[] = [
    {id: 'avds',   label: 'AVDs',          icon: <Icon.Phone/>},
    {id: 'images', label: 'System images', icon: <Icon.Layers/>},
    {id: 'root',   label: 'Root & certs',  icon: <Icon.Shield/>},
    {id: 'host',   label: 'Host',          icon: <Icon.Settings/>},
  ];

  return (
    <div className='screen'>
      <div className='screen-header'>
        <h1>Emulators</h1>
        <span className='subtitle mono'>
          {sdk?.sdkRoot || 'no SDK'}{sdk?.emulatorVer ? ` · emulator ${sdk.emulatorVer}` : ''}
        </span>
        {sdk && !sdk.accelerated && <Badge kind='warn'>no acceleration</Badge>}
        <div className='spacer' style={{flex: 1}}/>
        <button className='btn' onClick={() => loadSdk(true)}>
          <Icon.Refresh className={checking ? 'spin' : ''}/>Recheck
        </button>
      </div>

      <div className='tabbar'>
        {tabs.map(t => (
          <button key={t.id} className={`tabbar-tab${tab === t.id ? ' active' : ''}`} onClick={() => setTab(t.id)}>
            {t.icon}{t.label}
          </button>
        ))}
      </div>

      <div className='screen-body' style={{paddingTop: 16}}>
        {/* The nav entry is never hidden: an absent SDK is explained here, where
            the user can also point adbq at one. */}
        {sdk && !sdk.available && tab !== 'host'
          ? <FeatureNotice state={{kind: 'unavailable', reason: sdk.error || 'No Android SDK found on this computer.'}}/>
          : <>
              {tab === 'avds'   && <AVDsTab sdk={sdk}/>}
              {tab === 'images' && <ImagesTab/>}
              {tab === 'root'   && <RootTab/>}
              {tab === 'host'   && <HostTab sdk={sdk} onChanged={setSdk} checking={checking} recheck={() => loadSdk(true)}/>}
            </>}
      </div>
    </div>
  );
}

// ─── shared bits ───────────────────────────────────────────────────────────

const STATE_BADGE: Record<string, {kind?: string; label: string}> = {
  running: {kind: 'ok',     label: 'running'},
  booting: {kind: 'accent', label: 'booting'},
  stopped: {kind: undefined, label: 'stopped'},
  offline: {kind: 'warn',   label: 'offline'},
  error:   {kind: 'err',    label: 'broken'},
};

function StateBadge({state}: {state: string}) {
  const s = STATE_BADGE[state] || {label: state};
  return <Badge kind={s.kind}>{s.label}</Badge>;
}

function humanBytes(n: number): string {
  if (!n) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = n, i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${units[i]}`;
}

/**
 * Run onDone when a background task of one of these kinds stops running.
 *
 * Installing an image or rooting an AVD happens in the task tray, so without
 * this the screen that started the work keeps showing the state from before it.
 * The unsubscribe function EventsOn returns is what gets called on unmount —
 * EventsOff would take the task tray's own listener down with it.
 */
function useTaskDone(kinds: string, onDone: () => void) {
  const saved = useRef(onDone);
  saved.current = onDone;
  useEffect(() => {
    const wanted = new Set(kinds.split(' '));
    return EventsOn('task:update', (t: adb.TaskState) => {
      if (wanted.has(t.kind) && t.status !== 'running') saved.current();
    });
  }, [kinds]);
}

/** Poll while anything is mid-transition; idle lists don't need a timer. */
function usePolling(active: boolean, fn: () => void, ms: number) {
  const saved = useRef(fn);
  saved.current = fn;
  useEffect(() => {
    if (!active) return;
    const t = setInterval(() => saved.current(), ms);
    return () => clearInterval(t);
  }, [active, ms]);
}

// ─── AVDs ──────────────────────────────────────────────────────────────────

function AVDsTab({sdk}: {sdk: adb.AndroidSDKInfo | null}) {
  const [avds, setAvds] = useState<adb.AVD[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [selected, setSelected] = useState<string>('');
  const [creating, setCreating] = useState(false);
  const [q, setQ] = useState('');

  const reload = useCallback(() => {
    API.ListAVDs()
      .then(list => { setAvds(list ?? []); setError(''); })
      .catch(e => setError(String(e)))
      .finally(() => setLoading(false));
  }, []);
  useEffect(() => { reload(); }, [reload]);

  // A booting AVD changes state on its own; a settled list does not.
  const settling = avds.some(a => a.state === 'booting');
  usePolling(true, reload, settling ? 3000 : 10000);

  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return avds;
    return avds.filter(a =>
      a.name.toLowerCase().includes(needle) ||
      (a.display || '').toLowerCase().includes(needle) ||
      (a.tag || '').toLowerCase().includes(needle) ||
      String(a.api).includes(needle));
  }, [avds, q]);

  const current = avds.find(a => a.name === selected) || null;

  if (loading) return <FeatureNotice state={{kind: 'loading'}}/>;
  if (error) return <FeatureNotice state={{kind: 'error', message: error, retry: reload}}/>;

  return (
    <div style={{display: 'flex', gap: 16, alignItems: 'flex-start'}}>
      <div style={{flex: 1, minWidth: 0}}>
        <div style={{display: 'flex', gap: 8, marginBottom: 12}}>
          <SearchInput value={q} onChange={setQ} placeholder='Filter AVDs…' style={{flex: 1}}/>
          <button className='btn primary' onClick={() => setCreating(true)}><Icon.Plus/>New AVD</button>
        </div>

        {filtered.length === 0
          ? <FeatureNotice state={{kind: 'empty', hint: avds.length
              ? 'No AVD matches that filter.'
              : 'No AVDs defined yet. Create one to get started.'}}/>
          : filtered.map(a => (
              <AVDRow key={a.name} avd={a} sdk={sdk}
                      selected={a.name === selected}
                      onSelect={() => setSelected(a.name === selected ? '' : a.name)}
                      onChanged={reload}/>
            ))}
      </div>

      {current && <AVDDetail avd={current} onChanged={reload} onClose={() => setSelected('')}/>}
      {creating && <CreateAVDModal onClose={() => setCreating(false)} onCreated={() => { setCreating(false); reload(); }}/>}
    </div>
  );
}

function AVDRow({avd, sdk, selected, onSelect, onChanged}: {
  avd: adb.AVD; sdk: adb.AndroidSDKInfo | null; selected: boolean; onSelect: () => void; onChanged: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [opts, setOpts] = useState<adb.EmulatorOpts>({} as adb.EmulatorOpts);
  const [showOpts, setShowOpts] = useState(false);
  const launchCmd = useLaunchCommand(avd.name, opts, showOpts);

  const running = avd.state === 'running' || avd.state === 'booting';

  const start = (extra?: Partial<adb.EmulatorOpts>) => {
    setBusy(true);
    API.StartAVD(avd.name, {...opts, ...extra} as adb.EmulatorOpts)
      .then(serial => showToast({title: `${avd.name} starting`, body: serial, kind: 'ok', mono: true}))
      .catch(e => showToast({title: 'Start failed', body: String(e), kind: 'err'}))
      .finally(() => { setBusy(false); onChanged(); });
  };
  const stop = () => {
    setBusy(true);
    API.StopAVD(avd.name)
      .then(() => showToast({title: `${avd.name} stopping`, kind: 'ok'}))
      .catch(e => showToast({title: 'Stop failed', body: String(e), kind: 'err'}))
      .finally(() => { setBusy(false); onChanged(); });
  };
  const wipe = async () => {
    const ok = await confirmDialog({
      title: `Wipe data on ${avd.name}?`,
      body: 'Everything installed on this AVD — apps, accounts, files — is erased and it boots factory-fresh.\n\nThis cannot be undone.',
      confirmLabel: 'Wipe and start', danger: true,
    });
    if (ok) start({wipeData: true, coldBoot: true});
  };

  return (
    <div className='card' style={{marginBottom: 8, borderColor: selected ? 'var(--accent)' : undefined}}>
      <div className='card-body' style={{padding: 12}}>
        <div style={{display: 'flex', alignItems: 'center', gap: 10}}>
          <div style={{flex: 1, minWidth: 0, cursor: 'pointer'}} onClick={onSelect}>
            <div style={{display: 'flex', alignItems: 'center', gap: 7, flexWrap: 'wrap'}}>
              <strong style={{fontSize: 13}}>{avd.display || avd.name}</strong>
              <StateBadge state={avd.state}/>
              {/* A variant, not a fault — amber would read as "something is
                  wrong with this AVD". What it costs you is said in words,
                  next to the choices it actually affects. */}
              {avd.playStore ? <Badge kind='info'>Play Store</Badge> : <Badge>{avd.tagDisplay || avd.tag || 'AOSP'}</Badge>}
              {avd.patched && <Badge kind='accent'>patched</Badge>}
              {avd.root === 'adb-root' && <Badge kind='ok'>root</Badge>}
              {avd.root === 'su' && <Badge kind='ok'>su</Badge>}
            </div>
            <div className='mono subtle' style={{fontSize: 11, marginTop: 3}}>
              API {avd.api || '?'} · {avd.androidVer} · {avd.abi} · {avd.device || 'generic'}
              {avd.serial ? ` · ${avd.serial}` : ''}
            </div>
            {!!avd.error && <div style={{color: 'var(--err)', fontSize: 11, marginTop: 4}}>{avd.error}</div>}
            {!!avd.warning && <div className='warn-text' style={{fontSize: 11, marginTop: 4}}>{avd.warning}</div>}
          </div>

          <div style={{display: 'flex', gap: 4, flexShrink: 0}}>
            {running
              ? <button className='btn sm danger' disabled={busy} onClick={stop}><Icon.Stop/>Stop</button>
              : <>
                  <button className='btn sm primary' disabled={busy || !!avd.error} onClick={() => start()}><Icon.Play/>Start</button>
                  <button className='btn sm' disabled={busy || !!avd.error} title='Boot without loading the saved snapshot'
                          onClick={() => start({coldBoot: true})}>Cold</button>
                </>}
            <IconBtn title='Boot options' active={showOpts} onClick={() => setShowOpts(o => !o)}>
              <Icon.Settings width={13} height={13}/>
            </IconBtn>
          </div>
        </div>

        {showOpts && (
          <div style={{marginTop: 10, paddingTop: 10, borderTop: '1px solid var(--border)'}}>
            <BootOptions opts={opts} onChange={setOpts} snapshots={avd.snapshots ?? []}/>
            <div style={{marginTop: 8, display: 'flex', gap: 6, alignItems: 'center'}}>
              {/* No locally assembled fallback: until Go answers there is no
                  command to show, and inventing one risks showing a line that is
                  not what would run (CLAUDE.md §4.1). */}
              <CommandChip label='Launch' commands={launchCmd ? [launchCmd] : []}/>
              <div style={{flex: 1}}/>
              <button className='btn sm danger' disabled={busy} onClick={wipe}><Icon.Trash/>Wipe data</button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

/**
 * The launch command is rendered by Go from the same EmulatorOpts that Start
 * receives, so what the user copies is provably what runs (CLAUDE.md §4.1). It
 * is never assembled here.
 */
function useLaunchCommand(name: string, opts: adb.EmulatorOpts, active: boolean): string {
  const [cmd, setCmd] = useState('');
  useEffect(() => {
    if (!active) return;
    let live = true;
    API.EmulatorLaunchCommand(name, opts)
      .then(c => { if (live) setCmd(c); })
      .catch(() => { if (live) setCmd(''); });
    return () => { live = false; };
  }, [name, opts, active]);
  return cmd;
}

function BootOptions({opts, onChange, snapshots}: {
  opts: adb.EmulatorOpts; onChange: (o: adb.EmulatorOpts) => void; snapshots: string[];
}) {
  const set = (patch: Partial<adb.EmulatorOpts>) => onChange({...opts, ...patch} as adb.EmulatorOpts);
  const toggles: {key: keyof adb.EmulatorOpts; label: string; hint: string}[] = [
    {key: 'coldBoot',       label: 'Cold boot',       hint: 'Ignore the saved snapshot for this boot'},
    {key: 'noSnapshotSave', label: 'Discard on exit', hint: 'Do not write state back on shutdown'},
    {key: 'noWindow',       label: 'Headless',        hint: 'No emulator window'},
    {key: 'noBootAnim',     label: 'No boot anim',    hint: 'Boots faster'},
    {key: 'writableSystem', label: 'Writable /system', hint: 'Needed to modify the system partition at runtime'},
    {key: 'readOnly',       label: 'Read-only',       hint: 'Allows several instances of one AVD; disables snapshots'},
  ];

  return (
    <div>
      <div style={{display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(190px, 1fr))', gap: 6}}>
        {toggles.map(t => (
          <label key={String(t.key)} title={t.hint}
                 style={{display: 'flex', alignItems: 'center', gap: 7, fontSize: 12, cursor: 'pointer'}}>
            <Switch on={!!(opts as any)[t.key]} onChange={v => set({[t.key]: v} as any)}/>
            <span>{t.label}</span>
          </label>
        ))}
      </div>
      <div style={{display: 'flex', gap: 8, marginTop: 8, flexWrap: 'wrap'}}>
        <div className='field' style={fitCell}>
          <label>GPU</label>
          <select className='input' style={fitInput} value={opts.gpu || ''} onChange={e => set({gpu: e.target.value})}>
            <option value=''>AVD default</option>
            {['auto', 'host', 'swiftshader_indirect', 'angle_indirect', 'off'].map(g => <option key={g} value={g}>{g}</option>)}
          </select>
        </div>
        {snapshots.length > 0 && (
          <div className='field' style={fitCell}>
            <label>Snapshot</label>
            <select className='input' style={fitInput} value={opts.snapshot || ''} onChange={e => set({snapshot: e.target.value})}>
              <option value=''>default</option>
              {snapshots.map(s => <option key={s} value={s}>{s}</option>)}
            </select>
          </div>
        )}
        <div className='field' style={fitCell}>
          <label>Proxy</label>
          <input className='input mono' style={{width: 150}} placeholder='127.0.0.1:8080'
                 value={opts.httpProxy || ''} onChange={e => set({httpProxy: e.target.value})}/>
        </div>
        <div className='field' style={fitCell}>
          <label>DNS</label>
          <input className='input mono' style={{width: 130}} placeholder='1.1.1.1'
                 value={opts.dns || ''} onChange={e => set({dns: e.target.value})}/>
        </div>
      </div>
    </div>
  );
}

// ─── AVD detail: hardware, snapshots, log ──────────────────────────────────

function AVDDetail({avd, onChanged, onClose}: {avd: adb.AVD; onChanged: () => void; onClose: () => void}) {
  return (
    <div className='card' style={{width: 360, flexShrink: 0, position: 'sticky', top: 0}}>
      <div className='card-header'>
        <span className='title'>{avd.display || avd.name}</span>
        <div style={{flex: 1}}/>
        <IconBtn title='Close' onClick={onClose}><Icon.X width={13} height={13}/></IconBtn>
      </div>
      <div className='card-body' style={{maxHeight: '70vh', overflow: 'auto'}}>
        <Facts avd={avd}/>
        <HardwareEditor avd={avd} onSaved={onChanged}/>
        <SnapshotList avd={avd} onChanged={onChanged}/>
        <EmulatorLogPanel name={avd.name} live={avd.state === 'booting' || avd.state === 'running'}/>
        <DangerZone avd={avd} onChanged={onChanged}/>
      </div>
    </div>
  );
}

function Facts({avd}: {avd: adb.AVD}) {
  const rows: [string, React.ReactNode][] = [
    ['State', <StateBadge state={avd.state}/>],
    ['Serial', avd.serial || '—'],
    ['API', `${avd.api || '?'} · ${avd.androidVer}`],
    ['Image', avd.tagDisplay || avd.tag || '—'],
    ['ABI', avd.abi || '—'],
    ['Device', avd.device ? `${avd.device}${avd.deviceMfr ? ` (${avd.deviceMfr})` : ''}` : '—'],
    ['RAM', avd.ramMB ? `${avd.ramMB} MB` : '—'],
    ['CPU cores', avd.cores || '—'],
    ['Screen', avd.resolution ? `${avd.resolution} @ ${avd.density || '?'} dpi` : '—'],
    ['Data partition', avd.dataSize || '—'],
    ['SD card', avd.sdCard || '—'],
    ['GPU', avd.gpuMode || '—'],
    ['Disk used', humanBytes(avd.diskBytes)],
    ['Root', avd.root || '—'],
  ];
  return (
    <div style={{marginBottom: 14}}>
      <table style={{width: '100%', fontSize: 11.5}}>
        <tbody>
          {rows.map(([k, v]) => (
            <tr key={k}>
              <td className='muted' style={{padding: '2px 8px 2px 0', whiteSpace: 'nowrap', verticalAlign: 'top'}}>{k}</td>
              <td className='mono' style={{padding: '2px 0', wordBreak: 'break-all'}}>{v}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {!!avd.path && (
        <div style={{marginTop: 8}}>
          <div className='muted' style={{fontSize: 11, marginBottom: 3}}>On disk:</div>
          <CodeBlock multiline>{avd.path}</CodeBlock>
        </div>
      )}
      {!!avd.sysImgDir && (
        <div style={{marginTop: 6}}>
          <div className='muted' style={{fontSize: 11, marginBottom: 3}}>System image (shared):</div>
          <CodeBlock multiline>{avd.sysImgDir}</CodeBlock>
        </div>
      )}
    </div>
  );
}

function HardwareEditor({avd, onSaved}: {avd: adb.AVD; onSaved: () => void}) {
  const [open, setOpen] = useState(false);
  const [hw, setHw] = useState<adb.AVDHardware>({} as adb.AVDHardware);
  const [changes, setChanges] = useState<Record<string, string>>({});
  const [invalid, setInvalid] = useState('');
  const [saving, setSaving] = useState(false);

  // The list behind this panel refreshes every few seconds, handing down a new
  // AVD object each time. Seeding from a ref means the form is filled once, when
  // it opens — keying the effect on `avd` would wipe half-typed values on the
  // next poll, which is exactly what a user notices and cannot explain.
  const latest = useRef(avd);
  latest.current = avd;
  useEffect(() => {
    if (!open) return;
    const a = latest.current;
    const [w, h] = (a.resolution || '').split('x').map(n => parseInt(n, 10) || 0);
    setHw({
      ramMB: a.ramMB, cores: a.cores, dataSize: a.dataSize, sdCard: a.sdCard,
      gpuMode: a.gpuMode, width: w, height: h, density: a.density, keyboard: a.keyboard,
    } as unknown as adb.AVDHardware);
  }, [open]);

  // The backend decides which config.ini keys an edit writes; show exactly
  // those, and its rejection reason when a value is out of range — a silently
  // empty change list would leave the user guessing why Save does nothing.
  useEffect(() => {
    if (!open) return;
    API.AVDHardwareChanges(hw)
      .then(c => { setChanges(c ?? {}); setInvalid(''); })
      .catch(e => { setChanges({}); setInvalid(String(e)); });
  }, [open, hw]);

  const set = (patch: Partial<adb.AVDHardware>) => setHw({...hw, ...patch} as adb.AVDHardware);
  const save = () => {
    setSaving(true);
    API.UpdateAVDHardware(avd.name, hw)
      .then(() => { showToast({title: 'Saved', body: 'Applies the next time this AVD boots.', kind: 'ok'}); setOpen(false); onSaved(); })
      .catch(e => showToast({title: 'Could not save', body: String(e), kind: 'err'}))
      .finally(() => setSaving(false));
  };

  return (
    <div style={{marginBottom: 14}}>
      <button className='btn sm' style={{width: '100%'}} onClick={() => setOpen(o => !o)}>
        <Icon.Cpu width={12} height={12}/>{open ? 'Close hardware settings' : 'Edit hardware'}
      </button>
      {open && (
        <div style={{marginTop: 8}}>
          <div style={{display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(120px, 1fr))', gap: 6}}>
            <NumField label='RAM (MB)' value={hw.ramMB} onChange={v => set({ramMB: v})}/>
            <NumField label='CPU cores' value={hw.cores} onChange={v => set({cores: v})}/>
            <TextField label='Data partition' value={hw.dataSize} placeholder='8G' onChange={v => set({dataSize: v})}/>
            <TextField label='SD card' value={hw.sdCard} placeholder='512M' onChange={v => set({sdCard: v})}/>
            <NumField label='Width' value={hw.width} onChange={v => set({width: v})}/>
            <NumField label='Height' value={hw.height} onChange={v => set({height: v})}/>
            <NumField label='Density (dpi)' value={hw.density} onChange={v => set({density: v})}/>
            <div className='field' style={fitCell}>
              <label>GPU mode</label>
              <select className='input' style={fitInput} value={hw.gpuMode || ''} onChange={e => set({gpuMode: e.target.value})}>
                <option value=''>unchanged</option>
                {['auto', 'host', 'swiftshader_indirect', 'angle_indirect', 'guest', 'off'].map(g => <option key={g} value={g}>{g}</option>)}
              </select>
            </div>
          </div>

          <label style={{display: 'flex', alignItems: 'center', gap: 7, fontSize: 12, marginTop: 8, cursor: 'pointer'}}>
            <Switch on={!!hw.keyboard} onChange={v => set({keyboard: v})}/>
            <span>Hardware keyboard</span>
          </label>

          {!!invalid && (
            <div style={{color: 'var(--err)', fontSize: 11, marginTop: 8, lineHeight: 1.5}}>{invalid}</div>
          )}
          {Object.keys(changes).length > 0 && (
            <div style={{marginTop: 8}}>
              <span className='muted' style={{fontSize: 11}}>config.ini changes:</span>{' '}
              <CodeBlock multiline>{Object.entries(changes).map(([k, v]) => `${k}=${v}`).join('\n')}</CodeBlock>
            </div>
          )}
          <div className='muted' style={{fontSize: 11, marginTop: 6}}>
            Takes effect the next time this AVD boots.
          </div>
          <button className='btn sm primary' style={{width: '100%', marginTop: 8}} disabled={saving || !!invalid} onClick={save}>
            Save hardware settings
          </button>
        </div>
      )}
    </div>
  );
}

// `.input` sets no width and a grid/flex item defaults to min-width:auto, so a
// bare input keeps its ~170px intrinsic width and pushes its container wider
// than the modal. Both halves of that have to be undone for the field to fit.
const fitCell: React.CSSProperties = {margin: 0, minWidth: 0};
const fitInput: React.CSSProperties = {width: '100%', minWidth: 0, boxSizing: 'border-box'};

function NumField({label, value, onChange}: {label: string; value?: number; onChange: (v: number) => void}) {
  return (
    <div className='field' style={fitCell}>
      <label>{label}</label>
      <input className='input mono' style={fitInput} type='number' value={value || ''}
             onChange={e => onChange(parseInt(e.target.value, 10) || 0)}/>
    </div>
  );
}
function TextField({label, value, placeholder, onChange}: {label: string; value?: string; placeholder?: string; onChange: (v: string) => void}) {
  return (
    <div className='field' style={fitCell}>
      <label>{label}</label>
      <input className='input mono' style={fitInput} value={value || ''} placeholder={placeholder}
             onChange={e => onChange(e.target.value)}/>
    </div>
  );
}

function SnapshotList({avd, onChanged}: {avd: adb.AVD; onChanged: () => void}) {
  const snaps = avd.snapshots ?? [];
  if (snaps.length === 0) return null;
  const del = async (s: string) => {
    const ok = await confirmDialog({
      title: `Delete snapshot “${s}”?`,
      body: `The saved state in ${avd.name}/snapshots/${s} is removed. This cannot be undone.`,
      confirmLabel: 'Delete', danger: true,
    });
    if (!ok) return;
    API.DeleteAVDSnapshot(avd.name, s)
      .then(() => { showToast({title: 'Snapshot deleted', kind: 'ok'}); onChanged(); })
      .catch(e => showToast({title: 'Delete failed', body: String(e), kind: 'err'}));
  };
  return (
    <div style={{marginBottom: 14}}>
      <div className='muted' style={{fontSize: 11, marginBottom: 4}}>Snapshots</div>
      {snaps.map(s => (
        <div key={s} style={{display: 'flex', alignItems: 'center', gap: 6, padding: '3px 0'}}>
          <span className='mono' style={{flex: 1, fontSize: 11, wordBreak: 'break-all'}}>{s}</span>
          <IconBtn title={`Delete snapshot ${s}`} onClick={() => del(s)}><Icon.Trash width={12} height={12}/></IconBtn>
        </div>
      ))}
    </div>
  );
}

/**
 * The emulator's own output, kept apart from device logcat. Lines are pulled by
 * sequence cursor and only while the panel is open, so a closed panel costs
 * nothing and a long-running AVD can't grow this list without bound.
 */
function EmulatorLogPanel({name, live}: {name: string; live: boolean}) {
  const [open, setOpen] = useState(false);
  const [lines, setLines] = useState<adb.HostLogLine[]>([]);
  const cursor = useRef(0);

  const pull = useCallback(() => {
    API.EmulatorLog(name, cursor.current)
      .then(next => {
        const fresh = next ?? [];
        if (fresh.length === 0) return;
        cursor.current = fresh[fresh.length - 1].seq;
        setLines(prev => [...prev, ...fresh].slice(-1500));
      })
      .catch(() => {});
  }, [name]);

  useEffect(() => { cursor.current = 0; setLines([]); }, [name]);
  useEffect(() => { if (open) pull(); }, [open, pull]);
  usePolling(open && live, pull, 2000);

  return (
    <div style={{marginBottom: 14}}>
      <button className='btn sm' style={{width: '100%'}} onClick={() => setOpen(o => !o)}>
        <Icon.Terminal width={12} height={12}/>{open ? 'Hide emulator log' : 'Emulator log'}
      </button>
      {open && (
        <div style={{marginTop: 6}}>
          <div className='mono' style={{
            background: 'var(--bg-inset)', border: '1px solid var(--border)', borderRadius: 4,
            padding: 8, fontSize: 10.5, maxHeight: 220, overflow: 'auto', whiteSpace: 'pre-wrap', wordBreak: 'break-all',
          }}>
            {lines.length === 0
              ? <span className='muted'>No output yet.</span>
              : lines.map(l => (
                  <div key={l.seq} style={{color: l.err ? 'var(--err)' : undefined}}>{l.text}</div>
                ))}
          </div>
          <button className='btn sm ghost' style={{marginTop: 4}}
                  onClick={() => { API.ClearEmulatorLog(name); cursor.current = 0; setLines([]); }}>
            Clear
          </button>
        </div>
      )}
    </div>
  );
}

function DangerZone({avd, onChanged}: {avd: adb.AVD; onChanged: () => void}) {
  const del = async () => {
    const cmd = await API.DeleteAVDCommand(avd.name).catch(() => '');
    const ok = await confirmDialog({
      title: `Delete ${avd.name}?`,
      body: (
        <div style={{fontSize: 12}}>
          <div>The AVD and everything in it — {humanBytes(avd.diskBytes)} of data, apps and snapshots — are removed permanently.</div>
          <CommandPreview commands={cmd ? [cmd] : []} defaultOpen/>
        </div>
      ),
      confirmLabel: 'Delete AVD', danger: true,
    });
    if (!ok) return;
    API.DeleteAVD(avd.name)
      .then(() => { showToast({title: `${avd.name} deleted`, kind: 'ok'}); onChanged(); })
      .catch(e => showToast({title: 'Delete failed', body: String(e), kind: 'err'}));
  };
  return (
    <div style={{borderTop: '1px solid var(--border)', paddingTop: 10}}>
      <button className='btn sm danger' style={{width: '100%'}} onClick={del}>
        <Icon.Trash width={12} height={12}/>Delete this AVD
      </button>
    </div>
  );
}

// ─── create ────────────────────────────────────────────────────────────────

function CreateAVDModal({onClose, onCreated}: {onClose: () => void; onCreated: () => void}) {
  const [images, setImages] = useState<adb.SystemImage[]>([]);
  const [devices, setDevices] = useState<adb.DeviceProfile[]>([]);
  const [spec, setSpec] = useState<adb.AVDSpec>({name: '', pkg: ''} as adb.AVDSpec);
  const [form, setForm] = useState('phone');
  const [cmd, setCmd] = useState('');
  const [busy, setBusy] = useState(false);
  const touchedName = useRef(false);

  // Picking an image fills the whole form: the emulator's own defaults (1.5 GB
  // RAM, 2 cores) make a modern image feel broken, and a blank form gives the
  // user nothing to go on. A name the user has typed is never overwritten.
  const chooseImage = useCallback((pkg: string) => {
    setSpec(prev => ({...prev, pkg} as adb.AVDSpec));
    if (!pkg) return;
    API.DefaultAVDSpec(pkg).then(d => {
      setSpec(prev => ({...d, name: touchedName.current ? prev.name : d.name} as adb.AVDSpec));
    }).catch(() => {});
  }, []);

  // Open with a complete, valid proposal rather than a blank form: preselect the
  // newest image this computer can actually run and fill the hardware from it.
  useEffect(() => {
    API.ListInstalledSystemImages().then(l => {
      const list = l ?? [];
      setImages(list);
      const first = list.find(i => i.compatible);
      if (first) chooseImage(first.pkg);
    }).catch(() => {});
    API.ListDeviceProfiles().then(l => setDevices(l ?? [])).catch(() => {});
  }, [chooseImage]);

  useEffect(() => {
    if (!spec.name || !spec.pkg) { setCmd(''); return; }
    API.CreateAVDCommand(spec).then(setCmd).catch(() => setCmd(''));
  }, [spec]);

  const create = () => {
    setBusy(true);
    API.CreateAVD(spec)
      .then(a => {
        // The AVD exists either way; a warning means the hardware settings did
        // not land, which is worth more than the three seconds a toast lasts.
        showToast(a.warning
          ? {title: `${a.name} created with a problem`, body: a.warning, kind: 'err', ttl: 12000}
          : {title: `${a.name} created`, kind: 'ok'});
        onCreated();
      })
      .catch(e => showToast({title: 'Create failed', body: String(e), kind: 'err'}))
      .finally(() => setBusy(false));
  };

  // Only runnable images can be chosen; the rest would install and then never
  // boot, so they are excluded here rather than explained after the fact.
  const usable = images.filter(i => i.compatible);
  const chosen = images.find(i => i.pkg === spec.pkg) || null;

  // The SDK ships ~90 device definitions, mostly wear/TV/automotive/headset.
  // Default to phones and let the user widen it.
  const formFactors = useMemo(() => {
    const seen = new Set(devices.map(d => d.formFactor));
    return ['phone', 'tablet', 'foldable', 'wear', 'tv', 'automotive', 'desktop', 'xr'].filter(f => seen.has(f));
  }, [devices]);
  const shownDevices = useMemo(
    () => devices.filter(d => form === 'all' || d.formFactor === form),
    [devices, form]);

  return (
    <Modal open onClose={onClose} title='New AVD' width={580}
           footer={<>
             <button className='btn' onClick={onClose}>Cancel</button>
             <button className='btn primary' disabled={busy || !spec.name || !spec.pkg} onClick={create}>Create</button>
           </>}>
      <div className='field'>
        <label>System image</label>
        <select className='input' style={fitInput} value={spec.pkg} onChange={e => chooseImage(e.target.value)}>
          <option value=''>Choose an installed image…</option>
          {usable.map(i => (
            <option key={i.pkg} value={i.pkg}>
              {i.androidVer} · {i.tag} · {i.abi}{i.playStore ? ' (Play Store)' : ''}
            </option>
          ))}
        </select>
        {usable.length === 0 && (
          <div className='muted' style={{fontSize: 11, marginTop: 3}}>
            {images.length === 0
              ? 'No system images installed. Install one from the System images tab first.'
              : 'None of the installed images can run on this computer — install one matching its architecture.'}
          </div>
        )}
        {/* The rooting story hinges on this, so say it where the choice is made. */}
        {chosen?.playStore && (
          <div className='warn-text' style={{fontSize: 11, marginTop: 4}}>
            Play Store images refuse <span className='mono'>adb root</span>. Rooting one needs rootAVD (Root &amp; certs tab).
          </div>
        )}
        {!!chosen?.note && <div className='warn-text' style={{fontSize: 11, marginTop: 4}}>{chosen.note}</div>}
      </div>

      <div className='field'>
        <label>Name</label>
        <input className='input mono' style={fitInput} value={spec.name} placeholder='Android_34_GApis'
               onChange={e => { touchedName.current = true; setSpec({...spec, name: e.target.value} as adb.AVDSpec); }}/>
        <div className='muted' style={{fontSize: 11, marginTop: 3}}>Letters, digits, dot, dash and underscore only.</div>
      </div>

      <div className='field'>
        <label>Device profile</label>
        <div style={{display: 'flex', gap: 6, minWidth: 0}}>
          <select className='input' style={{width: 120, flexShrink: 0}} value={form} onChange={e => setForm(e.target.value)}>
            {formFactors.map(f => <option key={f} value={f}>{f}</option>)}
            <option value='all'>all ({devices.length})</option>
          </select>
          <select className='input' style={{flex: 1, minWidth: 0}} value={spec.device}
                  onChange={e => setSpec({...spec, device: e.target.value} as adb.AVDSpec)}>
            <option value=''>avdmanager default</option>
            {shownDevices.map(d => (
              <option key={d.id} value={d.id}>{d.name}{d.recommended ? ' — recommended' : ''}</option>
            ))}
          </select>
        </div>
      </div>

      <div style={{display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(120px, 1fr))', gap: 8}}>
        <NumField label='RAM (MB)' value={spec.ramMB} onChange={v => setSpec({...spec, ramMB: v} as adb.AVDSpec)}/>
        <NumField label='CPU cores' value={spec.cores} onChange={v => setSpec({...spec, cores: v} as adb.AVDSpec)}/>
        <TextField label='Data' value={spec.dataSize} placeholder='8G' onChange={v => setSpec({...spec, dataSize: v} as adb.AVDSpec)}/>
        <TextField label='SD card' value={spec.sdCard} placeholder='512M' onChange={v => setSpec({...spec, sdCard: v} as adb.AVDSpec)}/>
      </div>
      <div style={{display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))', gap: 8, marginTop: 8}}>
        <div className='field' style={fitCell}>
          <label>GPU mode</label>
          <select className='input' style={fitInput} value={spec.gpuMode || ''} onChange={e => setSpec({...spec, gpuMode: e.target.value} as adb.AVDSpec)}>
            <option value=''>image default</option>
            {['auto', 'host', 'swiftshader_indirect', 'angle_indirect', 'guest', 'off'].map(g => <option key={g} value={g}>{g}</option>)}
          </select>
        </div>
        <label style={{display: 'flex', alignItems: 'center', gap: 7, fontSize: 12, marginTop: 18}}>
          <Switch on={!!spec.keyboard} onChange={v => setSpec({...spec, keyboard: v} as adb.AVDSpec)}/>
          <span>Hardware keyboard</span>
        </label>
      </div>

      {!!cmd && (
        <div style={{marginTop: 12}}>
          <CommandPreview commands={cmd ? [cmd] : []} defaultOpen/>
          <div className='muted' style={{fontSize: 11, marginTop: 4}}>
            RAM, cores, data size, GPU and keyboard have no avdmanager flags — adbq writes them
            into <span className='mono'>config.ini</span> right after creation, as Android Studio does.
          </div>
        </div>
      )}
    </Modal>
  );
}

// ─── system images ─────────────────────────────────────────────────────────

function ImagesTab() {
  const [images, setImages] = useState<adb.SystemImage[]>([]);
  const [loading, setLoading] = useState(true);
  const [warn, setWarn] = useState('');
  const [q, setQ] = useState('');
  const [onlyInstalled, setOnlyInstalled] = useState(true);
  const [onlyPlay, setOnlyPlay] = useState(false);
  // An image whose ABI this computer cannot run installs fine and then never
  // boots, so the default view hides them rather than explaining afterwards.
  const [onlyRunnable, setOnlyRunnable] = useState(true);

  const load = useCallback((refresh: boolean) => {
    setLoading(true);
    // The installed half is instant and offline; show it first so the screen is
    // never blank while the remote catalogue is fetched.
    API.ListInstalledSystemImages().then(l => setImages(prev => (prev.length ? prev : (l ?? [])))).catch(() => {});
    API.ListSystemImages(refresh)
      .then(l => { setImages(l ?? []); setWarn(''); })
      .catch(e => setWarn(String(e)))
      .finally(() => setLoading(false));
  }, []);
  useEffect(() => { load(false); }, [load]);
  // A finished download changes what is on disk, and the row it came from still
  // says "Install" until the list is re-read.
  useTaskDone('sdk-install', () => load(false));

  const filtered = useMemo(() => images.filter(i => {
    if (onlyInstalled && !i.installed) return false;
    if (onlyPlay && !i.playStore) return false;
    if (onlyRunnable && !i.compatible) return false;
    const needle = q.trim().toLowerCase();
    if (!needle) return true;
    return i.pkg.toLowerCase().includes(needle) || (i.androidVer || '').toLowerCase().includes(needle);
  }), [images, q, onlyInstalled, onlyPlay, onlyRunnable]);

  const install = async (img: adb.SystemImage) => {
    const ok = await confirmDialog({
      title: `Install ${img.pkg}?`,
      body: `This downloads a system image from Google's SDK repository — typically 1–2 GB.\n\nBy installing it you accept the Android SDK licence for this package. adbq never accepts licences on your behalf; if it has not been accepted yet the install will stop and tell you.\n\n${img.commands?.[0] ?? ''}`,
      confirmLabel: 'Download and install',
    });
    if (!ok) return;
    API.InstallSystemImage(img.pkg)
      .then(() => showToast({title: 'Download started', body: 'Progress is in the task tray.', kind: 'ok'}))
      .catch(e => showToast({title: 'Install failed', body: String(e), kind: 'err'}));
  };
  const uninstall = async (img: adb.SystemImage) => {
    const ok = await confirmDialog({
      title: `Remove ${img.pkg}?`,
      body: `The system image is deleted from disk. Any AVD using it stops working until it is reinstalled.\n\n${img.commands?.[0] ?? ''}`,
      confirmLabel: 'Remove', danger: true,
    });
    if (!ok) return;
    API.UninstallSystemImage(img.pkg)
      .then(() => { showToast({title: 'Removed', kind: 'ok'}); load(true); })
      .catch(e => showToast({title: 'Remove failed', body: String(e), kind: 'err'}));
  };

  return (
    <div>
      <div style={{display: 'flex', gap: 10, alignItems: 'center', marginBottom: 12, flexWrap: 'wrap'}}>
        <SearchInput value={q} onChange={setQ} placeholder='Filter images…' style={{flex: 1, minWidth: 180}}/>
        <label style={{display: 'flex', alignItems: 'center', gap: 6, fontSize: 12}}>
          <Switch on={onlyInstalled} onChange={setOnlyInstalled}/>Installed only
        </label>
        <label style={{display: 'flex', alignItems: 'center', gap: 6, fontSize: 12}}>
          <Switch on={onlyPlay} onChange={setOnlyPlay}/>Play Store
        </label>
        <label style={{display: 'flex', alignItems: 'center', gap: 6, fontSize: 12}}
               title='Hide images whose CPU architecture this computer cannot run'>
          <Switch on={onlyRunnable} onChange={setOnlyRunnable}/>Runnable here
        </label>
        <button className='btn' onClick={() => load(true)}><Icon.Refresh className={loading ? 'spin' : ''}/>Refresh</button>
      </div>

      {!!warn && (
        <div className='card' style={{marginBottom: 10, padding: 10, fontSize: 11.5}}>
          <span className='muted'>Showing installed images only — the SDK catalogue could not be read: </span>{warn}
        </div>
      )}

      {filtered.length === 0
        ? <FeatureNotice state={{kind: 'empty', hint: loading ? 'Loading the SDK catalogue…' : 'No image matches those filters.'}}/>
        : (
          <div className='card'>
            <table className='table'>
              <thead>
                <tr>
                  <th>Android</th>
                  <th>Tag</th>
                  <th>ABI</th>
                  <th>Rev</th>
                  <th/>
                  <th className='actions'/>
                </tr>
              </thead>
              <tbody>
                {filtered.slice(0, 400).map(i => (
                  <tr key={i.pkg}>
                    <td style={{whiteSpace: 'nowrap'}}>{i.androidVer}</td>
                    <td>
                      {i.playStore ? <Badge kind='info'>Play Store</Badge> : <span className='mono subtle'>{i.tag}</span>}
                    </td>
                    <td className='mono subtle' title={i.note || undefined}>
                      {i.abi}
                      {!i.compatible && <span style={{color: 'var(--err)', marginLeft: 4}}>✕</span>}
                    </td>
                    <td className='mono subtle'>{i.revision || '—'}</td>
                    <td>{i.installed && <Badge kind='ok'>installed</Badge>}</td>
                    <td className='actions'>
                      {i.installed
                        ? <button className='btn sm ghost' onClick={() => uninstall(i)}><Icon.Trash width={11} height={11}/>Remove</button>
                        : <button className='btn sm' disabled={!i.compatible} title={i.note || undefined}
                                  onClick={() => install(i)}><Icon.Download width={11} height={11}/>Install</button>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {filtered.length > 400 && (
              <div className='muted' style={{padding: 8, fontSize: 11}}>
                Showing the first 400 of {filtered.length} — narrow the filter to see the rest.
              </div>
            )}
          </div>
        )}
    </div>
  );
}

// ─── root & certs ──────────────────────────────────────────────────────────

function RootTab() {
  const [info, setInfo] = useState<adb.RootAVDInfo | null>(null);
  const [avds, setAvds] = useState<adb.AVD[]>([]);
  const [name, setName] = useState('');
  const [advice, setAdvice] = useState<Record<string, string> | null>(null);
  const [busy, setBusy] = useState(false);

  const reload = useCallback(() => {
    API.RootAVDInfo().then(setInfo).catch(() => {});
    API.ListAVDs().then(l => setAvds(l ?? [])).catch(() => {});
  }, []);
  useEffect(() => { reload(); }, [reload]);
  // Everything on this tab is a statement about live state — is it running, does
  // it have root, is the image patched — and all three change from elsewhere:
  // the AVDs tab, the task tray, the emulator itself.
  usePolling(true, reload, 8000);
  useTaskDone('avd-root avd-restore rootavd-download', reload);

  const avd = avds.find(a => a.name === name) || null;
  // Re-asked only when an input to the answer moved, not on every poll: the
  // advice call probes the device, and the list identity changes regardless.
  useEffect(() => {
    if (!name) { setAdvice(null); return; }
    API.RootAVDAdvice(name).then(a => setAdvice(a ?? null)).catch(() => setAdvice(null));
  }, [name, avd?.state, avd?.root, avd?.patched]);

  const offered = advice?.offered === 'true';

  const download = async () => {
    if (!info) return;
    const ok = await confirmDialog({
      title: 'Download rootAVD?',
      body: [
        ...(info.disclosures ?? []),
        '',
        `Source: ${info.archive}`,
        `rootAVD.sh SHA-256: ${info.scriptSHA}`,
        `Magisk.zip SHA-256: ${info.magiskSHA}`,
      ].join('\n'),
      confirmLabel: 'Download and verify',
    });
    if (!ok) return;
    setBusy(true);
    API.DownloadRootAVD()
      .then(i => { setInfo(i); showToast({title: 'rootAVD verified', body: i.dir, kind: 'ok', mono: true}); })
      .catch(e => showToast({title: 'Download failed', body: String(e), kind: 'err'}))
      .finally(() => setBusy(false));
  };

  const root = async () => {
    if (!avd) return;
    const cmd = await API.RootAVDCommand(avd.name, false).catch(() => '');
    const ok = await confirmDialog({
      title: `Root ${avd.name}?`,
      body: (
        <div style={{fontSize: 12}}>
          <div>This patches the shared system image, not just this AVD:</div>
          <div className='mono' style={{margin: '4px 0'}}>{avd.sysImgDir}</div>
          <div>Every AVD using that image is affected. A ramdisk backup is written next to it and Restore undoes the change.</div>
          <div style={{marginTop: 6}}>{avd.name} is shut down and cold-booted at the end.</div>
          <CommandPreview commands={cmd ? [cmd] : []} defaultOpen/>
        </div>
      ),
      confirmLabel: 'Root this AVD', danger: true,
    });
    if (!ok) return;
    API.RootAVD(avd.name)
      .then(() => showToast({title: 'Rooting started', body: 'Progress is in the task tray.', kind: 'ok'}))
      .catch(e => showToast({title: 'Could not start', body: String(e), kind: 'err'}));
  };

  const restore = async () => {
    if (!avd) return;
    const cmd = await API.RootAVDCommand(avd.name, true).catch(() => '');
    const ok = await confirmDialog({
      title: `Restore ${avd.name}'s system image?`,
      body: (
        <div style={{fontSize: 12}}>
          <div>The original ramdisk is put back from the backup, removing Magisk from every AVD using this image.</div>
          <CommandPreview commands={cmd ? [cmd] : []} defaultOpen/>
        </div>
      ),
      confirmLabel: 'Restore', danger: true,
    });
    if (!ok) return;
    API.RestoreAVDRamdisk(avd.name)
      .then(() => showToast({title: 'Restore started', body: 'Progress is in the task tray.', kind: 'ok'}))
      .catch(e => showToast({title: 'Could not start', body: String(e), kind: 'err'}));
  };

  const removeTool = async () => {
    const ok = await confirmDialog({title: 'Remove the downloaded rootAVD?', body: 'It can be downloaded again at any time.', confirmLabel: 'Remove'});
    if (!ok) return;
    API.RemoveRootAVD().then(reload).catch(e => showToast({title: 'Remove failed', body: String(e), kind: 'err'}));
  };

  return (
    <div style={{display: 'grid', gap: 14, gridTemplateColumns: 'repeat(auto-fit, minmax(340px, 1fr))', alignItems: 'start'}}>
      <div className='card'>
        <div className='card-header'><span className='title'>rootAVD</span>
          <div style={{flex: 1}}/>
          {info?.installed ? <Badge kind='ok'>downloaded</Badge> : <Badge>not downloaded</Badge>}
        </div>
        <div className='card-body'>
          <div className='muted' style={{fontSize: 11.5, lineHeight: 1.55}}>
            A third-party tool ({info?.license}) that installs Magisk into an emulator system image.
            adbq does not ship it — it is fetched from a pinned commit and verified by SHA-256 before it runs.
          </div>
          <div style={{marginTop: 8}}>
            <CodeBlock multiline>{`${info?.source ?? ''}\ncommit ${info?.commit?.slice(0, 12) ?? ''}`}</CodeBlock>
          </div>
          <ul className='muted' style={{fontSize: 11, lineHeight: 1.6, margin: '10px 0 0', paddingLeft: 16}}>
            {(info?.disclosures ?? []).map((d, i) => <li key={i}>{d}</li>)}
          </ul>
          {/* rootAVD is a bash script. Saying so before the download beats
              failing with "exec: bash: not found" after it. */}
          {!!info && !info.runner && (
            <div className='warn-text' style={{fontSize: 11, marginTop: 10, lineHeight: 1.5}}>{info.runnerNote}</div>
          )}
          <div style={{display: 'flex', gap: 6, marginTop: 10}}>
            {info?.installed
              ? <button className='btn sm ghost' onClick={removeTool}><Icon.Trash width={11} height={11}/>Remove</button>
              : <button className='btn sm primary' disabled={busy || (!!info && !info.runner)} onClick={download}>
                  <Icon.Download width={11} height={11}/>Download &amp; verify
                </button>}
          </div>
        </div>
      </div>

      <div className='card'>
        <div className='card-header'><span className='title'>Root an AVD</span></div>
        <div className='card-body'>
          <div className='field'>
            <label>AVD</label>
            <select className='input' value={name} onChange={e => setName(e.target.value)}>
              <option value=''>Choose an AVD…</option>
              {avds.map(a => <option key={a.name} value={a.name}>{a.display || a.name} · API {a.api}{a.playStore ? ' · Play Store' : ''}</option>)}
            </select>
          </div>

          {advice && (
            <div style={{
              marginTop: 8, padding: 10, borderRadius: 4, fontSize: 11.5, lineHeight: 1.5,
              background: 'var(--bg-inset)', border: '1px solid var(--border)',
              borderLeft: `3px solid var(--${advice.action === 'unsupported' ? 'err' : advice.action === 'risky' ? 'warn' : advice.action === 'eligible' ? 'accent' : 'ok'})`,
            }}>
              <strong style={{textTransform: 'capitalize'}}>{advice.action.replace('-', ' ')}</strong>
              <div className='muted' style={{marginTop: 3}}>{advice.reason}</div>
            </div>
          )}

          <div style={{display: 'flex', gap: 6, marginTop: 10, flexWrap: 'wrap'}}>
            <button className='btn sm primary' disabled={!avd || !offered || !info?.installed || !info?.runner} onClick={root}>
              <Icon.Shield width={11} height={11}/>Root this AVD
            </button>
            <button className='btn sm' disabled={!avd?.patched || !info?.runner} onClick={restore}>Restore original image</button>
          </div>
          {!info?.installed && <div className='muted' style={{fontSize: 11, marginTop: 6}}>Download rootAVD first.</div>}
        </div>
      </div>

      <CertCard avd={avd}/>
    </div>
  );
}

/**
 * Certificate installation reuses adbq's existing InstallSystemCert, which
 * already picks between six placement strategies. Once the AVD has root there is
 * nothing rootAVD-specific left to do here.
 */
function CertCard({avd}: {avd: adb.AVD | null}) {
  const [result, setResult] = useState<adb.CertInstallResult | null>(null);
  const [busy, setBusy] = useState(false);
  const rooted = avd?.root === 'adb-root' || avd?.root === 'su';

  const install = () => {
    if (!avd?.serial) return;
    setBusy(true);
    API.InstallSystemCertWithPicker(avd.serial)
      .then(r => { setResult(r); showToast({title: 'Certificate installed', body: r.note || r.strategy, kind: 'ok'}); })
      .catch(e => showToast({title: 'Install failed', body: String(e), kind: 'err'}))
      .finally(() => setBusy(false));
  };

  return (
    <div className='card'>
      <div className='card-header'><span className='title'>System CA certificate</span></div>
      <div className='card-body'>
        <div className='muted' style={{fontSize: 11.5, lineHeight: 1.55}}>
          Installs an interception CA (Burp, mitmproxy, ZAP) into the device trust store, so apps
          accept it without a per-app network-security config. Needs root on the AVD.
        </div>
        {!avd && <div className='muted' style={{fontSize: 11, marginTop: 8}}>Choose an AVD above.</div>}
        {avd && avd.state !== 'running' && (
          <div className='warn-text' style={{fontSize: 11, marginTop: 8}}>{avd.name} is not running — start it first.</div>
        )}
        {avd && avd.state === 'running' && !rooted && (
          <div className='warn-text' style={{fontSize: 11, marginTop: 8}}>{avd.name} has no root yet — root it first.</div>
        )}
        <button className='btn sm primary' style={{width: '100%', marginTop: 10}}
                disabled={busy || !avd || avd.state !== 'running' || !rooted} onClick={install}>
          <Icon.Shield width={12} height={12}/>Choose certificate and install
        </button>
        {result && (
          <div style={{marginTop: 10, fontSize: 11.5}}>
            <div><span className='muted'>Subject: </span><span className='mono'>{result.subject}</span></div>
            <div><span className='muted'>Strategy: </span><span className='mono'>{result.strategy}</span></div>
            <div style={{marginTop: 4}}>
              {result.persistent
                ? <Badge kind='ok'>survives reboot</Badge>
                : <Badge kind='warn'>lost on reboot</Badge>}
            </div>
            {!!result.note && <div className='muted' style={{marginTop: 4}}>{result.note}</div>}
          </div>
        )}
      </div>
    </div>
  );
}

// ─── host ──────────────────────────────────────────────────────────────────

function HostTab({sdk, onChanged, checking, recheck}: {
  sdk: adb.AndroidSDKInfo | null; onChanged: (i: adb.AndroidSDKInfo) => void; checking: boolean; recheck: () => void;
}) {
  const pick = () => {
    API.PickSDKRoot().then(p => {
      if (!p) return;
      return API.SetSDKRoot(p).then(i => {
        onChanged(i);
        // Whether an SDK is available is the wrong question: a folder that
        // isn't one is dropped and adbq falls back to auto-detection, so with
        // another SDK on the machine "available" would report success for a
        // setting that changed nothing. Only source === 'setting' means the
        // choice took.
        const took = i.source === 'setting';
        showToast({
          title: took ? 'SDK path set' : 'That folder is not an Android SDK',
          body: took ? i.sdkRoot : `Ignored — still using ${i.sdkRoot || 'no SDK'}${i.source ? ` (${i.source})` : ''}.`,
          kind: took ? 'ok' : 'err',
          mono: took,
        });
      });
    }).catch(e => showToast({title: 'Could not set the SDK path', body: String(e), kind: 'err'}));
  };
  const clear = () => {
    API.SetSDKRoot('').then(i => { onChanged(i); showToast({title: 'Back to auto-detection', body: i.sdkRoot, kind: 'ok'}); })
      .catch(e => showToast({title: 'Failed', body: String(e), kind: 'err'}));
  };

  const rows: [string, React.ReactNode][] = [
    ['SDK root', sdk?.sdkRoot ? <span className='mono'>{sdk.sdkRoot}</span> : <span className='muted'>not found</span>],
    ['Resolved from', sdk?.source || '—'],
    ['Emulator', sdk?.emulator ? `${sdk.emulatorVer || '?'}` : '—'],
    ['avdmanager', sdk?.avdManager ? 'found' : 'missing'],
    ['sdkmanager', sdk?.sdkManager ? 'found' : 'missing'],
    ['AVD home', sdk?.avdHome || '—'],
    ['Acceleration', sdk?.accelerated ? <Badge kind='ok'>{sdk.accelNote || 'available'}</Badge> : <Badge kind='warn'>{sdk?.accelNote || 'unavailable'}</Badge>],
    ['Android Studio', sdk?.studioPath ? `${sdk.studioVer || 'installed'}` : 'not installed'],
  ];

  return (
    <div style={{display: 'grid', gap: 14, gridTemplateColumns: 'repeat(auto-fit, minmax(340px, 1fr))', alignItems: 'start'}}>
      <div className='card'>
        <div className='card-header'>
          <span className='title'>Android SDK</span>
          <div style={{flex: 1}}/>
          {sdk?.available ? <Badge kind='ok'>ready</Badge> : <Badge kind='err'>unavailable</Badge>}
        </div>
        <div className='card-body'>
          <table style={{width: '100%', fontSize: 11.5}}>
            <tbody>
              {rows.map(([k, v]) => (
                <tr key={k}>
                  <td className='muted' style={{padding: '3px 8px 3px 0', whiteSpace: 'nowrap', verticalAlign: 'top'}}>{k}</td>
                  <td style={{padding: '3px 0', wordBreak: 'break-all'}}>{v}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {!!sdk?.error && <div className='warn-text' style={{fontSize: 11.5, marginTop: 8, lineHeight: 1.5}}>{sdk.error}</div>}
          <div style={{display: 'flex', gap: 6, marginTop: 12, flexWrap: 'wrap'}}>
            <button className='btn sm' onClick={pick}><Icon.Folder width={11} height={11}/>Choose SDK folder</button>
            {sdk?.source === 'setting' && <button className='btn sm ghost' onClick={clear}>Use auto-detection</button>}
            <button className='btn sm ghost' onClick={recheck}><Icon.Refresh className={checking ? 'spin' : ''} width={11} height={11}/>Recheck</button>
            {!!sdk?.studioPath && (
              <button className='btn sm ghost' onClick={() => API.OpenAndroidStudio().catch(e => showToast({title: 'Could not open Android Studio', body: String(e), kind: 'err'}))}>
                Open Android Studio
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
