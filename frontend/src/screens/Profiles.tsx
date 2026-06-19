import React, {useEffect, useState} from 'react';
import {adb} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {Icon} from '../icons';
import {Badge, Dropdown, FeatureNotice, IconBtn, Modal, Switch, showToast, promptDialog, confirmDialog} from '../ui';

// emptyProfile returns a fully-populated blank profile (all step sub-objects
// present) so the editor can bind inputs without undefined checks.
function emptyProfile(): any {
  return {
    id: '', name: '', createdAt: 0, updatedAt: 0,
    frida: {enabled: false, version: '', autoArch: true, arch: '', start: true, iface: '', port: 27042},
    forwards: {enabled: false, forwards: [], reverses: []},
    proxy: {enabled: false, hostPort: '', port: 8080},
    hosts: {enabled: false, content: '', flushDns: true},
    cert: {enabled: false, pem: '', subject: ''},
    iptables: {enabled: false, v4Blob: '', v6Blob: ''},
  };
}

const clone = (p: any) => JSON.parse(JSON.stringify(p));

// deviceKey mirrors Go's adb.DeviceKey: prefer the hardware serial, but treat
// "" / "unknown" (common on emulators where ro.serialno is unset) as absent and
// fall back to the adb id — so the frontend key always matches the backend key.
export function deviceKey(d?: {hardwareSerial?: string; id?: string}): string {
  const hw = (d?.hardwareSerial || '').trim();
  if (hw && hw !== 'unknown') return hw;
  return d?.id || '';
}

// ─── Titlebar profile selector ──────────────────────────────────────────────

export function ProfileSelector({device, refreshKey, onSwitch, onEdit, onNew, onCapture, onManage}: {
  device?: adb.Device;
  refreshKey?: number;
  onSwitch: (serial: string, profileId: string) => void;
  onEdit: (p: adb.Profile) => void;
  onNew: () => void;
  onCapture: () => void;
  onManage: () => void;
}) {
  const [profiles, setProfiles] = useState<adb.Profile[]>([]);
  const [boundId, setBoundId] = useState<string>('');
  const key = deviceKey(device);

  const refresh = () => {
    API.ListProfiles().then(p => setProfiles(p || [])).catch(() => {});
    if (key) API.LookupDeviceProfile(key).then(setBoundId).catch(() => {});
    else setBoundId('');
  };
  useEffect(refresh, [key, refreshKey]);

  const bound = profiles.find(p => p.id === boundId);
  const items = [];
  for (const p of profiles) {
    items.push({
      label: (p.id === boundId ? '● ' : '   ') + p.name,
      onClick: () => device && onSwitch(device.id, p.id),
    });
  }
  if (profiles.length) items.push({label: '', onClick: () => {}, divider: true});
  items.push({
    label: 'Base / no profile', icon: <Icon.X width={13} height={13}/>,
    onClick: () => {
      if (!key) return;
      API.BindDeviceProfileByKey(key, '').then(() => { setBoundId(''); showToast({title: 'Profile cleared', body: 'No profile will auto-apply', kind: 'ok'}); });
    },
  });
  items.push({label: '', onClick: () => {}, divider: true});
  items.push({label: 'New profile…', icon: <Icon.Plus width={13} height={13}/>, onClick: onNew});
  if (bound) items.push({label: 'Edit current…', icon: <Icon.Settings width={13} height={13}/>, onClick: () => onEdit(bound)});
  if (device?.online) items.push({label: 'Capture from this device…', icon: <Icon.Download width={13} height={13}/>, onClick: onCapture});
  items.push({label: 'Manage devices & profiles…', icon: <Icon.Grid width={13} height={13}/>, onClick: onManage});

  const trigger = (
    <button className='btn sm' title='Device profile' style={{display: 'flex', alignItems: 'center', gap: 5, maxWidth: 180}}>
      <Icon.Zap width={13} height={13}/>
      <span style={{overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'}}>{bound ? bound.name : 'No profile'}</span>
    </button>
  );
  return <Dropdown trigger={trigger} items={items}/>;
}

// ─── Apply confirm + report ─────────────────────────────────────────────────

export function ApplyConfirm({serial, profileId, onClose, onApplied, reload}: {
  serial: string;
  profileId: string;
  onClose: () => void;
  onApplied?: () => void;
  reload: () => void;
}) {
  const [name, setName] = useState('profile');
  const [preview, setPreview] = useState<adb.StepPreview[] | null>(null);
  const [report, setReport] = useState<adb.ApplyReport | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    API.GetProfile(profileId).then(p => setName(p.name)).catch(() => {});
    API.PreviewProfile(serial, profileId)
      .then(setPreview)
      .catch(e => { showToast({title: 'Preview failed', body: String(e), kind: 'err'}); onClose(); });
  }, [serial, profileId]);

  const apply = () => {
    setBusy(true);
    API.ApplyProfile(serial, profileId)
      .then(r => { setReport(r); onApplied?.(); })
      .catch(e => { showToast({title: 'Apply failed', body: String(e), kind: 'err'}); setBusy(false); });
  };

  const statusBadge = (s: string) =>
    s === 'ok' ? <Badge kind='ok'>ok</Badge> : s === 'skip' ? <Badge kind='warn'>skip</Badge> : <Badge kind='err'>fail</Badge>;

  return (
    <Modal open onClose={onClose} width={560}
           title={report ? `Applied: ${name}` : `Apply profile: ${name}`}
           footer={report
             ? <>
                 {report.needsReboot && <button className='btn' onClick={() => API.Reboot(serial, '').then(reload).catch(() => {})}>Reboot now</button>}
                 <button className='btn primary' onClick={onClose}>Done</button>
               </>
             : (preview && preview.length === 0)
               ? <button className='btn primary' onClick={onClose}>Close</button>
               : <>
                   <button className='btn' onClick={onClose}>Cancel</button>
                   <button className='btn primary' disabled={busy || !preview} onClick={apply}>
                     {busy ? 'Applying…' : `Apply ${preview?.length || 0} step(s)`}
                   </button>
                 </>}>
      {!report && !preview && <FeatureNotice state={{kind: 'loading'}}/>}
      {!report && preview && preview.length === 0 && (
        <div className='muted' style={{padding: 16}}>This profile has no enabled steps.</div>
      )}
      {!report && preview && preview.map((s, i) => (
        <div key={i} style={{padding: '8px 4px', borderBottom: '1px solid var(--border)', opacity: s.willSkip ? 0.55 : 1}}>
          <div style={{display: 'flex', alignItems: 'center', gap: 8}}>
            <strong style={{fontSize: 13}}>{s.title}</strong>
            {s.needsRoot && <Badge kind='warn'>root</Badge>}
            {s.willSkip && <Badge>will skip</Badge>}
          </div>
          <div className='muted' style={{fontSize: 12, marginTop: 2}}>{s.willSkip ? s.skipReason : s.detail}</div>
        </div>
      ))}
      {report && report.steps?.map((s, i) => (
        <div key={i} style={{display: 'flex', alignItems: 'center', gap: 8, padding: '7px 4px', borderBottom: '1px solid var(--border)'}}>
          {statusBadge(s.status)}
          <div style={{flex: 1}}>
            <strong style={{fontSize: 13}}>{s.name}</strong>
            <div className='muted' style={{fontSize: 12}}>{s.message}</div>
          </div>
          {s.needsReboot && <Badge kind='warn'>reboot</Badge>}
        </div>
      ))}
      {report && report.needsReboot && (
        <div className='card' style={{marginTop: 10, padding: 10, borderColor: 'var(--warn)'}}>
          <strong>Reboot required.</strong> Some changes (Magisk-module hosts / non-persistent cert) take effect after a reboot.
        </div>
      )}
    </Modal>
  );
}

// ─── Profile editor ──────────────────────────────────────────────────────────

function Section({title, on, onToggle, children}: {title: string; on: boolean; onToggle: (v: boolean) => void; children?: React.ReactNode}) {
  return (
    <div style={{border: '1px solid var(--border)', borderRadius: 8, marginBottom: 8}}>
      <div style={{display: 'flex', alignItems: 'center', gap: 8, padding: '8px 10px'}}>
        <Switch on={on} onChange={onToggle}/>
        <strong style={{fontSize: 13}}>{title}</strong>
      </div>
      {on && <div style={{padding: '0 10px 10px', display: 'flex', flexDirection: 'column', gap: 8}}>{children}</div>}
    </div>
  );
}

export function ProfileEditor({initial, device, onClose, onSaved}: {
  initial: adb.Profile | null;
  device?: adb.Device;
  onClose: () => void;
  onSaved: () => void;
}) {
  const del = async () => {
    if (!initial?.id) return;
    if (!await confirmDialog({title: `Delete profile “${initial.name}”?`, body: 'This also unbinds it from any device.', confirmLabel: 'Delete', danger: true})) return;
    API.DeleteProfile(initial.id).then(() => { showToast({title: 'Profile deleted', kind: 'ok'}); onSaved(); onClose(); })
      .catch(e => showToast({title: 'Delete failed', body: String(e), kind: 'err'}));
  };
  const [d, setD] = useState<any>(() => (initial ? clone(initial) : emptyProfile()));
  const [versions, setVersions] = useState<string[]>([]);
  const upd = (fn: (x: any) => void) => setD((prev: any) => { const n = clone(prev); fn(n); return n; });

  useEffect(() => {
    if (device?.online) API.ListFridaReleases(device.id, '').then(rs => setVersions((rs || []).map(r => r.version))).catch(() => {});
  }, [device?.id]);

  const save = () => {
    if (!d.name.trim()) { showToast({title: 'Name required', kind: 'err'}); return; }
    API.SaveProfile(d as adb.Profile).then(() => { showToast({title: 'Profile saved', body: d.name, kind: 'ok'}); onSaved(); onClose(); })
      .catch(e => showToast({title: 'Save failed', body: String(e), kind: 'err'}));
  };

  const captureHere = () => {
    if (!device?.online) return;
    API.CaptureProfileFromDevice(device.id, d.name || 'Captured').then(p => { setD(clone(p)); showToast({title: 'Captured current settings', kind: 'ok'}); })
      .catch(e => showToast({title: 'Capture failed', body: String(e), kind: 'err'}));
  };

  return (
    <Modal open onClose={onClose} width={640} title={initial ? 'Edit profile' : 'New profile'}
           footer={<>
             {initial && <button className='btn danger' onClick={del}><Icon.Trash width={13} height={13}/>Delete</button>}
             <div style={{flex: 1}}/>
             <button className='btn' onClick={onClose}>Cancel</button>
             <button className='btn primary' onClick={save}>Save</button>
           </>}>
      <div className='field' style={{marginBottom: 10}}>
        <label>Name</label>
        <div style={{display: 'flex', gap: 8}}>
          <input className='input' value={d.name} placeholder='e.g. Burp Pentest' onChange={e => upd(x => x.name = e.target.value)}/>
          {device?.online && <button className='btn sm' title='Capture this device’s current settings' onClick={captureHere}><Icon.Download width={13} height={13}/>Capture</button>}
        </div>
      </div>

      <Section title='Frida — install + start chosen version' on={d.frida.enabled} onToggle={v => upd(x => x.frida.enabled = v)}>
        <div className='field'>
          <label>Version</label>
          <input className='input mono' list='frida-versions' value={d.frida.version} placeholder='latest'
                 onChange={e => upd(x => x.frida.version = e.target.value)}/>
          <datalist id='frida-versions'>{versions.map(v => <option key={v} value={v}/>)}</datalist>
        </div>
        <label style={{display: 'flex', alignItems: 'center', gap: 8, fontSize: 12}}>
          <Switch on={d.frida.autoArch} onChange={v => upd(x => x.frida.autoArch = v)}/> Resolve arch from device automatically
        </label>
        {!d.frida.autoArch && <input className='input mono' value={d.frida.arch} placeholder='arm64 / arm / x86_64 / x86' onChange={e => upd(x => x.frida.arch = e.target.value)}/>}
        <label style={{display: 'flex', alignItems: 'center', gap: 8, fontSize: 12}}>
          <Switch on={d.frida.start} onChange={v => upd(x => x.frida.start = v)}/> Start frida-server after install (needs root)
        </label>
        {d.frida.start && (
          <div style={{display: 'flex', gap: 8}}>
            <input className='input mono' style={{flex: 2}} value={d.frida.iface} placeholder='0.0.0.0' onChange={e => upd(x => x.frida.iface = e.target.value)}/>
            <input className='input mono' style={{flex: 1}} value={d.frida.port || ''} placeholder='27042' onChange={e => upd(x => x.frida.port = parseInt(e.target.value, 10) || 0)}/>
          </div>
        )}
      </Section>

      <Section title='Forwards / reverses' on={d.forwards.enabled} onToggle={v => upd(x => x.forwards.enabled = v)}>
        <SpecList label='Forwards (local → remote)' rows={d.forwards.forwards} a='local' b='remote'
                  onChange={rows => upd(x => x.forwards.forwards = rows)}/>
        <SpecList label='Reverses (remote → local)' rows={d.forwards.reverses} a='remote' b='local'
                  onChange={rows => upd(x => x.forwards.reverses = rows)}/>
      </Section>

      <Section title='Proxy (global HTTP proxy)' on={d.proxy.enabled} onToggle={v => upd(x => x.proxy.enabled = v)}>
        <div className='field'>
          <label>host:port — empty clears, "auto" resolves from device</label>
          <input className='input mono' value={d.proxy.hostPort} placeholder='127.0.0.1:8080 or auto'
                 onChange={e => upd(x => x.proxy.hostPort = e.target.value)}/>
        </div>
      </Section>

      <Section title='Hosts override (/system/etc/hosts)' on={d.hosts.enabled} onToggle={v => upd(x => x.hosts.enabled = v)}>
        <textarea className='input mono' rows={5} value={d.hosts.content} placeholder={'127.0.0.1 localhost\n10.0.0.1 api.example.test'}
                  onChange={e => upd(x => x.hosts.content = e.target.value)}/>
        <label style={{display: 'flex', alignItems: 'center', gap: 8, fontSize: 12}}>
          <Switch on={d.hosts.flushDns} onChange={v => upd(x => x.hosts.flushDns = v)}/> Flush DNS after applying
        </label>
      </Section>

      <Section title='CA certificate' on={d.cert.enabled} onToggle={v => upd(x => x.cert.enabled = v)}>
        <div className='field'>
          <label>Subject (label)</label>
          <input className='input' value={d.cert.subject} placeholder='e.g. Burp Suite CA' onChange={e => upd(x => x.cert.subject = e.target.value)}/>
        </div>
        <textarea className='input mono' rows={4} value={d.cert.pem} placeholder={'-----BEGIN CERTIFICATE-----'}
                  onChange={e => upd(x => x.cert.pem = e.target.value)}/>
        <div className='muted' style={{fontSize: 11}}>System-store install needs root; otherwise it falls back to the user store.</div>
      </Section>

      <Section title='iptables ruleset' on={d.iptables.enabled} onToggle={v => upd(x => x.iptables.enabled = v)}>
        {(!d.iptables.v4Blob && !d.iptables.v6Blob)
          ? <FeatureNotice state={{kind: 'empty', hint: 'No ruleset captured. Use "Capture" above on a rooted device to snapshot iptables-save.'}}/>
          : <div className='muted' style={{fontSize: 12}}>Captured: {d.iptables.v4Blob ? 'IPv4 ' : ''}{d.iptables.v6Blob ? 'IPv6' : ''} ruleset (applied via iptables-restore; needs root).</div>}
      </Section>

      <div className='muted' style={{fontSize: 11, marginTop: 4}}>
        Enabled steps are applied (after you confirm) whenever a device using this profile connects.
      </div>
    </Modal>
  );
}

function SpecList({label, rows, a, b, onChange}: {label: string; rows: any[]; a: string; b: string; onChange: (rows: any[]) => void}) {
  return (
    <div className='field'>
      <label>{label}</label>
      {rows.map((r, i) => (
        <div key={i} style={{display: 'flex', gap: 6, marginBottom: 4}}>
          <input className='input mono' value={r[a]} placeholder='tcp:8080' onChange={e => { const n = [...rows]; n[i] = {...n[i], [a]: e.target.value}; onChange(n); }}/>
          <input className='input mono' value={r[b]} placeholder='tcp:8080' onChange={e => { const n = [...rows]; n[i] = {...n[i], [b]: e.target.value}; onChange(n); }}/>
          <IconBtn title='Remove' onClick={() => onChange(rows.filter((_, j) => j !== i))}><Icon.Trash width={13} height={13}/></IconBtn>
        </div>
      ))}
      <button className='btn sm' onClick={() => onChange([...rows, {[a]: '', [b]: ''}])}><Icon.Plus width={13} height={13}/>Add</button>
    </div>
  );
}

// ─── Past devices ─────────────────────────────────────────────────────────────

export function PastDevices({onClose, onApply}: {onClose: () => void; onApply: (serial: string, profileId: string) => void}) {
  const [recs, setRecs] = useState<adb.DeviceRecord[]>([]);
  const [profiles, setProfiles] = useState<adb.Profile[]>([]);
  const [connected, setConnected] = useState<Record<string, string>>({}); // key → adb serial (online)

  const refresh = () => {
    API.ListDeviceRecords().then(r => setRecs((r || []).sort((x, y) => y.lastSeen - x.lastSeen))).catch(() => {});
    API.ListProfiles().then(p => setProfiles(p || [])).catch(() => {});
    API.ListDevices().then(ds => {
      const m: Record<string, string> = {};
      (ds || []).forEach(dev => { if (dev.online) m[deviceKey(dev)] = dev.id; });
      setConnected(m);
    }).catch(() => {});
  };
  useEffect(refresh, []);

  const bind = (key: string, profileId: string) => {
    API.BindDeviceProfileByKey(key, profileId).then(refresh).catch(e => showToast({title: 'Bind failed', body: String(e), kind: 'err'}));
  };
  const del = async (key: string) => {
    if (!await confirmDialog({title: 'Forget this device?', confirmLabel: 'Forget', danger: true})) return;
    // No backend delete for records; just unbind so it stops auto-applying.
    API.BindDeviceProfileByKey(key, '').then(refresh).catch(() => {});
  };

  return (
    <Modal open onClose={onClose} width={620} title='Devices & profiles'>
      {recs.length === 0 && <div className='muted' style={{padding: 16}}>No devices seen yet.</div>}
      {recs.map(rec => {
        const onlineSerial = connected[rec.key];
        return (
          <div key={rec.key} style={{display: 'flex', alignItems: 'center', gap: 10, padding: '9px 4px', borderBottom: '1px solid var(--border)'}}>
            <Icon.Phone width={16} height={16}/>
            <div style={{flex: 1, minWidth: 0}}>
              <div style={{display: 'flex', alignItems: 'center', gap: 6}}>
                <strong style={{fontSize: 13}}>{rec.label || rec.model || rec.adbSerial || rec.key}</strong>
                {onlineSerial ? <Badge kind='ok'>online</Badge> : <Badge>offline</Badge>}
              </div>
              <div className='muted mono' style={{fontSize: 11, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'}}>{rec.key}</div>
            </div>
            <Dropdown
              trigger={<button className='btn sm' style={{display: 'flex', alignItems: 'center', gap: 5, maxWidth: 160}}>
                <Icon.Zap width={12} height={12}/>
                <span style={{overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'}}>
                  {profiles.find(p => p.id === rec.boundProfileId)?.name || 'Base / no profile'}
                </span>
              </button>}
              items={[
                {label: 'Base / no profile', onClick: () => bind(rec.key, '')},
                ...(profiles.length ? [{label: '', onClick: () => {}, divider: true}] : []),
                ...profiles.map(p => ({label: (p.id === rec.boundProfileId ? '● ' : '   ') + p.name, onClick: () => bind(rec.key, p.id)})),
              ]}/>
            {onlineSerial && rec.boundProfileId &&
              <button className='btn sm primary' onClick={() => { onApply(onlineSerial, rec.boundProfileId); onClose(); }}>Apply now</button>}
            <IconBtn title='Forget' onClick={() => del(rec.key)}><Icon.Trash width={13} height={13}/></IconBtn>
          </div>
        );
      })}
    </Modal>
  );
}
