import React, {useEffect, useState} from 'react';
import {adb} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {EventsOn, EventsOff} from '../../wailsjs/runtime/runtime';
import {Icon} from '../icons';
import {Badge, Modal, SearchInput, confirmDialog, showToast} from '../ui';
import {useStore} from '../store';
import {CodeEditor} from '../components/CodeEditor';

export function FridaScreen({device}: {device: adb.Device}) {
  const store = useStore();
  const [servers, setServers] = useState<adb.FridaServer[]>([]);
  const [port, setPort] = useState(27042);
  const [iface, setIface] = useState('0.0.0.0');
  const [starting, setStarting] = useState<string | null>(null);
  const [stopping, setStopping] = useState(false);
  const [tab, setTab] = useState<'server' | 'runtime' | 'scripts'>('server');

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
          <button className={`btn sm${tab === 'scripts' ? ' primary' : ''}`} onClick={() => setTab('scripts')}>Scripts</button>
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
        </>) : tab === 'runtime' ? (
          <FridaRuntimeTab device={device}/>
        ) : (
          <FridaScriptsTab/>
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

// FridaScriptsTab is the device-independent script library: a list on the left,
// a CodeMirror editor on the right. Create / view / edit / save / delete.
function FridaScriptsTab() {
  const [scripts, setScripts] = useState<adb.FridaScript[]>([]);
  const [selId, setSelId] = useState('');
  const [draft, setDraft] = useState<adb.FridaScript | null>(null);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [csOpen, setCsOpen] = useState(false);

  const reload = (select?: string) => {
    API.ListFridaScripts().then(list => {
      setScripts(list || []);
      if (select !== undefined) setSelId(select);
    }).catch(e => showToast({title: 'List scripts failed', body: String(e), kind: 'err'}));
  };
  useEffect(() => { reload(); }, []);

  // Load the selected script's source into the editor (skip while editing an
  // unsaved new script, which has no id yet).
  useEffect(() => {
    if (!selId) return;
    API.GetFridaScript(selId).then(s => { setDraft(s); setDirty(false); }).catch(() => setDraft(null));
  }, [selId]);

  function newScript() {
    setSelId('');
    setDraft(adb.FridaScript.createFrom({name: 'New script', origin: 'local', trusted: true, source: '// Frida script\n'}));
    setDirty(true);
  }

  function save() {
    if (!draft || !draft.name.trim()) { showToast({title: 'Name required', body: 'Give the script a name before saving.', kind: 'info'}); return; }
    setSaving(true);
    API.SaveFridaScript(draft)
      .then(saved => { showToast({title: 'Saved', body: saved.name, kind: 'ok'}); setDraft(saved); setSelId(saved.id); setDirty(false); reload(); })
      .catch(e => showToast({title: 'Save failed', body: String(e), kind: 'err'}))
      .finally(() => setSaving(false));
  }

  function remove(s: adb.FridaScript) {
    confirmDialog({title: `Delete “${s.name}”?`, body: 'Removes the script and detaches it from any apps.', confirmLabel: 'Delete', danger: true}).then(ok => {
      if (!ok) return;
      API.DeleteFridaScript(s.id).then(() => {
        if (selId === s.id) { setSelId(''); setDraft(null); }
        reload();
      }).catch(e => showToast({title: 'Delete failed', body: String(e), kind: 'err'}));
    });
  }

  return (
    <div style={{height: '100%', display: 'grid', gridTemplateColumns: 'minmax(220px, 280px) minmax(0, 1fr)', gap: 12, minHeight: 0}}>
      <div style={{display: 'flex', flexDirection: 'column', minHeight: 0, border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden'}}>
        <div style={{display: 'flex', alignItems: 'center', gap: 6, padding: 8, borderBottom: '1px solid var(--border)'}}>
          <span className='title' style={{fontSize: 12}}>Library</span>
          <span className='subtle' style={{fontSize: 11}}>{scripts.length}</span>
          <div style={{flex: 1}}/>
          <button className='btn sm' onClick={() => setCsOpen(true)} title='Import from Frida CodeShare'><Icon.Globe width={12} height={12}/></button>
          <button className='btn sm primary' onClick={newScript}><Icon.Plus width={12} height={12}/>New</button>
        </div>
        <div style={{flex: 1, overflow: 'auto', minHeight: 0}}>
          {scripts.length === 0 && <div className='muted' style={{padding: 12, fontSize: 12}}>No scripts yet. Click <strong>New</strong> to create one.</div>}
          {scripts.map(s => (
            <div key={s.id} onClick={() => setSelId(s.id)}
                 style={{padding: '8px 10px', cursor: 'pointer', borderBottom: '1px solid var(--border)', background: selId === s.id ? 'var(--accent-soft)' : undefined}}>
              <div style={{display: 'flex', alignItems: 'center', gap: 6}}>
                <span style={{fontWeight: 600, fontSize: 12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'}}>{s.name}</span>
                {s.origin === 'codeshare' && <Badge>CS</Badge>}
              </div>
              {s.description && <div className='subtle' style={{fontSize: 11, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'}}>{s.description}</div>}
            </div>
          ))}
        </div>
      </div>

      <div style={{display: 'flex', flexDirection: 'column', minHeight: 0, gap: 8}}>
        {draft ? (<>
          <div style={{display: 'flex', alignItems: 'center', gap: 8}}>
            <input className='input' style={{flex: 1, fontWeight: 600}} value={draft.name} placeholder='Script name'
                   onChange={e => { setDraft({...draft, name: e.target.value} as adb.FridaScript); setDirty(true); }}/>
            <button className='btn primary' onClick={save} disabled={saving || !dirty}>{saving ? '…saving' : <><Icon.Check/>Save</>}</button>
            {draft.id !== '' && <button className='btn danger' onClick={() => remove(draft)} title='Delete'><Icon.Trash/></button>}
          </div>
          <input className='input' style={{fontSize: 12}} value={draft.description} placeholder='Short description (optional)'
                 onChange={e => { setDraft({...draft, description: e.target.value} as adb.FridaScript); setDirty(true); }}/>
          {draft.origin === 'codeshare' && draft.codeshareOwner && (
            <div className='muted' style={{fontSize: 11, display: 'flex', alignItems: 'center', gap: 6}}>
              From CodeShare: <span className='mono'>@{draft.codeshareOwner}/{draft.codeshareSlug}</span>
              {!draft.trusted && <Badge kind='warn'>untrusted</Badge>}
            </div>
          )}
          <div style={{flex: 1, minHeight: 0}}>
            <CodeEditor value={draft.source || ''} onChange={v => { setDraft(d => d ? ({...d, source: v} as adb.FridaScript) : d); setDirty(true); }}/>
          </div>
        </>) : (
          <div className='muted' style={{display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', fontSize: 13}}>
            Select a script, or click&nbsp;<strong>New</strong>&nbsp;to create one.
          </div>
        )}
      </div>
      <CodeshareModal open={csOpen} onClose={() => setCsOpen(false)} onImported={(s) => { reload(s.id); setCsOpen(false); }}/>
    </div>
  );
}

// CodeshareModal lets the user search Frida CodeShare, preview a script's source
// (untrusted — shown read-only, never executed), and import it into the library.
function CodeshareModal({open, onClose, onImported}: {open: boolean; onClose: () => void; onImported: (s: adb.FridaScript) => void}) {
  const [q, setQ] = useState('');
  const [results, setResults] = useState<adb.CodeshareProject[]>([]);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');
  const [preview, setPreview] = useState<adb.CodeshareScript | null>(null);
  const [importing, setImporting] = useState(false);

  // Load the popular listing when the modal first opens.
  useEffect(() => {
    if (open && results.length === 0 && !q) search('');
    if (!open) { setPreview(null); setErr(''); }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  function search(query: string) {
    setLoading(true);
    setErr('');
    API.SearchCodeshare(query)
      .then(r => setResults(r || []))
      .catch(e => { setErr(String(e)); setResults([]); })
      .finally(() => setLoading(false));
  }

  function openPreview(p: adb.CodeshareProject) {
    setPreview(null);
    setErr('');
    API.GetCodeshareScript(p.owner, p.slug)
      .then(setPreview)
      .catch(e => setErr(String(e)));
  }

  function doImport(p: {owner: string; slug: string}) {
    setImporting(true);
    API.ImportCodeshareScript(p.owner, p.slug)
      .then(s => { showToast({title: 'Imported (untrusted)', body: s.name, kind: 'ok'}); onImported(s); })
      .catch(e => showToast({title: 'Import failed', body: String(e), kind: 'err'}))
      .finally(() => setImporting(false));
  }

  return (
    <Modal open={open} onClose={onClose} title='Frida CodeShare' width={760}
      footer={<button className='btn' onClick={onClose}>Close</button>}>
      {!preview ? (
        <div style={{display: 'flex', flexDirection: 'column', gap: 10, maxHeight: 480}}>
          <form style={{display: 'flex', gap: 6}} onSubmit={e => { e.preventDefault(); search(q); }}>
            <SearchInput value={q} onChange={setQ} placeholder='Search CodeShare (e.g. ssl pinning)…'/>
            <button className='btn primary' type='submit'><Icon.Search/>Search</button>
          </form>
          {loading && <div className='muted' style={{padding: 16, textAlign: 'center'}}>Loading…</div>}
          {err && <div className='card' style={{padding: 10, borderColor: 'var(--err)', fontSize: 12, color: 'var(--err)'}}>{err}</div>}
          <div style={{display: 'flex', flexDirection: 'column', gap: 4, overflow: 'auto'}}>
            {!loading && results.length === 0 && !err && (
              <div className='muted' style={{padding: 16, textAlign: 'center', fontSize: 12}}>
                No results. CodeShare discovery relies on its website; you can still import by exact <span className='mono'>owner/slug</span> from a project URL.
              </div>
            )}
            {results.map(r => (
              <div key={`${r.owner}/${r.slug}`} className='frida-server-row' style={{cursor: 'pointer'}} onClick={() => openPreview(r)}>
                <div style={{minWidth: 0}}>
                  <div className='meta-row'><strong style={{overflow: 'hidden', textOverflow: 'ellipsis'}}>{r.title || r.slug}</strong></div>
                  <div className='filename'>@{r.owner}/{r.slug}</div>
                </div>
                <div style={{flex: 1}}/>
                <span className='subtle' style={{fontSize: 11, whiteSpace: 'nowrap'}}>♥ {r.likes || '0'} · 👁 {r.views || '0'}</span>
                <button className='btn sm' onClick={e => { e.stopPropagation(); openPreview(r); }}>View</button>
              </div>
            ))}
          </div>
        </div>
      ) : (
        <div style={{display: 'flex', flexDirection: 'column', gap: 10}}>
          <div style={{display: 'flex', alignItems: 'center', gap: 8}}>
            <button className='btn sm' onClick={() => setPreview(null)}>← Back</button>
            <div style={{minWidth: 0}}>
              <div style={{fontWeight: 600}}>{preview.projectName || preview.slug}</div>
              <div className='subtle mono' style={{fontSize: 11}}>@{preview.owner}/{preview.slug}{preview.fridaVersion ? ` · author's frida ${preview.fridaVersion}` : ''}</div>
            </div>
            <div style={{flex: 1}}/>
            <button className='btn primary' onClick={() => doImport(preview)} disabled={importing}>
              {importing ? '…importing' : <><Icon.Download/>Import</>}
            </button>
          </div>
          {preview.description && <div className='muted' style={{fontSize: 12}}>{preview.description}</div>}
          <div style={{color: 'var(--warn)', fontSize: 11, display: 'flex', alignItems: 'center', gap: 6}}>
            <Icon.Shield width={13} height={13}/> Untrusted code from a third party. Review it before running it against any app.
          </div>
          <div style={{height: 320, minHeight: 0}}>
            <CodeEditor value={preview.source} readOnly/>
          </div>
        </div>
      )}
    </Modal>
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
