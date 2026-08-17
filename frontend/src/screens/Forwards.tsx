import React, {useEffect, useState} from 'react';
import {adb} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {Icon} from '../icons';
import {CommandChip, CommandPreview, Modal, confirmDialog, showToast} from '../ui';
import {useDeviceData} from '../cache';

// Curated bidirectional preset catalogue. `dir` decides which adb call we
// make ("forward" host→device, "reverse" device→host). Kept together in one
// list so the picker shows both directions side-by-side.
const PRESETS: {label: string; local: string; remote: string; dir: 'fwd' | 'rev'}[] = [
  {label: 'Chrome DevTools',          local: 'tcp:9222',  remote: 'localabstract:chrome_devtools_remote', dir: 'fwd'},
  {label: 'Node inspector',           local: 'tcp:9229',  remote: 'tcp:9229',                              dir: 'fwd'},
  {label: 'frida-server',             local: 'tcp:27042', remote: 'tcp:27042',                             dir: 'fwd'},
  {label: 'Burp Suite (reverse)',     local: 'tcp:8080',  remote: 'tcp:8080',                              dir: 'rev'},
  {label: 'Burp Suite alt 8888',      local: 'tcp:8888',  remote: 'tcp:8888',                              dir: 'rev'},
  {label: 'mitmproxy (reverse)',      local: 'tcp:8080',  remote: 'tcp:8080',                              dir: 'rev'},
  {label: 'Metro bundler (reverse)',  local: 'tcp:8081',  remote: 'tcp:8081',                              dir: 'rev'},
  {label: 'Charles (reverse)',        local: 'tcp:8888',  remote: 'tcp:8888',                              dir: 'rev'},
];

export function ForwardsScreen({device}: {device: adb.Device}) {
  const [add, setAdd] = useState<null | 'fwd' | 'rev'>(null);
  const [aLocal, setALocal] = useState('tcp:8080');
  const [aRemote, setARemote] = useState('tcp:8080');

  const {data, refreshing, error, refresh} = useDeviceData(
    device?.id ? `forwards:${device.id}` : null,
    async () => {
      const [f, r] = await Promise.all([API.ListForwards(device.id), API.ListReverses(device.id)]);
      return {fwd: f || [], rev: r || []};
    },
    {staleMs: 8000},
  );
  const fwd = data?.fwd || [];
  const rev = data?.rev || [];
  const reload = refresh;

  // adb spells a forward and a reverse in opposite orders, so the commands come
  // from the backend rather than being re-assembled here (CLAUDE.md §4.1).
  const [fwdCmds, setFwdCmds] = useState<adb.ForwardCommands[]>([]);
  const [revCmds, setRevCmds] = useState<adb.ForwardCommands[]>([]);
  useEffect(() => {
    if (!device?.id) return;
    let live = true;
    Promise.all([
      API.ForwardCommands(device.id, 'forward', fwd),
      API.ForwardCommands(device.id, 'reverse', rev),
    ]).then(([f, r]) => { if (live) { setFwdCmds(f || []); setRevCmds(r || []); } })
      .catch(() => { if (live) { setFwdCmds([]); setRevCmds([]); } });
    return () => { live = false; };
    // Depends on `data`, not on the derived arrays: those get a fresh identity
    // on every render and would re-fetch forever.
  }, [device?.id, data]);

  // The command the Add dialog would run, live as the specs are typed.
  const [addCmds, setAddCmds] = useState<string[]>([]);
  useEffect(() => {
    if (!add || !device?.id) { setAddCmds([]); return; }
    let live = true;
    API.ForwardCommands(device.id, add === 'fwd' ? 'forward' : 'reverse',
      [{local: aLocal, remote: aRemote} as adb.Forward])
      .then(r => { if (live) setAddCmds(r?.[0]?.add ?? []); })
      .catch(() => { if (live) setAddCmds([]); });
    return () => { live = false; };
  }, [add, device?.id, aLocal, aRemote]);

  function applyPreset(p: typeof PRESETS[number]) {
    const call = p.dir === 'rev'
      ? API.AddReverse(device.id, p.remote, p.local)
      : API.AddForward(device.id, p.local, p.remote);
    call.then(() => { showToast({title: 'Preset applied', body: p.label, kind: 'ok'}); reload(); })
        .catch(e => showToast({title: 'Apply failed', body: String(e), kind: 'err'}));
  }
  function submitAdd() {
    if (!add) return;
    const call = add === 'fwd'
      ? API.AddForward(device.id, aLocal, aRemote)
      : API.AddReverse(device.id, aRemote, aLocal);
    call.then(() => { setAdd(null); reload(); })
        .catch(e => showToast({title: 'Add failed', body: String(e), kind: 'err'}));
  }
  function removeRow(dir: 'fwd' | 'rev', f: adb.Forward) {
    const call = dir === 'fwd' ? API.RemoveForward(device.id, f.local) : API.RemoveReverse(device.id, f.remote);
    call.then(reload).catch(e => showToast({title: 'Remove failed', body: String(e), kind: 'err'}));
  }
  async function removeAll(dir: 'fwd' | 'rev') {
    const list = dir === 'fwd' ? fwd : rev;
    if (list.length === 0) return;
    const sets = dir === 'fwd' ? fwdCmds : revCmds;
    if (!await confirmDialog({
      title: `Remove all ${dir === 'fwd' ? 'forwards' : 'reverses'}?`,
      body: <>
        {list.length} mapping{list.length === 1 ? '' : 's'} will be cleared.
        <CommandPreview commands={sets.flatMap(c => c.remove ?? [])} defaultOpen/>
      </>,
      danger: true, confirmLabel: 'Remove all',
    })) return;
    await Promise.all(list.map(f =>
      dir === 'fwd' ? API.RemoveForward(device.id, f.local) : API.RemoveReverse(device.id, f.remote)));
    reload();
  }

  return (
    <div className='screen'>
      <div className='screen-header'>
        <h1>ADB Forwards <span className='subtitle mono'>{fwd.length} fwd · {rev.length} rev</span></h1>
        {!!error && <span style={{color: 'var(--err)', fontSize: 11}}>load failed</span>}
        <div className='spacer' style={{flex: 1}}/>
        <button className='btn' onClick={reload}><Icon.Refresh className={refreshing ? 'spin' : ''}/>Reload</button>
      </div>

      <div className='screen-body'>
        <div className='card' style={{marginBottom: 12}}>
          <div className='card-header'>
            <div className='title'>Quick presets</div>
            <span className='muted' style={{fontSize: 11}}>one-click setup</span>
          </div>
          <div className='card-body' style={{display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 6}}>
            <div>
              <div className='muted' style={{fontSize: 11, marginBottom: 4, textTransform: 'uppercase', letterSpacing: '0.05em'}}>Forward (host → device)</div>
              <div style={{display: 'flex', flexWrap: 'wrap', gap: 4}}>
                {PRESETS.filter(p => p.dir === 'fwd').map(p =>
                  <button key={p.label} className='btn sm' onClick={() => applyPreset(p)} title={`${p.local} → ${p.remote}`}>{p.label}</button>)}
              </div>
            </div>
            <div>
              <div className='muted' style={{fontSize: 11, marginBottom: 4, textTransform: 'uppercase', letterSpacing: '0.05em'}}>Reverse (device → host)</div>
              <div style={{display: 'flex', flexWrap: 'wrap', gap: 4}}>
                {PRESETS.filter(p => p.dir === 'rev').map(p =>
                  <button key={p.label} className='btn sm' onClick={() => applyPreset(p)} title={`${p.local} ← ${p.remote}`}>{p.label}</button>)}
              </div>
            </div>
          </div>
        </div>

        <ForwardsTable
          title='Forward · host → device'
          subtitle='Host (computer) listens, device serves'
          rows={fwd} arrow='→'
          onAdd={() => { setAdd('fwd'); setALocal('tcp:8080'); setARemote('tcp:8080'); }}
          onRemove={f => removeRow('fwd', f)}
          onRemoveAll={() => removeAll('fwd')}
          cmds={fwdCmds}
        />

        <ForwardsTable
          title='Reverse · device → host'
          subtitle='Device serves on a host port (device-side localhost forwards back)'
          rows={rev} arrow='←'
          onAdd={() => { setAdd('rev'); setALocal('tcp:8080'); setARemote('tcp:8080'); }}
          onRemove={f => removeRow('rev', f)}
          onRemoveAll={() => removeAll('rev')}
          cmds={revCmds}
          style={{marginTop: 12}}
        />
      </div>

      <Modal open={!!add} onClose={() => setAdd(null)} title={`New ${add === 'fwd' ? 'forward (host → device)' : 'reverse (device → host)'}`}
             footer={<><button className='btn' onClick={() => setAdd(null)}>Cancel</button><button className='btn primary' onClick={submitAdd}>Add</button></>}>
        <div style={{display: 'grid', gap: 10}}>
          <div className='field'><label>Local (host)</label><input className='input mono' value={aLocal} onChange={e => setALocal(e.target.value)} placeholder='tcp:8080'/></div>
          <div className='field'><label>Remote (device)</label><input className='input mono' value={aRemote} onChange={e => setARemote(e.target.value)} placeholder='tcp:8080 or localabstract:foo'/></div>
          <CommandPreview commands={addCmds} defaultOpen/>
          <div className='muted' style={{fontSize: 11}}>
            {add === 'fwd'
              ? 'host port → device port. Useful for connecting from your computer to a service running on the device.'
              : 'device port ← host port. Useful for proxying device traffic through a tool running on your computer (Burp, mitmproxy, ...).'}
          </div>
        </div>
      </Modal>
    </div>
  );
}

function ForwardsTable({
  title, subtitle, rows, arrow, onAdd, onRemove, onRemoveAll, cmds, style,
}: {
  title: string; subtitle: string; rows: adb.Forward[]; arrow: '→' | '←';
  onAdd: () => void; onRemove: (f: adb.Forward) => void; onRemoveAll: () => void;
  /** One entry per row, in row order, from the backend. */
  cmds: adb.ForwardCommands[];
  style?: React.CSSProperties;
}) {
  const listCmd = cmds[0]?.list ?? [];
  return (
    <div className='card' style={style}>
      <div className='card-header'>
        <div>
          <div className='title'>{title}</div>
          <div className='muted' style={{fontSize: 11, marginTop: 1}}>{subtitle}</div>
        </div>
        <span className='muted' style={{marginLeft: 'auto', fontSize: 11}}>{rows.length} active</span>
        <CommandChip label={title} commands={listCmd}/>
        <button className='btn sm' onClick={onAdd}><Icon.Plus/>New</button>
        <button className='btn sm danger' onClick={onRemoveAll} disabled={rows.length === 0}>Remove all</button>
      </div>
      {rows.length === 0 ? (
        <div style={{padding: 20, textAlign: 'center'}} className='muted'>
          No active mappings.
        </div>
      ) : (
        <table className='table'>
          <thead><tr><th>Host</th><th></th><th>Device</th><th className='actions'></th></tr></thead>
          <tbody>
            {rows.map((f, i) => (
              <React.Fragment key={i}>
                <tr>
                  <td className='mono'>{f.local}</td>
                  <td className='muted' style={{textAlign: 'center', width: 24}}>{arrow}</td>
                  <td className='mono'>{f.remote}</td>
                  <td className='actions'>
                    <CommandChip label={`${f.local} ${arrow} ${f.remote}`} groups={[
                      {label: 'Add', commands: cmds[i]?.add},
                      {label: 'Remove', commands: cmds[i]?.remove},
                    ]}/>
                    <button className='btn sm danger' onClick={() => onRemove(f)}><Icon.Trash/></button>
                  </td>
                </tr>
              </React.Fragment>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
