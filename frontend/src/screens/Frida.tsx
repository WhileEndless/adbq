import React, {useEffect, useState} from 'react';
import {adb} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {EventsOn, EventsOff} from '../../wailsjs/runtime/runtime';
import {Icon} from '../icons';
import {Badge, Modal, SearchInput, confirmDialog, showToast} from '../ui';
import {useStore} from '../store';

export function FridaScreen({device}: {device: adb.Device}) {
  const store = useStore();
  const [servers, setServers] = useState<adb.FridaServer[]>([]);
  const [port, setPort] = useState(27042);
  const [iface, setIface] = useState('0.0.0.0');
  const [starting, setStarting] = useState<string | null>(null);
  const [stopping, setStopping] = useState(false);
  const [tab, setTab] = useState<'server' | 'runtime'>('server');

  // One-click install modal state.
  const [installOpen, setInstallOpen] = useState(false);
  const [releases, setReleases] = useState<adb.FridaRelease[] | null>(null);
  const [relLoading, setRelLoading] = useState(false);
  const [relError, setRelError] = useState('');
  const [installing, setInstalling] = useState<string | null>(null);
  const [verFilter, setVerFilter] = useState('');
  const [archInfo, setArchInfo] = useState<adb.FridaArchInfo | null>(null);
  const [arch, setArch] = useState('');

  const loadReleases = (a: string, force?: boolean) => {
    if (!device?.id) return;
    setRelLoading(true);
    setRelError('');
    const key = `frida-releases:${device.id}:${a}`;
    if (force) store.invalidate(key);
    store.cached(key, 15 * 60_000, () => API.ListFridaReleases(device.id, a))
      .then(r => { setReleases(r || []); setRelLoading(false); })
      .catch(e => { setRelError(String(e)); setReleases([]); setRelLoading(false); });
  };
  function openInstall() {
    setInstallOpen(true);
    API.FridaArchInfo(device.id).then(info => {
      setArchInfo(info);
      const a = arch || info.primary || (info.supported && info.supported[0]) || '';
      setArch(a);
      loadReleases(a);
    }).catch(() => loadReleases(arch));
  }
  function changeArch(a: string) { setArch(a); loadReleases(a); }

  async function installVersion(r: adb.FridaRelease) {
    const ok = await confirmDialog({
      title: `Install frida-server ${r.version}?`,
      body: `adbq will download the official ${r.arch} build (${fmtMB(r.size)}) from GitHub, verify its SHA256, decompress it locally, and push it to /data/local/tmp.`,
      confirmLabel: 'Download & install',
    });
    if (!ok) return;
    setInstalling(r.version);
    try {
      const remote = await API.InstallFridaServer(device.id, r.version, r.arch);
      showToast({title: 'frida-server installed', body: remote, kind: 'ok', mono: true});
      reload();
      loadReleases(r.arch, true); // refresh installed flags
    } catch (e) {
      showToast({title: 'Install failed', body: String(e), kind: 'err'});
    } finally {
      setInstalling(null);
    }
  }

  const reload = () => {
    if (!device?.id) return;
    API.ListFridaServers(device.id).then(s => {
      const arr = (s || []).slice();
      arr.sort((a, b) => {
        if (a.active !== b.active) return a.active ? -1 : 1;
        return (b.version || '').localeCompare(a.version || '');
      });
      setServers(arr);
      const act = arr.find(x => x.active);
      if (act && act.port) setPort(act.port);
    }).catch(e => showToast({title: 'Listing frida-server failed', body: String(e), kind: 'err'}));
  };
  useEffect(() => { reload(); }, [device?.id]);

  function start(s: adb.FridaServer) {
    if (!device.root) {
      showToast({title: 'Root required', body: 'frida-server needs root to bind a privileged port', kind: 'err'});
      return;
    }
    setStarting(s.name);
    API.StartFrida(device.id, s.path, iface, port)
      .then(() => {
        // Poll until active is detected (up to ~6s) so the UI reflects state truthfully.
        let tries = 0;
        const check = () => {
          tries++;
          API.ListFridaServers(device.id).then(list => {
            const ok = (list || []).some(x => x.active);
            if (ok) {
              showToast({title: 'frida-server started', body: `${s.version || s.name} on ${iface}:${port}`, kind: 'ok', mono: true});
              setStarting(null);
              reload();
            } else if (tries >= 12) {
              showToast({title: 'No active server detected', body: 'Check logcat for crashes; binary may not match the device arch.', kind: 'err'});
              setStarting(null);
              reload();
            } else {
              setTimeout(check, 400);
            }
          }).catch(() => { setStarting(null); });
        };
        setTimeout(check, 500);
      })
      .catch(e => { setStarting(null); showToast({title: 'Start failed', body: String(e), kind: 'err'}); });
  }
  async function stop() {
    const ok = await confirmDialog({title: 'Stop frida-server?', body: 'The running session and all attached scripts will be terminated.', confirmLabel: 'Stop', danger: true});
    if (!ok) return;
    setStopping(true);
    try {
      await API.StopFrida(device.id);
      showToast({title: 'frida-server stopped', kind: 'ok'});
      setTimeout(() => { reload(); setStopping(false); }, 500);
    } catch (e) {
      setStopping(false);
      showToast({title: 'Stop failed', body: String(e), kind: 'err'});
    }
  }
  function pushBinary() {
    API.PushFridaBinaryWithPicker(device.id).then(p => {
      if (p) { showToast({title: 'Pushed', body: p, kind: 'ok', mono: true}); reload(); }
    }).catch(e => showToast({title: 'Push failed', body: String(e), kind: 'err'}));
  }
  function removeBinary(s: adb.FridaServer) {
    confirmDialog({title: 'Delete binary?', body: s.path, confirmLabel: 'Delete', danger: true}).then(ok => {
      if (!ok) return;
      API.DeleteFile(device.id, s.path, false, false).then(reload);
    });
  }

  const active = servers.find(s => s.active);

  return (
    <div className='screen'>
      <div className='screen-header'>
        <h1>Frida {active && tab === 'server' && <Badge kind='accent'>active</Badge>}</h1>
        <div style={{display: 'flex', gap: 4, marginLeft: 8}}>
          <button className={`btn sm${tab === 'server' ? ' primary' : ''}`} onClick={() => setTab('server')}>Server</button>
          <button className={`btn sm${tab === 'runtime' ? ' primary' : ''}`} onClick={() => setTab('runtime')}>Runtime</button>
        </div>
        <div className='spacer' style={{flex: 1}}/>
        {tab === 'server' && <>
          <button className='btn primary' onClick={openInstall}><Icon.Download/>Install server</button>
          <button className='btn' onClick={pushBinary}><Icon.Upload/>Push binary</button>
          <button className='btn' onClick={reload}><Icon.Refresh/></button>
          {active && <button className='btn danger' onClick={stop} disabled={stopping}>{stopping ? '…stopping' : <><Icon.Stop/>Stop</>}</button>}
        </>}
      </div>
      <div className='screen-body'>
        {tab === 'server' ? (<>
        {/* Active session card */}
        <div className={`card${active ? '' : ''}`} style={{marginBottom: 14, borderColor: active ? 'var(--accent)' : undefined}}>
          <div className='card-header'>
            <span className='title'>{active ? 'Active session' : 'No active session'}</span>
            {active && <Badge kind='accent'>PID {active.pid}</Badge>}
            <div style={{flex: 1}}/>
            {active && <span className='pulse' style={{marginRight: 8}}/>}
          </div>
          <div className='card-body'>
            {active ? (
              <>
                <div className='mono' style={{fontSize: 12, marginBottom: 8}}>
                  <span className='muted'>$</span> {active.path} -l {iface}:{active.port}
                </div>
                <div style={{display: 'grid', gridTemplateColumns: 'auto 1fr auto 1fr auto', gap: 8, alignItems: 'center'}}>
                  <span className='muted' style={{fontSize: 11}}>Interface</span>
                  <input className='input mono' value={iface} onChange={e => setIface(e.target.value)}/>
                  <span className='muted' style={{fontSize: 11}}>Port</span>
                  <input className='input mono' type='number' value={port} onChange={e => setPort(parseInt(e.target.value) || 27042)}/>
                  <button className='btn primary' onClick={() => { stop(); setTimeout(() => start(active), 800); }}>
                    <Icon.Refresh/>Restart
                  </button>
                </div>
                <div className='muted' style={{fontSize: 11, marginTop: 8}}>
                  Connect from host: <span className='mono'>frida-ps -H {device.ip || '127.0.0.1'}:{active.port}</span>
                </div>
              </>
            ) : (
              <div className='muted' style={{fontSize: 12}}>
                Pick a binary below and click <strong>Start</strong>. Interface defaults to <span className='mono'>0.0.0.0</span>, port <span className='mono'>{port}</span>.
                {!device.root && <div style={{marginTop: 6, color: 'var(--warn)'}}>
                  This device is unrooted — frida-server cannot bind; use frida-gadget (LD_PRELOAD via repackaging) instead.
                </div>}
              </div>
            )}
          </div>
        </div>

        {/* Binaries list */}
        <div style={{display: 'flex', flexDirection: 'column', gap: 8}}>
          {servers.length === 0 && (
            <div className='card' style={{padding: 16, textAlign: 'center'}}>
              <div className='muted' style={{marginBottom: 6}}>No frida-server binary in /data/local/tmp.</div>
              <button className='btn primary' onClick={pushBinary}><Icon.Upload/>Push one</button>
            </div>
          )}
          {servers.map(s => (
            <div key={s.name} className={`frida-server-row${s.active ? ' active' : ''}`}>
              <div>
                <div className='meta-row'>
                  <strong>{s.version || s.name}</strong>
                  {s.arch && <Badge>{s.arch}</Badge>}
                  {s.active && <Badge kind='accent'>PID {s.pid} · :{s.port}</Badge>}
                </div>
                <div className='filename'>{s.path} · {(s.size / 1024 / 1024).toFixed(1)} MB · {s.perms}</div>
              </div>
              <div className='mono subtle' style={{fontSize: 11}}>{s.arch}</div>
              <div className='mono subtle' style={{fontSize: 11}}>{s.perms}</div>
              <div style={{display: 'flex', gap: 4}}>
                {s.active
                  ? <button className='btn sm danger' onClick={stop} disabled={stopping}>{stopping ? '…stopping' : <><Icon.Stop/>Stop</>}</button>
                  : <button className='btn sm primary' onClick={() => start(s)} disabled={!device.root || !!starting}>
                      {starting === s.name ? '…starting' : <><Icon.Play/>Start</>}
                    </button>}
                <button className='btn sm danger' title='Delete binary' onClick={() => removeBinary(s)}>
                  <Icon.Trash width={11} height={11}/>
                </button>
              </div>
            </div>
          ))}
        </div>

        {/* Script library */}
        <div className='card' style={{marginTop: 14}}>
          <div className='card-header'><span className='title'>Script library</span><span className='subtle' style={{marginLeft: 8, fontSize: 11}}>copy-paste into your frida CLI</span></div>
          <table className='table'>
            <thead><tr><th>Name</th><th>What it does</th><th className='actions'></th></tr></thead>
            <tbody>
            {SCRIPTS.map(s => (
              <tr key={s.name}>
                <td className='mono'>{s.name}</td>
                <td className='muted'>{s.desc}</td>
                <td className='actions'>
                  <button className='btn sm' onClick={() => { navigator.clipboard?.writeText(s.cmd); showToast({title: 'Copied', body: s.cmd, kind: 'ok', mono: true}); }}>
                    Copy cmd
                  </button>
                </td>
              </tr>
            ))}
            </tbody>
          </table>
        </div>
        </>) : (
          <FridaRuntimeTab device={device}/>
        )}
      </div>

      <Modal open={installOpen} onClose={() => setInstallOpen(false)} title='Install frida-server' width={620}
        footer={<button className='btn' onClick={() => setInstallOpen(false)}>Close</button>}>
        <div style={{display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6}}>
          <span className='muted' style={{fontSize: 12}}>
            Official builds from <span className='mono'>github.com/frida/frida</span>
          </span>
          <div style={{flex: 1}}/>
          <SearchInput value={verFilter} onChange={setVerFilter} placeholder='Filter version'/>
          <button className='btn sm' onClick={() => loadReleases(arch, true)} title='Refresh'><Icon.Refresh/></button>
        </div>
        {archInfo && (
          <div style={{display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10, fontSize: 12}}>
            <span className='muted'>Device:</span>
            <span className='mono'>{archInfo.abi || '?'}</span>
            <Badge kind={archInfo.bits64 ? 'accent' : undefined}>{archInfo.bits64 ? '64-bit' : '32-bit'}</Badge>
            <div style={{flex: 1}}/>
            <span className='muted'>Install arch:</span>
            <select className='btn sm mono' value={arch} onChange={e => changeArch(e.target.value)}>
              {(archInfo.supported && archInfo.supported.length ? archInfo.supported : [arch]).map(a => (
                <option key={a} value={a}>{a}{a === archInfo.primary ? ' (recommended)' : ''}</option>
              ))}
            </select>
          </div>
        )}

        {relLoading && <div className='muted' style={{padding: 16, textAlign: 'center'}}>Loading releases…</div>}
        {!relLoading && relError && (
          <div className='card' style={{padding: 12, borderColor: 'var(--err)'}}>
            <div style={{fontSize: 12, color: 'var(--err)'}}>{relError}</div>
          </div>
        )}
        {!relLoading && !relError && releases && (
          <div style={{display: 'flex', flexDirection: 'column', gap: 6, maxHeight: 420, overflow: 'auto'}}>
            {releases.filter(r => !verFilter || r.version.includes(verFilter)).map(r => (
              <div key={r.version} className='frida-server-row'>
                <div>
                  <div className='meta-row'>
                    <strong>{r.version}</strong>
                    <Badge>{r.arch}</Badge>
                    {r.installed && <Badge kind='ok'>installed</Badge>}
                  </div>
                  <div className='filename'>frida-server-{r.version}-android-{r.arch}.xz · {fmtMB(r.size)}</div>
                </div>
                <div style={{flex: 1}}/>
                {r.installed
                  ? <button className='btn sm' disabled>✓ Installed</button>
                  : <button className='btn sm primary' disabled={!!installing} onClick={() => installVersion(r)}>
                      {installing === r.version ? '…installing' : <><Icon.Download/>Install</>}
                    </button>}
              </div>
            ))}
            {releases.length === 0 && <div className='muted' style={{padding: 16, textAlign: 'center'}}>No compatible frida-server builds found.</div>}
          </div>
        )}
      </Modal>
    </div>
  );
}

// FridaRuntimeTab manages the HOST side of Frida: the Python runtime(s) that
// actually drive instrumentation. Two modes — adbq-managed venvs (pinned to the
// device's server version, verified wheel, offline install) and user-registered
// external interpreters ("bring your own frida", adbq installs nothing).
function FridaRuntimeTab({device}: {device: adb.Device}) {
  const [host, setHost] = useState<adb.FridaHostInfo | null>(null);
  const [runtimes, setRuntimes] = useState<adb.FridaRuntime[]>([]);
  const [deviceVer, setDeviceVer] = useState('');
  const [detecting, setDetecting] = useState(false);
  const [installing, setInstalling] = useState('');
  const [stage, setStage] = useState('');
  const [extPath, setExtPath] = useState('');
  const [managed, setManaged] = useState(true);

  const reload = () => {
    API.ListFridaRuntimes().then(r => setRuntimes(r || [])).catch(() => {});
    API.FridaManagedEnabled().then(setManaged).catch(() => {});
  };
  useEffect(() => {
    API.FridaHost().then(setHost).catch(() => setHost(null));
    reload();
  }, []);
  useEffect(() => { setDeviceVer(''); }, [device?.id]);

  function detect() {
    if (!device?.id) return;
    setDetecting(true);
    API.DetectRunningFridaVersion(device.id)
      .then(setDeviceVer)
      .catch(e => { setDeviceVer(''); showToast({title: 'No running frida-server', body: String(e), kind: 'info'}); })
      .finally(() => setDetecting(false));
  }

  function ensure(version: string) {
    if (!version) return;
    setInstalling(version);
    setStage('starting');
    // Stream install progress; the version pins the event so concurrent
    // installs don't cross-update each other's stage label.
    const ev = 'frida-venv:progress';
    EventsOn(ev, (p: {version: string; stage: string}) => { if (p?.version === version) setStage(p.stage); });
    API.EnsureFridaVenv(version)
      .then(rt => { showToast({title: 'Host venv ready', body: `frida ${rt.fridaVersion}`, kind: 'ok', mono: true}); reload(); })
      .catch(e => showToast({title: 'Venv setup failed', body: String(e), kind: 'err'}))
      .finally(() => { EventsOff(ev); setInstalling(''); setStage(''); });
  }

  function addExternal() {
    const p = extPath.trim();
    const call = p ? API.RegisterExternalFrida(p) : API.PickExternalFridaInterpreter();
    call.then(rt => {
      if (rt && rt.id) { showToast({title: 'Interpreter added', body: `frida ${rt.fridaVersion}`, kind: 'ok', mono: true}); setExtPath(''); reload(); }
    }).catch(e => showToast({title: 'Could not add interpreter', body: String(e), kind: 'err'}));
  }

  function remove(rt: adb.FridaRuntime) {
    confirmDialog({
      title: rt.kind === 'managed' ? 'Delete managed venv?' : 'Forget interpreter?',
      body: rt.kind === 'managed' ? `Removes adbq's venv for frida ${rt.fridaVersion}.` : rt.pythonPath,
      confirmLabel: rt.kind === 'managed' ? 'Delete' : 'Forget', danger: true,
    }).then(ok => {
      if (!ok) return;
      API.RemoveFridaRuntime(rt.id).then(reload).catch(e => showToast({title: 'Remove failed', body: String(e), kind: 'err'}));
    });
  }

  function toggleManaged() {
    const next = !managed;
    setManaged(next);
    API.SetFridaManagedEnabled(next).catch(() => setManaged(!next));
  }

  return (
    <div style={{display: 'flex', flexDirection: 'column', gap: 12}}>
      <div className='card'>
        <div className='card-header'>
          <span className='title'>Host Python</span>
          <div style={{flex: 1}}/>
          <button className='btn sm' onClick={() => API.FridaHost().then(setHost).catch(() => {})} title='Recheck'><Icon.Refresh/></button>
        </div>
        <div className='card-body' style={{fontSize: 12}}>
          {host?.available ? (
            <div style={{display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap'}}>
              <Badge kind='ok'>Python {host.pythonVersion}</Badge>
              <span className='mono subtle'>{host.pythonPath}</span>
              {!host.hasVenv && <Badge kind='warn'>venv module missing</Badge>}
            </div>
          ) : (
            <div style={{color: 'var(--warn)'}}>{host?.error || 'Checking for a host Python…'}</div>
          )}
        </div>
      </div>

      <div className='card'>
        <div className='card-header'>
          <span className='title'>Device frida-server</span>
          <div style={{flex: 1}}/>
          <button className='btn sm' onClick={detect} disabled={detecting || !device?.id}>{detecting ? '…detecting' : <><Icon.Refresh/>Detect</>}</button>
        </div>
        <div className='card-body' style={{fontSize: 12}}>
          {deviceVer ? (
            <div style={{display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap'}}>
              <span>Running server:</span><Badge kind='accent'>frida {deviceVer}</Badge>
              <div style={{flex: 1}}/>
              {host?.available && managed && (
                <button className='btn sm primary' onClick={() => ensure(deviceVer)} disabled={!!installing}>
                  {installing === deviceVer ? `…${stage || 'installing'}` : <><Icon.Download/>Create matching venv</>}
                </button>
              )}
            </div>
          ) : (
            <div className='muted'>Click <strong>Detect</strong> to read the running frida-server's version, then create a host venv pinned to it (needs a running server on the device).</div>
          )}
        </div>
      </div>

      <div className='card'>
        <div className='card-header'>
          <span className='title'>Host runtimes</span>
          <span className='subtle' style={{marginLeft: 8, fontSize: 11}}>{runtimes.length} available</span>
          <div style={{flex: 1}}/>
          <label className='muted' style={{display: 'flex', alignItems: 'center', gap: 6, fontSize: 11}}>
            <input type='checkbox' checked={managed} onChange={toggleManaged}/> allow managed installs
          </label>
        </div>
        <div className='card-body' style={{display: 'flex', flexDirection: 'column', gap: 6}}>
          {runtimes.length === 0 && <div className='muted' style={{fontSize: 12}}>No host runtimes yet. Create a managed venv above, or add your own interpreter below.</div>}
          {runtimes.map(rt => (
            <div key={rt.id} className='frida-server-row'>
              <div>
                <div className='meta-row'>
                  <strong>frida {rt.fridaVersion || '?'}</strong>
                  <Badge kind={rt.kind === 'managed' ? 'accent' : undefined}>{rt.kind}</Badge>
                  {rt.pythonVersion && <Badge>py {rt.pythonVersion}</Badge>}
                </div>
                <div className='filename'>{rt.pythonPath}</div>
              </div>
              <div style={{flex: 1}}/>
              <button className='btn sm danger' onClick={() => remove(rt)} title={rt.kind === 'managed' ? 'Delete venv' : 'Forget'}>
                <Icon.Trash width={11} height={11}/>
              </button>
            </div>
          ))}
          <div style={{display: 'flex', gap: 6, marginTop: 6, alignItems: 'center'}}>
            <input className='input mono' style={{flex: 1}} placeholder='/path/to/venv (or its bin/python) with frida installed'
                   value={extPath} onChange={e => setExtPath(e.target.value)}/>
            <button className='btn sm' onClick={addExternal}>{extPath.trim() ? 'Add path' : <><Icon.Upload/>Browse…</>}</button>
          </div>
          <div className='muted' style={{fontSize: 11}}>
            “Bring your own frida”: install <span className='mono'>frida</span> yourself (e.g. <span className='mono'>pip install frida=={deviceVer || 'X.Y.Z'}</span>) and point adbq at that interpreter — adbq installs nothing.
          </div>
        </div>
      </div>
    </div>
  );
}

function fmtMB(n: number): string {
  if (!n) return '—';
  if (n < 1024 * 1024) return (n / 1024).toFixed(0) + ' KB';
  return (n / 1024 / 1024).toFixed(1) + ' MB';
}

const SCRIPTS = [
  {name: 'frida-ps',          desc: 'List processes on device',                                 cmd: 'frida-ps -U'},
  {name: 'frida-ls-devices',  desc: 'Show all attached devices',                                cmd: 'frida-ls-devices'},
  {name: 'attach by name',    desc: 'Attach to a package and start a REPL',                     cmd: 'frida -U com.example.app'},
  {name: 'codeshare/pinning', desc: 'SSL pinning bypass (univeral)',                            cmd: 'frida -U --codeshare akabe1/frida-multiple-unpinning -f com.example.app'},
  {name: 'root detect bypass',desc: 'Bypass common root detection',                             cmd: 'frida -U --codeshare dzonerzy/fridantiroot -f com.example.app'},
  {name: 'dump prefs',        desc: 'Hook SharedPreferences and dump on read/write',            cmd: '# inline JS — see frida codeshare snippets'},
];
