import React, {useEffect, useMemo, useState} from 'react';
import {adb, main} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {Icon} from '../icons';
import {Badge, CommandPreview, SearchInput, Switch, confirmDialog, showToast} from '../ui';
import {installTcpdumpAuto} from '../lib/tcpdump';
import {useDeviceData} from '../cache';

type Tab = 'overview' | 'proxy' | 'cert' | 'hosts' | 'dns' | 'capture' | 'connections';

export function NetworkScreen({device}: {device: adb.Device}) {
  const [tab, setTab] = useState<Tab>('overview');

  // Cached-first: show the last network snapshot instantly, revalidate quietly.
  const {data: info, refreshing, error, refresh: reload} = useDeviceData(
    device?.id ? `net-info:${device.id}` : null,
    () => API.GetNetworkInfo(device.id),
    {staleMs: 10000},
  );

  const tabs: {id: Tab; label: string; icon: React.ReactNode}[] = [
    {id: 'overview',    label: 'Overview',       icon: <Icon.Wifi/>},
    {id: 'proxy',       label: 'Proxy',          icon: <Icon.Shield/>},
    {id: 'cert',        label: 'CA certs',       icon: <Icon.Shield/>},
    {id: 'hosts',       label: 'Host overrides', icon: <Icon.Settings/>},
    {id: 'dns',         label: 'DNS',            icon: <Icon.Activity/>},
    {id: 'capture',     label: 'Capture',        icon: <Icon.Download/>},
    {id: 'connections', label: 'Connections',    icon: <Icon.Arrows/>},
  ];

  return (
    <div className='screen'>
      <div className='screen-header'>
        <h1>Network</h1>
        <span className='subtitle mono'>{info?.wifiSsid || '—'} · {info?.ip || device.ip} · {info?.mac || device.mac}</span>
        {!!error && <span style={{color: 'var(--err)', fontSize: 11}}>load failed</span>}
        <div className='spacer' style={{flex: 1}}/>
        <button className='btn' onClick={reload}><Icon.Refresh className={refreshing ? 'spin' : ''}/>Refresh</button>
      </div>

      <div className='tabbar'>
        {tabs.map(t => (
          <button key={t.id} className={`tabbar-tab${tab === t.id ? ' active' : ''}`} onClick={() => setTab(t.id)}>
            {t.icon}{t.label}
          </button>
        ))}
      </div>

      <div className='screen-body' style={{paddingTop: 16}}>
        {tab === 'overview'    && <NetOverview device={device} info={info ?? null}/>}
        {tab === 'proxy'       && <NetProxy device={device} info={info ?? null} reload={reload}/>}
        {tab === 'cert'        && <NetCert device={device}/>}
        {tab === 'hosts'       && <NetHosts device={device}/>}
        {tab === 'dns'         && <NetDns device={device} info={info ?? null}/>}
        {tab === 'capture'     && <NetCapture device={device}/>}
        {tab === 'connections' && <NetConnections device={device}/>}
      </div>
    </div>
  );
}

// ─── Overview ────────────────────────────────────────────────────────────

function NetCard({icon, label, value}: {icon: React.ReactNode; label: string; value: string}) {
  return (
    <div className='netcard'>
      <div className='iconwrap'>{icon}</div>
      <div><div className='label'>{label}</div><div className='value'>{value || '—'}</div></div>
    </div>
  );
}

function NetOverview({device, info}: {device: adb.Device; info: adb.NetworkInfo | null}) {
  return (
    <>
      <div className='grid-4' style={{marginBottom: 14}}>
        <NetCard icon={<Icon.Wifi/>} label='Wi-Fi SSID' value={info?.wifiSsid || device.wifi || '—'}/>
        <NetCard icon={<Icon.Activity/>} label='Device IP' value={info?.ip || device.ip || '—'}/>
        <NetCard icon={<Icon.Phone/>} label='MAC' value={info?.mac || device.mac || '—'}/>
        <NetCard icon={<Icon.Settings/>} label='Gateway' value={info?.gateway || '—'}/>
      </div>
      <div className='card'>
        <div className='card-header'><Icon.Wifi width={13} height={13}/><span className='title'>Interfaces</span></div>
        <table className='table'>
          <thead><tr><th style={{paddingLeft: 14}}>Iface</th><th>IPv4</th><th>MAC</th><th>State</th></tr></thead>
          <tbody>
          {(info?.interfaces || []).map(i => (
            <tr key={i.name}>
              <td style={{paddingLeft: 14}} className='mono'>{i.name}</td>
              <td className='mono'>{i.ipv4 || '—'}</td>
              <td className='mono muted'>{i.mac || '—'}</td>
              <td>{i.up ? <Badge kind='ok'><span className='dot'/>up</Badge> : <Badge>down</Badge>}</td>
            </tr>
          ))}
          </tbody>
        </table>
      </div>
      {info?.dns && info.dns.length > 0 && (
        <div className='card' style={{marginTop: 12}}>
          <div className='card-header'><span className='title'>DNS servers</span></div>
          <div className='card-body mono'>{info.dns.join(' · ')}</div>
        </div>
      )}
    </>
  );
}

// ─── Proxy (sade) ────────────────────────────────────────────────────────

function NetProxy({device, info, reload}: {device: adb.Device; info: adb.NetworkInfo | null; reload: () => void}) {
  const active = !!info?.proxy;
  const [host, setHost] = useState('');
  const [port, setPort] = useState('');
  useEffect(() => {
    const v = info?.proxy || '';
    const i = v.lastIndexOf(':');
    if (i > 0) { setHost(v.slice(0, i)); setPort(v.slice(i + 1)); }
    else { setHost(v); setPort(''); }
  }, [info?.proxy]);
  // The command comes from Go, and follows the fields as they are typed, so
  // what is shown is what Apply would run (CLAUDE.md §4.1).
  const [proxyCmd, setProxyCmd] = useState('');
  useEffect(() => {
    let live = true;
    API.ProxyCommand(device.id, host && port ? `${host}:${port}` : '')
      .then(c => { if (live) setProxyCmd(c); })
      .catch(() => { if (live) setProxyCmd(''); });
    return () => { live = false; };
  }, [device.id, host, port]);
  const PRESETS = [
    {label: 'Burp Suite',  port: 8080},
    {label: 'mitmproxy',   port: 8080},
    {label: 'Charles',     port: 8888},
  ];
  const apply = (h = host, p = port) => {
    const v = h && p ? `${h}:${p}` : '';
    API.SetProxy(device.id, v)
      .then(() => { showToast({title: 'Proxy ' + (v ? 'applied' : 'cleared'), body: v, kind: 'ok', mono: true}); reload(); })
      .catch(e => showToast({title: 'Apply failed', body: String(e), kind: 'err'}));
  };

  // Smart-fill: choose 127.0.0.1 + reverse on USB, or host LAN IP on Wi-Fi.
  function applyPreset(preset: {label: string; port: number}) {
    API.SuggestProxyHost(device.id, preset.port).then(async (s: main.ProxySuggestion) => {
      if (!s.host) {
        showToast({title: 'No proxy host detected', body: 'Set it manually below', kind: 'err'});
        return;
      }
      if (s.needsReverse) {
        await API.AddReverse(device.id, `tcp:${preset.port}`, `tcp:${preset.port}`).catch(() => {});
      }
      setHost(s.host);
      setPort(String(preset.port));
      apply(s.host, String(preset.port));
      showToast({
        title: `${preset.label} preset applied`,
        body: `${s.host}:${preset.port} · ${s.reason}`,
        kind: 'ok', mono: true,
      });
    }).catch(e => showToast({title: 'Suggest failed', body: String(e), kind: 'err'}));
  }
  return (
    <>
      <div className='card' style={{marginBottom: 12}}>
        <div className='card-body' style={{padding: 18}}>
          <div className='spread' style={{marginBottom: 14}}>
            <div>
              <div style={{fontWeight: 600, fontSize: 14, marginBottom: 2}}>System HTTP/HTTPS proxy</div>
              <div className='muted' style={{fontSize: 12}}>{active ? <>Currently routing through <span className='mono'>{info?.proxy}</span></> : 'Disabled'}</div>
            </div>
            <Switch on={active} onChange={(v) => { if (!v) apply('', ''); else if (host && port) apply(); else showToast({title: 'Fill host & port first', kind: 'info'}); }}/>
          </div>
          <div style={{display: 'grid', gridTemplateColumns: '2fr 100px auto', gap: 8}}>
            <input className='input mono' placeholder='proxy host  (e.g. 192.168.1.10)' value={host} onChange={e => setHost(e.target.value)}/>
            <input className='input mono' placeholder='port' value={port} onChange={e => setPort(e.target.value)}/>
            <button className='btn primary' onClick={() => apply()}>Apply</button>
          </div>
          <div style={{marginTop: 10, display: 'flex', gap: 4, alignItems: 'center', flexWrap: 'wrap'}}>
            <span className='muted' style={{fontSize: 11}}>Quick presets:</span>
            {PRESETS.map(p => (
              <button key={p.label} className='btn sm' onClick={() => applyPreset(p)}
                      title={`Auto-detect proxy IP for ${p.label} (USB→127.0.0.1 + reverse, Wi-Fi→LAN IP)`}>
                {p.label}
              </button>
            ))}
          </div>
          <CommandPreview commands={proxyCmd ? [proxyCmd] : []}/>
        </div>
      </div>
      <div className='muted' style={{fontSize: 11, marginTop: 6}}>
        Affects system HTTP/HTTPS only. Apps with their own networking stack (TLS pinning, custom DNS) ignore this setting. For HTTPS interception you'll also need to install the proxy's CA.
      </div>
    </>
  );
}

// ─── CA certificates (install + view trust store) ───────────────────────

function NetCert({device}: {device: adb.Device}) {
  const [certs, setCerts] = useState<adb.CACert[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [q, setQ] = useState('');
  const [installing, setInstalling] = useState(false);
  const [last, setLast] = useState<adb.CertInstallResult | null>(null);
  // What the install will attempt on this device. Root decides whether that is
  // the system store or a staged manual import, so it comes from the backend.
  const [plan, setPlan] = useState<adb.CertInstallPlan | null>(null);
  useEffect(() => {
    if (!device?.id) { setPlan(null); return; }
    let live = true;
    API.PlanCertInstall(device.id)
      .then(p => { if (live) setPlan(p); })
      .catch(() => { if (live) setPlan(null); });
    return () => { live = false; };
  }, [device?.id]);

  const load = () => {
    if (!device?.id) return;
    setLoading(true);
    API.ListCACerts(device.id)
      .then(c => setCerts(c || []))
      .catch(e => showToast({title: 'List CA certs failed', body: String(e), kind: 'err'}))
      .finally(() => setLoading(false));
  };
  useEffect(() => { load(); }, [device?.id]);

  async function install() {
    // Landing a CA in the system store remounts or overlays a read-only
    // partition, so the escalation is read before it runs (CLAUDE.md §4.1 K2).
    const ok = await confirmDialog({
      title: plan?.store === 'user' ? 'Stage a CA certificate?' : 'Install a CA into the system store?',
      body: <>
        {plan?.note}
        <CommandPreview commands={plan?.commands ?? []} defaultOpen/>
      </>,
      confirmLabel: 'Pick a certificate',
      danger: plan?.store !== 'user',
    });
    if (!ok) return;
    setInstalling(true);
    API.InstallSystemCertWithPicker(device.id)
      .then(res => {
        if (!res) return; // cancelled
        setLast(res);
        showToast({
          title: res.persistent ? 'CA installed (system store)' : 'CA installed (active until reboot)',
          body: `${res.subject} · ${res.strategy}`,
          kind: 'ok', mono: true,
        });
        setQ(res.subject.split(/[\s,]/)[0] || ''); // surface it in the list
        load();
      })
      .catch(e => showToast({title: 'Install failed', body: String(e), kind: 'err'}))
      .finally(() => setInstalling(false));
  }

  const filtered = useMemo(() => {
    const list = certs || [];
    if (!q.trim()) return list;
    const Q = q.toLowerCase();
    return list.filter(c => c.subject.toLowerCase().includes(Q) || c.issuer.toLowerCase().includes(Q) || c.fileName.includes(Q));
  }, [certs, q]);

  return (
    <>
      <div className='card' style={{marginBottom: 12}}>
        <div className='card-body' style={{padding: 18}}>
          <div className='spread' style={{marginBottom: 12, alignItems: 'flex-start'}}>
            <div>
              <div style={{fontWeight: 600, fontSize: 14, marginBottom: 2}}>Install a CA certificate</div>
              <div className='muted' style={{fontSize: 12, maxWidth: 560}}>
                Adds an interception CA (Burp, ZAP, mitmproxy, Charles) to the device <strong>system</strong> trust store so apps trust it.
                Pick a <span className='mono'>.der</span> / <span className='mono'>.pem</span> / <span className='mono'>.crt</span> file — adbq computes the
                Android subject hash, converts, and installs it. {device.root
                  ? 'This device is rooted: it installs into the system store (persistent if /system is writable, otherwise a live tmpfs overlay).'
                  : 'This device is not rooted: the cert is staged to /sdcard and you finish via Settings (user store).'}
              </div>
            </div>
            <button className='btn primary' onClick={install} disabled={installing}>
              {installing ? '…installing' : <><Icon.Download/>Install CA</>}
            </button>
          </div>
          {last && (
            <div className='card' style={{padding: 12, borderColor: last.persistent ? 'var(--ok)' : 'var(--warn)'}}>
              <div className='spread'>
                <span style={{fontSize: 13, fontWeight: 600}}>{last.subject}</span>
                <div style={{display: 'flex', gap: 6}}>
                  <Badge>{last.strategy}</Badge>
                  {last.persistent ? <Badge kind='ok'>persistent</Badge> : <Badge kind='warn'>until reboot</Badge>}
                </div>
              </div>
              <div className='muted' style={{fontSize: 11.5, marginTop: 6}}>{last.note}</div>
              <div className='mono subtle' style={{fontSize: 11, marginTop: 4}}>{last.path}</div>
            </div>
          )}
          <CommandPreview commands={plan?.commands ?? []}/>
          <div className='muted' style={{fontSize: 11, marginTop: 10}}>
            Burp: <span className='mono'>Proxy → Proxy settings → Import / export CA certificate → Certificate in DER format</span>, then install the saved file here.
          </div>
        </div>
      </div>

      <div className='card' style={{display: 'flex', flexDirection: 'column', minHeight: 0}}>
        <div className='card-header'>
          <span className='title'>Trust store</span>
          {certs && <Badge>{filtered.length}{filtered.length !== certs.length ? `/${certs.length}` : ''} certs</Badge>}
          <div style={{flex: 1}}/>
          <SearchInput value={q} onChange={setQ} placeholder='Filter subject / issuer / hash'/>
          <button className='btn sm' onClick={load} disabled={loading}><Icon.Refresh/></button>
        </div>
        <div style={{maxHeight: '52vh', overflow: 'auto'}}>
          <table className='table'>
            <thead><tr>
              <th style={{paddingLeft: 14}}>Subject</th><th>Issuer</th><th>Expires</th><th>Store</th><th className='mono'>Hash</th>
            </tr></thead>
            <tbody>
            {filtered.map((c, i) => (
              <tr key={c.store + c.fileName + i}>
                <td style={{paddingLeft: 14, fontWeight: 500}}>{c.subject}</td>
                <td className='muted'>{c.issuer === c.subject ? <span className='subtle'>self-signed</span> : c.issuer}</td>
                <td>{c.expired ? <Badge kind='err'>expired {c.notAfter}</Badge> : <span className='mono subtle'>{c.notAfter}</span>}</td>
                <td>{c.store === 'apex' ? <Badge kind='accent'>apex</Badge> : <Badge>system</Badge>}</td>
                <td className='mono subtle' style={{fontSize: 10.5}}>{c.fileName}</td>
              </tr>
            ))}
            </tbody>
          </table>
          {loading && <div className='muted' style={{padding: 16, fontSize: 12}}>Loading trust store…</div>}
          {!loading && certs && filtered.length === 0 && (
            <div className='muted' style={{padding: 16, fontSize: 12}}>
              {certs.length === 0 ? 'No CA certificates found in the device trust store.' : `No certs match "${q}".`}
            </div>
          )}
        </div>
      </div>
    </>
  );
}

// ─── Hosts overrides w/ auto-restore ────────────────────────────────────

function NetHosts({device}: {device: adb.Device}) {
  const [text, setText] = useState('');
  const [saved, setSaved] = useState('');
  const [loading, setLoading] = useState(false);
  const [drifted, setDrifted] = useState(false);
  const [plan, setPlan] = useState<adb.HostsApplyPlan | null>(null);
  const [flushCmd, setFlushCmd] = useState<string[]>([]);

  const load = () => {
    if (!device?.id) return;
    setLoading(true);
    Promise.all([
      device.root
        ? API.RunCommandRoot(device.id, 'cat /system/etc/hosts')
        : API.RunCommand(device.id, 'cat /system/etc/hosts'),
      API.LoadHostsConfig(device.id),
    ])
      .then(([live, persisted]) => {
        setText(live);
        setSaved(persisted || '');
        setDrifted(!!persisted && persisted.trim() !== live.trim());
        refreshPlan(live);
        API.NetCommands(device.id, '').then(c => setFlushCmd(c.flushDns ?? [])).catch(() => setFlushCmd([]));
      })
      .catch(e => showToast({title: 'Read hosts failed', body: String(e), kind: 'err'}))
      .finally(() => setLoading(false));
  };
  useEffect(() => { load(); }, [device?.id]);

  // On (re)connect, check drift and notify so the user can re-apply with one click.
  useEffect(() => {
    if (!device?.id) return;
    API.HostsDrifted(device.id).then(d => {
      if (d) {
        showToast({
          title: 'Hosts file reverted',
          body: 'Saved override on host doesn\'t match device. Open Network → Hosts to re-apply.',
          kind: 'info',
          ttl: 6000,
        });
      }
    }).catch(() => {});
  }, [device?.id]);

  // The plan is for the text in the editor, so it stages the bytes it shows.
  // Refreshed on demand rather than per keystroke: resolving the real hosts
  // path is a device call.
  function refreshPlan(content: string) {
    API.PlanHostsApply(device.id, content).then(setPlan).catch(() => setPlan(null));
  }

  async function apply() {
    if (!device.root) {
      showToast({title: 'Root required', body: 'Editing /system/etc/hosts needs root + writable /system.', kind: 'err'});
      return;
    }
    const p = await API.PlanHostsApply(device.id, text).catch(() => null);
    setPlan(p);
    const ok = await confirmDialog({
      title: `Write ${p?.path || '/system/etc/hosts'}?`,
      body: <>
        Tried in order until one write reads back intact; the last resort scaffolds a Magisk module and needs a reboot.
        <CommandPreview commands={p?.commands ?? []} defaultOpen/>
      </>,
      confirmLabel: 'Save & Apply', danger: true,
    });
    if (!ok) return;
    // Persist locally first so a future reboot can restore it.
    API.SaveHostsConfig(device.id, text)
      .then(() => API.ApplyHostsConfig(device.id))
      .then(res => {
        if (res?.content) setText(res.content);
        setSaved(text);
        setDrifted(false);
        if (res?.needsReboot) {
          showToast({
            title: 'Magisk module installed — reboot required',
            body: `Direct write was blocked (dm-verity/overlay). Scaffolded /data/adb/modules/adbq-hosts; reboot to take effect.`,
            kind: 'info', ttl: 8000,
          });
        } else {
          const body = `${res?.path || '/system/etc/hosts'} · ${res?.strategy || '?'}${res?.netdFlushed ? ' · DNS cache flushed' : ''}`;
          showToast({title: 'Hosts applied', body, kind: 'ok', mono: true});
        }
      })
      .catch(e => showToast({title: 'Write failed', body: String(e), kind: 'err'}));
  }

  function flushDns() {
    API.FlushDeviceDNS(device.id)
      .then(() => showToast({title: 'DNS cache flushed', body: 'netd resolver cleared on all interfaces', kind: 'ok'}))
      .catch(e => showToast({title: 'Flush failed', body: String(e), kind: 'err'}));
  }

  return (
    <>
      <div className='card' style={{marginBottom: 12, padding: 14, borderColor: drifted ? 'var(--warn)' : undefined}}>
        {drifted ? (
          <>
            <strong>⚠️ Device reverted to original hosts</strong>
            <div className='muted' style={{fontSize: 12, marginTop: 4}}>
              The hosts file on the device no longer matches what you previously saved. This usually happens after a reboot on a stock system. Click <strong>Re-apply saved</strong> to push your last-known-good content back.
            </div>
            <button className='btn primary' style={{marginTop: 8}} onClick={() => {
              API.ApplyHostsConfig(device.id).then(res => {
                if (res?.content) setText(res.content);
                setDrifted(false);
                showToast({title: 'Restored', body: res?.strategy ? `via ${res.strategy}` : '', kind: 'ok'});
              }).catch(e => showToast({title: 'Restore failed', body: String(e), kind: 'err'}));
            }}><Icon.Refresh/>Re-apply saved</button>
          </>
        ) : saved ? (
          <span className='muted' style={{fontSize: 12}}>Last saved override: {saved.split('\n').length} lines. Auto-checks on reconnect.</span>
        ) : (
          <span className='muted' style={{fontSize: 12}}>No saved override for this device yet. Edit below and Apply to persist; we'll re-apply automatically if the device reverts after reboot.</span>
        )}
      </div>

      <div className='card'>
        <div className='card-header'>
          <span className='title mono' style={{fontSize: 11}}>/system/etc/hosts</span>
          <div style={{flex: 1}}/>
          <button className='btn sm' onClick={load} disabled={loading}><Icon.Refresh/>Reload</button>
        </div>
        <textarea
          className='input mono'
          style={{margin: 14, width: 'calc(100% - 28px)', minHeight: 260, resize: 'vertical', fontSize: 11.5}}
          value={text}
          onChange={e => setText(e.target.value)}
        />
        <div className='card-body' style={{display: 'flex', gap: 6, borderTop: '1px solid var(--border)', flexWrap: 'wrap'}}>
          <button className='btn primary' disabled={!device.root} onClick={apply}>
            {device.root ? 'Save & Apply' : 'Root required'}
          </button>
          <button className='btn' disabled={!device.root} onClick={flushDns} title='Clear netd DNS cache so running apps pick up the new hosts entries'>
            <Icon.Refresh/>Flush DNS cache
          </button>
          <span className='muted' style={{fontSize: 11, alignSelf: 'center'}}>
            Tries direct write → magisk remount → /system remount → bind-mount → Magisk module scaffold (auto). Verifies via md5 and flushes netd cache.
          </span>
          <div style={{flexBasis: '100%', display: 'grid', gap: 4}}>
            <CommandPreview commands={plan?.commands ?? []} label='Save & Apply'/>
            <CommandPreview commands={flushCmd} label='Flush DNS cache'/>
          </div>
        </div>
      </div>

      <div className='card' style={{marginTop: 12, padding: 14}}>
        <strong style={{color: 'var(--warn)'}}>Caveats</strong>
        <ul className='muted' style={{fontSize: 12, margin: '6px 0 0 18px'}}>
          <li>Apps using DNS-over-HTTPS or their own resolver bypass /etc/hosts.</li>
          <li>dm-verity on stock devices may block /system remount; we'll auto-fall-back to a Magisk module if available (reboot needed once).</li>
          <li>Some Android 11+ devices use systemless overlays — direct /system writes silently revert; the Magisk module path survives these.</li>
          <li>After applying, running apps may still hit stale IPs from their own caches (Chrome, OkHttp). Use <b>Flush DNS cache</b> to clear netd, then force-stop affected apps if needed.</li>
        </ul>
      </div>
    </>
  );
}

// ─── DNS lookup ──────────────────────────────────────────────────────────

interface DnsRecord { time: string; host: string; result: string; ok: boolean; }

function NetDns({device, info}: {device: adb.Device; info: adb.NetworkInfo | null}) {
  const [host, setHost] = useState('');
  const [busy, setBusy] = useState(false);
  const [history, setHistory] = useState<DnsRecord[]>([]);
  const [cmds, setCmds] = useState<string[]>([]);
  useEffect(() => {
    if (!device?.id || !host.trim()) { setCmds([]); return; }
    let live = true;
    API.DNSLookupCommands(device.id, host.trim())
      .then(c => { if (live) setCmds(c || []); })
      .catch(() => { if (live) setCmds([]); });
    return () => { live = false; };
  }, [device?.id, host]);

  function lookup() {
    if (!host.trim()) return;
    const h = host.trim();
    setBusy(true);
    API.DNSLookup(device.id, h)
      .then(out => {
        const ok = !out.toLowerCase().includes('not found') && !out.toLowerCase().includes('unknown host') && !out.toLowerCase().includes('bad address');
        setHistory(prev => [{time: new Date().toLocaleTimeString(), host: h, result: out.trim(), ok}, ...prev].slice(0, 25));
      })
      .catch(e => setHistory(prev => [{time: new Date().toLocaleTimeString(), host: h, result: String(e), ok: false}, ...prev]))
      .finally(() => setBusy(false));
  }

  return (
    <>
      <div className='card' style={{marginBottom: 12}}>
        <div className='card-header'><span className='title'>DNS servers</span></div>
        <div className='card-body'>
          {info?.dns && info.dns.length > 0
            ? <div style={{display: 'flex', gap: 8, flexWrap: 'wrap'}}>{info.dns.map((d, i) => <Badge key={i}>{d}</Badge>)}</div>
            : <div className='muted' style={{fontSize: 12}}>No DNS servers reported by getprop. Android 9+ uses netd for resolution; servers are not exposed via legacy properties.</div>}
          <div className='muted' style={{fontSize: 11, marginTop: 8}}>
            Lookups run on the device and combine three views: a direct grep of <span className='mono'>/system/etc/hosts</span> + <span className='mono'>/etc/hosts</span> (proves whether an override is in place), <span className='mono'>ping</span> (uses bionic's resolver, which honors hosts), and <span className='mono'>nslookup</span> (pure DNS — useful for comparing). After editing hosts, click <b>Flush DNS cache</b> in the Hosts tab to clear netd.
          </div>
        </div>
      </div>

      <div className='card' style={{marginBottom: 12}}>
        <div className='card-header'><span className='title'>Lookup</span></div>
        <div className='card-body' style={{display: 'flex', gap: 8}}>
          <input
            className='input mono'
            style={{flex: 1}}
            placeholder='hostname (e.g. api.example.com)'
            value={host}
            onChange={e => setHost(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && lookup()}/>
          <button className='btn primary' onClick={lookup} disabled={busy || !host.trim()}>
            {busy ? '…' : 'Resolve'}
          </button>
        </div>
        <div style={{padding: '0 14px 12px'}}>
          <CommandPreview commands={cmds} label='Lookup'/>
        </div>
      </div>

      {history.length > 0 && (
        <div className='card'>
          <div className='card-header'>
            <span className='title'>Recent ({history.length})</span>
            <div style={{flex: 1}}/>
            <button className='btn sm' onClick={() => setHistory([])}>Clear</button>
          </div>
          <div style={{maxHeight: 380, overflow: 'auto'}}>
            {history.map((r, i) => (
              <div key={i} style={{padding: '10px 14px', borderBottom: '1px solid var(--border)', borderLeft: `3px solid ${r.ok ? 'var(--ok)' : 'var(--err)'}`}}>
                <div className='spread'>
                  <span className='mono' style={{fontWeight: 600, fontSize: 12}}>{r.host}</span>
                  <span className='subtle mono' style={{fontSize: 10.5}}>{r.time}</span>
                </div>
                <div className='mono subtle' style={{fontSize: 11, marginTop: 4, whiteSpace: 'pre-wrap', wordBreak: 'break-all'}}>{r.result}</div>
              </div>
            ))}
          </div>
        </div>
      )}
    </>
  );
}

// ─── Capture ─────────────────────────────────────────────────────────────

function NetCapture({device}: {device: adb.Device}) {
  const [iface, setIface] = useState('any');
  const [filter, setFilter] = useState('tcp port 443');
  const [state, setState] = useState<adb.CaptureState | null>(null);
  const [busy, setBusy] = useState(false);
  const [td, setTd] = useState<adb.TcpdumpInfo | null>(null);

  const probeTd = () => {
    if (!device?.id) return;
    API.ProbeTcpdump(device.id).then(setTd).catch(() => setTd(null));
  };
  const poll = () => {
    if (!device?.id) return;
    API.CaptureStatus(device.id).then(setState).catch(() => {});
  };
  useEffect(() => {
    poll();
    probeTd();
    const t = setInterval(poll, 1500);
    return () => clearInterval(t);
  }, [device?.id]);

  function installTd() {
    API.InstallTcpdumpWithPicker(device.id)
      .then(info => {
        if (info) {
          setTd(info);
          showToast({title: 'tcpdump installed', body: `${info.path} (${info.source || 'tmp'})`, kind: 'ok', mono: true});
        }
      })
      .catch(e => showToast({title: 'Install failed', body: String(e), kind: 'err'}));
  }
  async function installTdAuto() {
    const ok = await installTcpdumpAuto(device.id);
    if (ok) await probeTd();
  }

  const active = !!state?.active;
  const elapsed = active && state?.startedAt ? Math.max(0, Math.floor(Date.now() / 1000 - state.startedAt)) : 0;

  function start() {
    setBusy(true);
    API.StartCapture(device.id, iface, filter)
      .then(s => { setState(s); showToast({title: 'Capture started', body: `${iface} · ${filter || 'no filter'}`, kind: 'ok', mono: true}); })
      .catch(e => showToast({title: 'Start failed', body: String(e), kind: 'err'}))
      .finally(() => setBusy(false));
  }
  function stop() {
    setBusy(true);
    API.StopCapture(device.id)
      .then(s => { setState(s); showToast({title: 'Capture stopped', body: `${fmtBytes(s?.sizeBytes || 0)} captured`, kind: 'ok'}); })
      .catch(e => showToast({title: 'Stop failed', body: String(e), kind: 'err'}))
      .finally(() => setBusy(false));
  }

  const cmd = `nohup tcpdump -i ${iface} -U -w /sdcard/adbq-capture.pcap${filter ? ' ' + JSON.stringify(filter) : ''} >/dev/null 2>&1 &`;

  return (
    <>
      <div className='card' style={{marginBottom: 12, padding: 14, borderColor: td?.available ? 'var(--ok)' : 'var(--warn)'}}>
        {td?.available ? (
          <div className='spread' style={{alignItems: 'center'}}>
            <div>
              <strong>tcpdump found</strong>{' '}
              <span className='muted mono' style={{fontSize: 12}}>{td.path} ({td.source})</span>
              {td.version && <div className='muted' style={{fontSize: 11, marginTop: 2}}>{td.version}</div>}
            </div>
            <button className='btn sm' onClick={installTd} title='Replace with another binary'>
              <Icon.Download/>Reinstall
            </button>
          </div>
        ) : (
          <div className='spread' style={{alignItems: 'center'}}>
            <div>
              <strong>Requires <span className='mono'>tcpdump</span> on the device + root.</strong>{' '}
              <span className='muted'>Not found in /system/bin, /vendor/bin, /data/local/tmp, or Magisk paths.</span>
              <div className='muted' style={{fontSize: 11, marginTop: 4}}>
                <b>Install automatically</b> downloads a pinned static build matching this device's ABI, verifies its SHA256, and pushes it to <span className='mono'>/data/local/tmp/tcpdump</span>. Or pick your own binary with <b>Install from file</b>.
              </div>
            </div>
            <div style={{display: 'flex', gap: 6, flexShrink: 0}}>
              <button className='btn primary' onClick={installTdAuto}>
                <Icon.Download/>Install automatically
              </button>
              <button className='btn' onClick={installTd}>
                <Icon.Upload/>From file
              </button>
            </div>
          </div>
        )}
      </div>

      <div className='card' style={{marginBottom: 12, borderColor: active ? (state?.ourSession ? 'var(--accent)' : 'var(--warn)') : undefined}}>
        <div className='card-header'>
          <span className='title'>Status</span>
          {active && <span className='pulse' style={{marginLeft: 8}}/>}
          {active
            ? state?.ourSession
              ? <Badge kind='accent'>recording</Badge>
              : <Badge kind='warn'>external tcpdump</Badge>
            : <Badge>idle</Badge>}
          <div style={{flex: 1}}/>
          {state?.sizeBytes !== undefined && state.sizeBytes > 0 && <Badge>{fmtBytes(state.sizeBytes)}</Badge>}
        </div>
        <div className='card-body'>
          {state?.warning && (
            <div style={{padding: '8px 10px', marginBottom: 10, background: 'rgba(233,180,84,0.12)', border: '1px solid rgba(233,180,84,0.4)', borderRadius: 6, fontSize: 12}}>
              {state.warning} You can adopt it (treat as ours) or kill it.
              <div style={{marginTop: 6, display: 'flex', gap: 6}}>
                <button className='btn sm' onClick={() => API.AdoptExternalCapture(device.id).then(poll).then(() => showToast({title: 'Adopted', kind: 'ok'}))}>
                  Adopt
                </button>
                <button className='btn sm danger' onClick={() => API.KillExternalCapture(device.id).then(s => { setState(s); showToast({title: 'Killed external tcpdump', kind: 'ok'}); })}>
                  Kill it
                </button>
              </div>
            </div>
          )}
          <div style={{display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '6px 14px', fontSize: 12, alignItems: 'center'}}>
            <span className='muted'>State</span>
            <span>{active ? `Recording for ${fmtDuration(elapsed)}` : (state?.sizeBytes ? 'Stopped' : 'Not running')}</span>
            {state?.pid !== undefined && state.pid > 0 && <><span className='muted'>PID</span><span className='mono'>{state.pid}</span></>}
            <span className='muted'>Remote file</span>
            <span className='mono'>{state?.remoteFile || '/sdcard/adbq-capture.pcap'}</span>
            <span className='muted'>Size</span>
            <span className='mono'>{fmtBytes(state?.sizeBytes || 0)}{state?.packetHint ? ` · ${state.packetHint}` : ''}</span>
          </div>
        </div>
      </div>

      <div className='card' style={{marginBottom: 12}}>
        <div className='card-header'><span className='title'>Configure</span></div>
        <div className='card-body' style={{display: 'grid', gridTemplateColumns: 'auto 1fr', gap: 8, alignItems: 'center'}}>
          <span className='muted' style={{fontSize: 11}}>Interface</span>
          <input className='input mono' value={iface} onChange={e => setIface(e.target.value)} placeholder='any · wlan0 · rmnet_data0' disabled={active}/>
          <span className='muted' style={{fontSize: 11}}>BPF filter</span>
          <input className='input mono' value={filter} onChange={e => setFilter(e.target.value)} placeholder='e.g. host api.example.com — empty = all' disabled={active}/>
        </div>
        <div className='card-body' style={{display: 'flex', gap: 6, borderTop: '1px solid var(--border)'}}>
          <button className='btn primary' disabled={!device.root || active || busy} onClick={start}>
            <Icon.Play/>Start
          </button>
          <button className='btn danger' disabled={!active || busy} onClick={stop}>
            <Icon.Stop/>Stop
          </button>
          <button className='btn' disabled={!state?.sizeBytes || active}
            onClick={() => API.PullCapture(device.id).then(p => p && showToast({title: 'pcap saved', body: p, kind: 'ok', mono: true,
              actions: [{label: 'Reveal', onClick: () => API.RevealPath(p)}]}))}>
            <Icon.Download/>Pull pcap
          </button>
          <div style={{flex: 1}}/>
          <button className='btn sm' onClick={poll}><Icon.Refresh/>Refresh</button>
        </div>
        <div style={{padding: '0 14px 14px'}}>
          {/* A running capture keeps its command in view (CLAUDE.md §4.1). */}
          <CommandPreview commands={cmd ? [cmd] : []} defaultOpen/>
        </div>
      </div>
    </>
  );
}

function fmtBytes(n: number) {
  if (!n) return '0 B';
  if (n < 1024) return n + ' B';
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
  if (n < 1024 * 1024 * 1024) return (n / 1024 / 1024).toFixed(2) + ' MB';
  return (n / 1024 / 1024 / 1024).toFixed(2) + ' GB';
}

function fmtDuration(s: number) {
  if (s < 60) return s + 's';
  const m = Math.floor(s / 60);
  const sec = s % 60;
  if (m < 60) return `${m}m ${sec}s`;
  const h = Math.floor(m / 60);
  return `${h}h ${m % 60}m`;
}

// ─── Connections (live /proc/net) ───────────────────────────────────────

function NetConnections({device}: {device: adb.Device}) {
  const [rows, setRows] = useState<adb.Connection[]>([]);
  const [uidMap, setUidMap] = useState<Record<number, string>>({});
  const [q, setQ] = useState('');
  const [proto, setProto] = useState<'all' | 'tcp' | 'udp'>('all');
  const [paused, setPaused] = useState(false);
  const [busy, setBusy] = useState(false);
  const [lastTs, setLastTs] = useState(0);
  useEffect(() => {
    if (!device?.id) return;
    API.ListPackageUIDs(device.id).then(m => setUidMap(m || {})).catch(() => {});
  }, [device?.id]);
  const reload = () => {
    if (!device?.id || paused) return;
    setBusy(true);
    API.ListConnections(device.id)
      .then(c => { setRows(c || []); setLastTs(Date.now()); })
      .finally(() => setBusy(false));
  };
  useEffect(() => {
    reload();
    if (paused) return;
    const t = setInterval(reload, 3000);
    return () => clearInterval(t);
  }, [device?.id, paused]);

  // Sockets are read out of procfs; `ss` is missing on stripped ROMs, and the
  // preview should say what actually runs.
  const [cmds, setCmds] = useState<string[]>([]);
  useEffect(() => {
    if (!device?.id) return;
    let live = true;
    API.NetCommands(device.id, '')
      .then(c => { if (live) setCmds(c.connections ?? []); })
      .catch(() => { if (live) setCmds([]); });
    return () => { live = false; };
  }, [device?.id]);

  const filtered = useMemo(() => {
    return rows.filter(r => {
      if (proto !== 'all' && !r.proto.startsWith(proto)) return false;
      if (!q) return true;
      const Q = q.toLowerCase();
      const owner = (uidMap[r.uid] || '').toLowerCase();
      return r.local.toLowerCase().includes(Q) || r.remote.toLowerCase().includes(Q) || r.state.toLowerCase().includes(Q) || owner.includes(Q);
    });
  }, [rows, q, proto, uidMap]);

  return (
    <div className='card' style={{display: 'flex', flexDirection: 'column', minHeight: 0, height: '100%'}}>
      <div className='card-header'>
        <span className='title'>Sockets ({filtered.length}/{rows.length})</span>
        {!paused && <span className='pulse' title='live'/>}
        <div style={{flex: 1}}/>
        <SearchInput value={q} onChange={setQ} placeholder='host / port / state / pkg'/>
        <div style={{display: 'flex', gap: 4}}>
          {(['all', 'tcp', 'udp'] as const).map(p => (
            <button key={p} className={`btn sm${proto === p ? ' primary' : ''}`} onClick={() => setProto(p)}>{p}</button>
          ))}
        </div>
        <button className={`btn sm${paused ? ' primary' : ''}`} onClick={() => setPaused(p => !p)} title={paused ? 'Resume' : 'Pause'}>
          {paused ? <Icon.Play/> : <Icon.Pause/>}
        </button>
        <button className='btn sm' onClick={reload} disabled={busy}><Icon.Refresh/></button>
      </div>
      <div style={{flex: 1, minHeight: 0, overflow: 'auto'}}>
        <table className='table'>
          <thead><tr><th>Proto</th><th>Local</th><th>Remote</th><th>State</th><th>Owner</th></tr></thead>
          <tbody>
          {filtered.map((r, i) => (
            <tr key={i}>
              <td className='mono'>{r.proto}</td>
              <td className='mono'>{r.local}</td>
              <td className='mono'>{r.remote}</td>
              <td>{r.state === 'ESTABLISHED' ? <Badge kind='ok'>ESTABLISHED</Badge> :
                  r.state === 'LISTEN' ? <Badge kind='info'>LISTEN</Badge> :
                  r.state === 'TIME_WAIT' ? <Badge>TIME_WAIT</Badge> :
                  <Badge>{r.state}</Badge>}</td>
              <td className='mono muted'>{uidMap[r.uid] || (r.uid > 0 ? `uid=${r.uid}` : '—')}</td>
            </tr>
          ))}
          </tbody>
        </table>
      </div>
      <div style={{padding: '6px 14px', borderTop: '1px solid var(--border)', fontSize: 11}} className='muted spread'>
        <span>{paused ? 'Paused' : 'Live · refreshes every 3s'}</span>
        <span className='subtle mono'>{lastTs ? `last update ${new Date(lastTs).toLocaleTimeString()}` : 'never refreshed'}</span>
      </div>
      <div style={{padding: '0 14px 8px'}}>
        <CommandPreview commands={cmds} label='Read sockets'/>
      </div>
    </div>
  );
}
