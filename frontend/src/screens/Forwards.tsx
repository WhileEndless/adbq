import React, {useEffect, useState} from 'react';
import {adb} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {Icon} from '../icons';
import {Modal, confirmDialog, showToast} from '../ui';

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
  const [fwd, setFwd] = useState<adb.Forward[]>([]);
  const [rev, setRev] = useState<adb.Forward[]>([]);
  const [add, setAdd] = useState<null | 'fwd' | 'rev'>(null);
  const [aLocal, setALocal] = useState('tcp:8080');
  const [aRemote, setARemote] = useState('tcp:8080');
  const [showCmds, setShowCmds] = useState(false);

  const reload = () => {
    if (!device?.id) return;
    API.ListForwards(device.id).then(f => setFwd(f || [])).catch(() => {});
    API.ListReverses(device.id).then(f => setRev(f || [])).catch(() => {});
  };
  useEffect(reload, [device?.id]);

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
    if (!await confirmDialog({
      title: `Remove all ${dir === 'fwd' ? 'forwards' : 'reverses'}?`,
      body: `${list.length} mapping${list.length === 1 ? '' : 's'} will be cleared.`,
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
        <div className='spacer' style={{flex: 1}}/>
        <button className={`btn sm${showCmds ? ' primary' : ''}`} onClick={() => setShowCmds(v => !v)} title='Toggle equivalent adb commands'>
          <Icon.Terminal/>Show commands
        </button>
        <button className='btn' onClick={reload}><Icon.Refresh/>Reload</button>
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
          showCmds={showCmds}
          cmdRow={f => `adb -s ${device.id} forward ${f.local} ${f.remote}`}
          cmdRemove={f => `adb -s ${device.id} forward --remove ${f.local}`}
          cmdList={`adb -s ${device.id} forward --list`}
        />

        <ForwardsTable
          title='Reverse · device → host'
          subtitle='Device serves on a host port (device-side localhost forwards back)'
          rows={rev} arrow='←'
          onAdd={() => { setAdd('rev'); setALocal('tcp:8080'); setARemote('tcp:8080'); }}
          onRemove={f => removeRow('rev', f)}
          onRemoveAll={() => removeAll('rev')}
          showCmds={showCmds}
          cmdRow={f => `adb -s ${device.id} reverse ${f.remote} ${f.local}`}
          cmdRemove={f => `adb -s ${device.id} reverse --remove ${f.remote}`}
          cmdList={`adb -s ${device.id} reverse --list`}
          style={{marginTop: 12}}
        />
      </div>

      <Modal open={!!add} onClose={() => setAdd(null)} title={`New ${add === 'fwd' ? 'forward (host → device)' : 'reverse (device → host)'}`}
             footer={<><button className='btn' onClick={() => setAdd(null)}>Cancel</button><button className='btn primary' onClick={submitAdd}>Add</button></>}>
        <div style={{display: 'grid', gap: 10}}>
          <div className='field'><label>Local (host)</label><input className='input mono' value={aLocal} onChange={e => setALocal(e.target.value)} placeholder='tcp:8080'/></div>
          <div className='field'><label>Remote (device)</label><input className='input mono' value={aRemote} onChange={e => setARemote(e.target.value)} placeholder='tcp:8080 or localabstract:foo'/></div>
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
  title, subtitle, rows, arrow, onAdd, onRemove, onRemoveAll,
  showCmds, cmdRow, cmdRemove, cmdList, style,
}: {
  title: string; subtitle: string; rows: adb.Forward[]; arrow: '→' | '←';
  onAdd: () => void; onRemove: (f: adb.Forward) => void; onRemoveAll: () => void;
  showCmds: boolean;
  cmdRow: (f: adb.Forward) => string; cmdRemove: (f: adb.Forward) => string; cmdList: string;
  style?: React.CSSProperties;
}) {
  return (
    <div className='card' style={style}>
      <div className='card-header'>
        <div>
          <div className='title'>{title}</div>
          <div className='muted' style={{fontSize: 11, marginTop: 1}}>{subtitle}</div>
        </div>
        <span className='muted' style={{marginLeft: 'auto', fontSize: 11}}>{rows.length} active</span>
        <button className='btn sm' onClick={onAdd}><Icon.Plus/>New</button>
        <button className='btn sm danger' onClick={onRemoveAll} disabled={rows.length === 0}>Remove all</button>
      </div>
      {rows.length === 0 ? (
        <div style={{padding: 20, textAlign: 'center'}} className='muted'>
          No active mappings. {showCmds && <div className='mono' style={{marginTop: 8, fontSize: 11}}>{cmdList}</div>}
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
                    <button className='btn sm danger' onClick={() => onRemove(f)}><Icon.Trash/></button>
                  </td>
                </tr>
                {showCmds && (
                  <tr><td colSpan={4} className='mono' style={{fontSize: 10.5, color: 'var(--text-subtle)', paddingTop: 0}}>
                    <span style={{opacity: 0.7}}>$</span> {cmdRow(f)}
                    {'   '}
                    <span style={{opacity: 0.7}}>$</span> {cmdRemove(f)}
                  </td></tr>
                )}
              </React.Fragment>
            ))}
            {showCmds && (
              <tr><td colSpan={4} className='mono subtle' style={{fontSize: 10.5, paddingTop: 6, borderTop: '1px solid var(--border)'}}>
                <span style={{opacity: 0.7}}>$</span> {cmdList}
              </td></tr>
            )}
          </tbody>
        </table>
      )}
    </div>
  );
}
