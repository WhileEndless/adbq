import React, {useEffect, useLayoutEffect, useMemo, useState} from 'react';
import {adb} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {EventsOn, EventsOff} from '../../wailsjs/runtime/runtime';
import {Icon} from '../icons';
import {Badge, CommandChip, CommandPreview, Modal, SearchInput, commandToast, confirmDialog, showToast} from '../ui';
import {useStore} from '../store';
import {SEARCH_DEBOUNCE_MS, highlight} from '../lib/logSearch';
import {rootUnavailableReason} from '../lib/android';
import {CodeEditor} from '../components/CodeEditor';
import {fileStamp, saveTextAs} from '../lib/saveText';

export function FridaScreen({device}: {device: adb.Device}) {
  const store = useStore();
  const [servers, setServers] = useState<adb.FridaServer[]>([]);
  const [port, setPort] = useState(27042);
  const [iface, setIface] = useState('0.0.0.0');
  const [starting, setStarting] = useState<string | null>(null);
  const [stopping, setStopping] = useState(false);
  // frida-server's own stdout/stderr, captured on the device per port. Empty
  // means the last launch on that port was clean.
  const [srvLog, setSrvLog] = useState('');
  const [logOpen, setLogOpen] = useState(false);
  const [listError, setListError] = useState('');
  const [tab, setTab] = useState<'server' | 'runtime' | 'scripts' | 'appscripts' | 'sessions'>('server');
  const sessionCount = Object.keys(store.fridaSessions).length;

  // Honor a cross-screen request to open on a specific tab (e.g. Apps
  // "Start with Frida" lands here on Sessions).
  useEffect(() => {
    const req = store.consumeFridaTab();
    if (req === 'server' || req === 'runtime' || req === 'scripts' || req === 'appscripts' || req === 'sessions') setTab(req);
  }, [store]);

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
    // A build rated incompatible fails in a way that is hard to attribute later
    // — it installs and starts fine, and only the agent inside the target app
    // gives up — so say so before the download, not after.
    const body = r.advice === 'broken'
      ? `${r.adviceNote}\n\nInstall it anyway?`
      : `adbq will download the official ${r.arch} build (${fmtMB(r.size)}) from GitHub, verify its SHA256, decompress it locally, and push it to /data/local/tmp.`;
    const ok = await confirmDialog({
      title: r.advice === 'broken'
        ? `frida-server ${r.version} is not compatible with this device`
        : `Install frida-server ${r.version}?`,
      body,
      confirmLabel: r.advice === 'broken' ? 'Install anyway' : 'Download & install',
      danger: r.advice === 'broken',
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
      setListError('');
      const act = arr.find(x => x.active);
      if (act && act.port) setPort(act.port);
    }).catch(e => {
      // Surface the failure in place instead of only as a toast: an empty list
      // and an unreadable device used to look identical here.
      setServers([]);
      setListError(String(e));
      showToast({title: 'Listing frida-server failed', body: String(e), kind: 'err'});
    });
  };
  // Logs are per-port, since a device can run several servers at once; show the
  // one for the server in view (the running one, else the port about to be used).
  const loadServerLog = (p?: number) => {
    if (!device?.id) return Promise.resolve('');
    return API.FridaServerLog(device.id, p ?? port)
      .then(t => { setSrvLog(t || ''); return t || ''; })
      .catch(() => { setSrvLog(''); return ''; });
  };
  useEffect(() => { reload(); loadServerLog(); }, [device?.id]);

  function start(s: adb.FridaServer) {
    if (!device.root) {
      showToast({title: 'Root required', body: `frida-server needs root. ${rootUnavailableReason(device)}`, kind: 'err'});
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
              showToast({
                title: 'frida-server started', body: `${s.version || s.name} on ${iface}:${port}`,
                kind: 'ok', mono: true, actions: commandToast(cmds?.start),
              });
              setStarting(null);
              reload();
            } else if (tries >= 12) {
              // The server's own output says why far more reliably than logcat.
              loadServerLog(port).then(log => {
                const first = (log || '').split('\n').find(l => l.trim()) || '';
                setLogOpen(!!first);
                showToast({
                  title: 'No active server detected',
                  body: first || 'The server left no output — the binary may not match the device architecture.',
                  kind: 'err', mono: !!first,
                });
              });
              setStarting(null);
              reload();
            } else {
              setTimeout(check, 400);
            }
          }).catch(() => { setStarting(null); });
        };
        setTimeout(check, 500);
      })
      .catch(e => {
        setStarting(null);
        // The backend already folds the device-side log into this error, so the
        // message is the real cause rather than a generic failure.
        loadServerLog(port).then(log => setLogOpen(!!log.trim()));
        showToast({title: 'Start failed', body: String(e), kind: 'err', mono: true});
      });
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
    // The rm comes from Go, same as everywhere else a file is deleted.
    API.FileCommands(device.id, {dir: '/data/local/tmp', name: s.name, isDir: false, asRoot: false, mode: '', owner: ''} as adb.FileCommandRequest)
      .catch(() => null)
      .then(fc => confirmDialog({
        title: 'Delete binary?',
        body: <>
          <span className='mono'>{s.path}</span>
          <CommandPreview commands={fc?.delete ?? []} defaultOpen/>
        </>,
        confirmLabel: 'Delete', danger: true,
      }))
      .then(ok => {
        if (!ok) return;
        API.DeleteFile(device.id, s.path, false, false).then(reload);
      });
  }

  const active = servers.find(s => s.active);

  // Everything this tab does to the device, as commands. Only Go knows the
  // daemonizing form and the `su` wrapper, so it renders them (CLAUDE.md §4.1).
  const [cmds, setCmds] = useState<adb.FridaCommands | null>(null);
  useEffect(() => {
    if (!device?.id) { setCmds(null); return; }
    let live = true;
    API.FridaCommands(device.id, active?.path || '', active?.port || port)
      .then(c => { if (live) setCmds(c); })
      .catch(() => { if (live) setCmds(null); });
    return () => { live = false; };
  }, [device?.id, active?.path, active?.port, port]);

  return (
    <div className='screen'>
      <div className='screen-header'>
        <h1>Frida {active && tab === 'server' && <Badge kind='accent'>active</Badge>}</h1>
        <div className='subtabs' style={{marginLeft: 10}}>
          <button className={`subtab${tab === 'server' ? ' active' : ''}`} onClick={() => setTab('server')}>Server</button>
          <button className={`subtab${tab === 'runtime' ? ' active' : ''}`} onClick={() => setTab('runtime')}>Runtime</button>
          <button className={`subtab${tab === 'scripts' ? ' active' : ''}`} onClick={() => setTab('scripts')}>Scripts</button>
          <button className={`subtab${tab === 'appscripts' ? ' active' : ''}`} onClick={() => setTab('appscripts')}>App Scripts</button>
          <button className={`subtab${tab === 'sessions' ? ' active' : ''}`} onClick={() => setTab('sessions')}>
            Sessions{sessionCount > 0 ? ` (${sessionCount})` : ''}
          </button>
        </div>
        <div className='spacer' style={{flex: 1}}/>
        {tab === 'server' && <>
          <CommandChip label='frida-server' groups={[
            {label: 'Install', commands: cmds?.install, note: 'The download is verified on this computer before anything is pushed.'},
            {label: 'List binaries', commands: cmds?.list},
            {label: 'Start', commands: cmds?.start},
            {label: 'Stop', commands: cmds?.stop},
            {label: 'Server log', commands: cmds?.log},
            {label: 'Port forward', commands: cmds?.forward},
          ]}/>
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
                <div className='mono subtle' style={{fontSize: 11, marginBottom: 8, display: 'flex', alignItems: 'center', gap: 6}}>
                  <span style={{overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'}}>
                    {active.path} · {iface}:{active.port}
                  </span>
                  <CommandChip label='Started with' commands={cmds?.start}/>
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
                {active.port !== 27042 && (
                  <div className='muted' style={{fontSize: 11, marginTop: 4}}>
                    Frida's Android backend only dials <span className='mono'>27042</span>, so adbq reaches this
                    server through an <span className='mono'>adb forward</span> it opens for the duration of each
                    session. Sessions work as usual; an external <span className='mono'>frida -U</span> will not see it.
                  </div>
                )}
              </>
            ) : (
              <div className='muted' style={{fontSize: 12}}>
                Pick a binary below and click <strong>Start</strong>. Interface defaults to <span className='mono'>0.0.0.0</span>, port <span className='mono'>{port}</span>.
                {port !== 27042 && <div style={{marginTop: 4}}>
                  Off the default port, adbq forwards the port per session so instrumentation still works.
                </div>}
                {!device.root && <div style={{marginTop: 6, color: 'var(--warn)'}}>
                  {rootUnavailableReason(device)} frida-server cannot bind without root
                  — use frida-gadget (LD_PRELOAD via repackaging) instead.
                </div>}
              </div>
            )}
          </div>
        </div>

        {/* Server log — frida-server writes here only when something went wrong,
            so an empty log is the healthy state and the panel stays collapsed. */}
        <div className='card' style={{marginBottom: 14}}>
          <div className='card-header' style={{cursor: 'pointer'}} onClick={() => { setLogOpen(o => !o); if (!logOpen) loadServerLog(active?.port || port); }}>
            <span className='title'>Server log</span>
            {srvLog.trim()
              ? <Badge kind='warn'>output</Badge>
              : <span className='muted' style={{fontSize: 11}}>clean</span>}
            <div style={{flex: 1}}/>
            <span onClick={e => e.stopPropagation()}><CommandChip label='Read log' commands={cmds?.log}/></span>
            <button className='btn sm' onClick={e => { e.stopPropagation(); loadServerLog(active?.port || port); setLogOpen(true); }}>
              <Icon.Refresh width={12} height={12}/>
            </button>
          </div>
          {logOpen && (
            <div className='card-body'>
              {srvLog.trim() ? (
                <pre className='mono' style={{fontSize: 11, margin: 0, maxHeight: 220, overflow: 'auto', whiteSpace: 'pre-wrap'}}>{srvLog}</pre>
              ) : (
                <div className='muted' style={{fontSize: 12}}>
                  Nothing logged — the last launch was clean. Failures (SELinux denial, busy port,
                  architecture mismatch, an agent that cannot map this Android's ART) show up here.
                </div>
              )}
            </div>
          )}
        </div>

        {/* Binaries list */}
        <div style={{display: 'flex', flexDirection: 'column', gap: 8}}>
          {/* "we could not ask" is not the same answer as "nothing is installed",
              and offering to push a binary in the first case is misleading. */}
          {listError ? (
            <div className='card' style={{padding: 16, textAlign: 'center'}}>
              <div style={{color: 'var(--warn)', marginBottom: 6}}>Could not list frida-server binaries.</div>
              <div className='mono subtle' style={{fontSize: 11, marginBottom: 8}}>{listError}</div>
              <button className='btn' onClick={reload}><Icon.Refresh/>Retry</button>
            </div>
          ) : servers.length === 0 && (
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
                  {!s.active && s.ambiguous && (
                    <span title='A frida-server is running, but without root we cannot tell which of these binaries it is'>
                      <Badge kind='warn'>maybe running</Badge>
                    </span>
                  )}
                  {!s.runnable && <Badge kind='warn'>not runnable</Badge>}
                </div>
                <div className='filename'>{s.path} · {(s.size / 1024 / 1024).toFixed(1)} MB · {s.perms}</div>
              </div>
              <div className='mono subtle' style={{fontSize: 11}}>{s.arch}</div>
              <div className='mono subtle' style={{fontSize: 11}}>{s.perms}</div>
              <div style={{display: 'flex', gap: 4}}>
                {s.active
                  ? <button className='btn sm danger' onClick={stop} disabled={stopping}>{stopping ? '…stopping' : <><Icon.Stop/>Stop</>}</button>
                  : <button className='btn sm primary' onClick={() => start(s)}
                      disabled={!device.root || !!starting || !s.runnable}
                      title={s.runnable ? undefined : 'This file is an archive or has no execute bit — it cannot be launched'}>
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
        ) : tab === 'scripts' ? (
          <FridaScriptsTab/>
        ) : tab === 'appscripts' ? (
          <FridaAppScriptsTab/>
        ) : (
          <FridaSessionsTab device={device}/>
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
                    {/* Some published builds cannot work on this device's Android
                        at all, and the failure is invisible from the outside —
                        the server starts and only the agent inside the app fails. */}
                    {r.advice === 'broken' && <Badge kind='err'>not compatible</Badge>}
                    {r.advice === 'warn' && <Badge kind='warn'>may not work</Badge>}
                  </div>
                  <div className='filename'>frida-server-{r.version}-android-{r.arch}.xz · {fmtMB(r.size)}</div>
                  {r.adviceNote && (
                    <div style={{fontSize: 11, marginTop: 3, color: r.advice === 'broken' ? 'var(--err)' : 'var(--warn)'}}>
                      {r.adviceNote}
                    </div>
                  )}
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
                  <Badge kind={rt.kind === 'managed' ? 'accent' : rt.kind === 'system' ? 'ok' : undefined}>{rt.kind === 'system' ? 'discovered' : rt.kind}</Badge>
                  {rt.pythonVersion && <Badge>py {rt.pythonVersion}</Badge>}
                </div>
                <div className='filename'>{rt.pythonPath}</div>
              </div>
              <div style={{flex: 1}}/>
              {rt.kind === 'system'
                ? <span className='subtle' style={{fontSize: 11}} title='Already installed on this machine — used automatically'>auto</span>
                : <button className='btn sm danger' onClick={() => remove(rt)} title={rt.kind === 'managed' ? 'Delete venv' : 'Forget'}>
                    <Icon.Trash width={11} height={11}/>
                  </button>}
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
  const [mode, setMode] = useState<'browse' | 'search'>('browse');
  const [page, setPage] = useState(1);
  const [hasMore, setHasMore] = useState(false);

  // Load the popular listing when the modal first opens.
  useEffect(() => {
    if (open && results.length === 0 && !q) run('');
    if (!open) { setPreview(null); setErr(''); }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // run searches CodeShare (full results) or, for an empty query, browses the
  // popular listing page 1. Browse is paginated (16/page) → "Load more"; search
  // returns everything matching in one response.
  function run(query: string) {
    const term = query.trim();
    setLoading(true);
    setErr('');
    if (!term) {
      setMode('browse');
      setPage(1);
      API.BrowseCodeshare(1)
        .then(r => { setResults(r || []); setHasMore((r || []).length >= 12); })
        .catch(e => { setErr(String(e)); setResults([]); setHasMore(false); })
        .finally(() => setLoading(false));
    } else {
      setMode('search');
      API.SearchCodeshare(term)
        .then(r => { setResults(r || []); setHasMore(false); })
        .catch(e => { setErr(String(e)); setResults([]); })
        .finally(() => setLoading(false));
    }
  }

  function loadMore() {
    const next = page + 1;
    setLoading(true);
    API.BrowseCodeshare(next)
      .then(r => {
        const add = r || [];
        setResults(prev => {
          const seen = new Set(prev.map(p => `${p.owner}/${p.slug}`));
          return prev.concat(add.filter(p => !seen.has(`${p.owner}/${p.slug}`)));
        });
        setPage(next);
        setHasMore(add.length >= 12);
      })
      .catch(() => setHasMore(false))
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
        <div style={{display: 'flex', flexDirection: 'column', gap: 10, maxHeight: 520}}>
          <form style={{display: 'flex', gap: 6}} onSubmit={e => { e.preventDefault(); run(q); }}>
            <SearchInput value={q} onChange={setQ} placeholder='Search CodeShare (e.g. ssl pinning)…'/>
            <button className='btn primary' type='submit'><Icon.Search/>Search</button>
            {q && <button className='btn' type='button' onClick={() => { setQ(''); run(''); }}>Clear</button>}
          </form>
          <div className='muted' style={{fontSize: 11, display: 'flex', alignItems: 'center', gap: 6}}>
            {mode === 'search'
              ? <span>{results.length} result{results.length === 1 ? '' : 's'} for “{q.trim()}”</span>
              : <span>Popular on CodeShare{results.length ? ` · ${results.length} loaded` : ''}</span>}
          </div>
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
            {loading && <div className='muted' style={{padding: 12, textAlign: 'center'}}>Loading…</div>}
            {!loading && mode === 'browse' && hasMore && (
              <button className='btn' style={{margin: '6px auto'}} onClick={loadMore}>Load more</button>
            )}
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

// FridaAppScriptsTab is the device-independent overview of every package→scripts
// binding (bindings are package-keyed, so this is the same on any device). It
// resolves script IDs to names and lets the user detach a binding.
function FridaAppScriptsTab() {
  const [bindings, setBindings] = useState<adb.AppScripts[]>([]);
  const [names, setNames] = useState<Record<string, string>>({});

  const reload = () => {
    Promise.all([API.ListAppFridaScripts(), API.ListFridaScripts()]).then(([b, scripts]) => {
      setBindings(b || []);
      const m: Record<string, string> = {};
      (scripts || []).forEach(s => { m[s.id] = s.name; });
      setNames(m);
    }).catch(e => showToast({title: 'Load failed', body: String(e), kind: 'err'}));
  };
  useEffect(() => { reload(); }, []);

  function clearBinding(pkg: string) {
    confirmDialog({title: `Detach all Frida scripts from ${pkg}?`, body: 'The scripts stay in your library; only this app’s binding is removed.', confirmLabel: 'Detach', danger: true}).then(ok => {
      if (!ok) return;
      API.SetAppFridaScripts(pkg, [], 'spawn', '').then(reload).catch(e => showToast({title: 'Failed', body: String(e), kind: 'err'}));
    });
  }

  return (
    <div className='card'>
      <div className='card-header'>
        <span className='title'>App → script bindings</span>
        <span className='subtle' style={{marginLeft: 8, fontSize: 11}}>device-independent · {bindings.length}</span>
        <div style={{flex: 1}}/>
        <button className='btn sm' onClick={reload}><Icon.Refresh/></button>
      </div>
      {bindings.length === 0 ? (
        <div className='card-body'>
          <div className='muted' style={{fontSize: 12, padding: 8}}>
            No apps have Frida scripts attached yet. Open an app in the Apps tab and use <strong>Manage scripts</strong>.
          </div>
        </div>
      ) : (
        <table className='table'>
          <thead><tr><th>Package</th><th>Mode</th><th>Scripts</th><th className='actions'></th></tr></thead>
          <tbody>
            {bindings.map(b => (
              <tr key={b.package}>
                <td className='mono'>{b.package}</td>
                <td><Badge>{b.mode}</Badge></td>
                <td className='muted'>
                  {(b.scriptIds && b.scriptIds.length)
                    ? b.scriptIds.map(id => names[id] || id).join(', ')
                    : '—'}
                </td>
                <td className='actions'>
                  <button className='btn sm danger' onClick={() => clearBinding(b.package)} title='Detach'>
                    <Icon.Trash width={11} height={11}/>
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

// FridaSessionsTab lists live/finished sessions and renders a colored console
// for the selected one. Messages keep arriving into the store while this tab is
// closed; on mount we re-attach (idempotent) so nothing is missed.
function FridaSessionsTab({device}: {device: adb.Device}) {
  const store = useStore();
  const sessions = Object.values(store.fridaSessions).sort((a, b) => b.info.startedAt - a.info.startedAt);
  const [selId, setSelId] = useState('');
  const sel = store.fridaSessions[selId] || sessions[0];
  const [history, setHistory] = useState<adb.FridaHistoryEntry[]>([]);
  const [repeating, setRepeating] = useState('');

  const loadHistory = () => { API.ListFridaHistory().then(h => setHistory(h || [])).catch(() => {}); };

  // Re-attach every known session so a remount/HMR re-subscribes + backfills.
  useEffect(() => {
    Object.keys(store.fridaSessions).forEach(id => store.attachFridaSession(id));
    loadHistory();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function repeat(h: adb.FridaHistoryEntry) {
    if (!device?.id) { showToast({title: 'No device', body: 'Connect a device first', kind: 'err'}); return; }
    setRepeating(h.package);
    try {
      // Repeat a past launch against whatever server is up now, on its own port
      // — it may not be the one (or the port) the original run used.
      const list = await API.ListFridaServers(device.id).catch(() => [] as adb.FridaServer[]);
      const active = (list || []).find(x => x.active);
      const info = await store.startFridaSession(device.id, h.package, h.mode, h.runtimeVer, active?.port || 0, h.scriptIds || []);
      setSelId(info.id);
      loadHistory();
    } catch (e) {
      showToast({title: 'Repeat failed', body: String(e), kind: 'err'});
    } finally {
      setRepeating('');
    }
  }
  function forget(pkg: string) { API.RemoveFridaHistory(pkg).then(loadHistory).catch(() => {}); }

  const recents = (
    <div className='card' style={{display: 'flex', flexDirection: 'column', minHeight: 0}}>
      <div className='card-header'>
        <span className='title'>Recents</span>
        {history.length > 0 && <Badge>{history.length}</Badge>}
        <div style={{flex: 1}}/>
        {history.length > 0 && (
          <button className='btn sm' title='Clear history' onClick={() => confirmDialog({title: 'Clear Frida recents?', body: 'Forget all previously instrumented apps.', confirmLabel: 'Clear', danger: true}).then(ok => { if (ok) API.ClearFridaHistory().then(loadHistory); })}>Clear</button>
        )}
      </div>
      <div style={{overflow: 'auto', minHeight: 0}}>
        {history.length === 0 && (
          <div className='muted' style={{padding: 14, fontSize: 12}}>
            No history yet. Launch an app from <strong>Start with Frida</strong> in Apps; it’ll appear here for one-click repeat.
          </div>
        )}
        {history.map(h => (
          <div key={h.package} className='frida-recent-row' style={{display: 'flex', alignItems: 'center', gap: 8, padding: '7px 10px', borderBottom: '1px solid var(--border)'}}>
            <div style={{minWidth: 0, flex: 1}}>
              <div className='mono' style={{fontSize: 12, fontWeight: 600, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'}}>{h.package}</div>
              <div className='subtle' style={{fontSize: 10.5, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'}}>
                {h.mode}{h.scriptNames && h.scriptNames.length ? ' · ' + h.scriptNames.join(', ') : ' · no scripts'} · {fmtAgo(h.lastRun)}{h.count > 1 ? ` · ×${h.count}` : ''}
              </div>
            </div>
            <button className='btn sm primary' disabled={!!repeating} onClick={() => repeat(h)} title='Run again with the same scripts'>
              {repeating === h.package ? '…' : <><Icon.Play/>Repeat</>}
            </button>
            <button className='btn sm' title='Forget' onClick={() => forget(h.package)}><Icon.Trash width={11} height={11}/></button>
          </div>
        ))}
      </div>
    </div>
  );

  return (
    <div style={{height: '100%', display: 'grid', gridTemplateColumns: 'minmax(220px, 300px) minmax(0, 1fr)', gap: 12, minHeight: 0}}>
      <div style={{display: 'flex', flexDirection: 'column', gap: 10, minHeight: 0}}>
        <div style={{flex: '0 0 auto', maxHeight: '45%', display: 'flex', flexDirection: 'column', minHeight: 0}}>{recents}</div>
        <div style={{flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0, border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden'}}>
          <div className='card-header' style={{borderBottom: '1px solid var(--border)'}}><span className='title'>Sessions</span>{sessions.length > 0 && <Badge>{sessions.length}</Badge>}</div>
          <div style={{overflow: 'auto', minHeight: 0}}>
            {sessions.length === 0 && <div className='muted' style={{padding: 14, fontSize: 12}}>No live sessions. Use Repeat above, or Start with Frida in Apps.</div>}
            {sessions.map(s => (
              <div key={s.info.id} onClick={() => setSelId(s.info.id)}
                   style={{padding: '8px 10px', cursor: 'pointer', borderBottom: '1px solid var(--border)', background: sel?.info.id === s.info.id ? 'var(--accent-soft)' : undefined}}>
                <div style={{display: 'flex', alignItems: 'center', gap: 6}}>
                  <span style={{fontWeight: 600, fontSize: 12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'}}>{s.info.package}</span>
                  <div style={{flex: 1}}/>
                  <SessionStatusBadge slice={s}/>
                </div>
                <div className='subtle' style={{fontSize: 11}}>{s.info.mode} · frida {s.info.runtime}</div>
              </div>
            ))}
          </div>
        </div>
      </div>
      {sel ? <FridaConsole key={sel.info.id} slice={sel}/> : <div className='muted' style={{padding: 20, fontSize: 13}}>Select a session, or Repeat a recent app to start one.</div>}
    </div>
  );
}

// fmtAgo renders a compact "time ago" from a unix-seconds timestamp.
function fmtAgo(unixSec: number): string {
  if (!unixSec) return '';
  const s = Math.max(0, Math.floor(Date.now() / 1000 - unixSec));
  if (s < 60) return 'just now';
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

function SessionStatusBadge({slice}: {slice: import('../store').FridaSessionSlice}) {
  const st = slice.info.status;
  if (st === 'error') return <Badge kind='err'>error</Badge>;
  if (slice.ended || st === 'ended') return <Badge>ended</Badge>;
  return <Badge kind='ok'>live</Badge>;
}

// The buckets the console toolbar filters on. They are coarser than the raw
// message kinds on purpose: a reader wants "hide the lifecycle chatter" or
// "errors only", not a checkbox per protocol kind.
type FridaCat = 'log' | 'send' | 'warn' | 'err' | 'meta';

const FRIDA_CATS: {key: FridaCat; label: string; title: string}[] = [
  {key: 'log',  label: 'Logs',     title: 'console.log / console.info from the script'},
  {key: 'send', label: 'Sends',    title: 'send() payloads from the script'},
  {key: 'warn', label: 'Warnings', title: 'console.warn from the script'},
  {key: 'err',  label: 'Errors',   title: 'Script exceptions and fatal session failures'},
  {key: 'meta', label: 'Events',   title: 'Session lifecycle: driver ready, script loaded, resumed, detached'},
];
const ALL_CATS = FRIDA_CATS.map(c => c.key);

interface FridaRow {
  m: adb.FridaMsg;
  cat: FridaCat;
  tag: string;
  text: string;
}

/**
 * Renders one message into the parts the console shows. Filtering, search and
 * export all go through here so a query matches exactly the text on screen — a
 * reader searching for `detached` should find the lifecycle line even though
 * that word never appears in the raw payload.
 *
 * Returns null for messages that have nothing to display.
 */
function fridaRow(m: adb.FridaMsg): FridaRow | null {
  const cat: FridaCat =
    m.kind === 'error' || m.kind === 'fatal' ? 'err'
    : m.kind === 'send' ? 'send'
    : m.kind === 'log' ? (m.level === 'error' ? 'err' : m.level === 'warning' ? 'warn' : 'log')
    : 'meta';
  let text = m.payload || '';
  if (m.kind === 'detached') text = `— detached (${m.detail || 'session ended'}) —`;
  else if (m.kind === 'resumed') text = '— process resumed —';
  else if (m.kind === 'loaded') text = `— loaded ${m.script || 'script'} —`;
  else if (m.kind === 'ready') text = '— driver ready —';
  else if (m.kind === 'status') text = m.detail ? `— ${m.detail} —` : '';
  else if (m.kind === 'fatal') text = `✕ ${m.payload || ''}${m.detail ? ': ' + m.detail : ''}`;
  if (m.stack) text += `\n${m.stack}`;
  if (!text) return null;
  const tag = m.kind === 'send' ? 'send' : m.kind === 'log' ? (m.level || 'log') : m.kind;
  return {m, cat, tag, text};
}

function FridaConsole({slice}: {slice: import('../store').FridaSessionSlice}) {
  const store = useStore();
  const wrap = React.useRef<HTMLDivElement>(null);
  const [searchInput, setSearchInput] = useState('');
  const [search, setSearch] = useState('');
  const [cats, setCats] = useState<FridaCat[]>(ALL_CATS);
  const [follow, setFollow] = useState(true);
  const id = slice.info.id;
  // Whether the console is parked at its newest line; a fresh mount renders the
  // tail, so it starts true.
  const atBottom = React.useRef(true);
  // Row count at the moment following was switched off, so the jump pill can
  // report how much has arrived since.
  const missedFrom = React.useRef(0);

  useEffect(() => {
    const t = setTimeout(() => setSearch(searchInput), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(t);
  }, [searchInput]);

  const rows = useMemo(() => {
    const q = search.toLowerCase();
    const keep = new Set(cats);
    const out: FridaRow[] = [];
    for (const m of slice.messages) {
      const r = fridaRow(m);
      if (!r) continue;
      if (!keep.has(r.cat)) continue;
      if (q && !r.text.toLowerCase().includes(q) && !r.tag.toLowerCase().includes(q)) continue;
      out.push(r);
    }
    return out;
  }, [slice.messages, search, cats]);

  // Keep the latest line in view while following. useLayoutEffect so the jump
  // lands in the same frame the rows paint, instead of flashing the old offset.
  useLayoutEffect(() => {
    if (follow && wrap.current) wrap.current.scrollTop = wrap.current.scrollHeight;
  }, [slice.rev, follow, rows.length]);

  const stopFollowing = () => { setFollow(false); missedFrom.current = rows.length; };
  const resumeFollow = () => {
    atBottom.current = true;
    setFollow(true);
    if (wrap.current) wrap.current.scrollTop = wrap.current.scrollHeight;
  };
  const missed = follow ? 0 : Math.max(0, rows.length - missedFrom.current);

  // Never let the last kind be switched off: an empty console with every filter
  // dark looks like a broken session rather than a filter the reader set.
  const toggleCat = (c: FridaCat) => setCats(prev =>
    prev.includes(c) ? (prev.length === 1 ? prev : prev.filter(x => x !== c)) : [...prev, c]);
  const filtering = cats.length !== ALL_CATS.length || !!search;

  return (
    <div style={{display: 'flex', flexDirection: 'column', minHeight: 0, gap: 8}}>
      <div style={{display: 'flex', alignItems: 'center', gap: 8}}>
        <span className='mono' style={{fontSize: 12}}>{slice.info.package}</span>
        <SessionStatusBadge slice={slice}/>
        {slice.info.statusNote && <span className='subtle' style={{fontSize: 11, color: 'var(--err)'}}>{slice.info.statusNote}</span>}
        <div style={{flex: 1}}/>
        <span className='subtle mono' style={{fontSize: 11}}>
          {filtering ? `${rows.length} / ${slice.messages.length}` : `${slice.messages.length}`} lines
        </span>
        <button className={`btn sm${follow ? ' primary' : ''}`} title={follow ? 'Auto-scroll on' : 'Auto-scroll off'}
                onClick={() => (follow ? stopFollowing() : resumeFollow())}>
          <Icon.Activity width={12} height={12}/>Follow
        </button>
        <button className='btn sm' title='Save the visible lines to a text file'
                disabled={rows.length === 0} onClick={() => exportFridaLog(rows, slice.info)}>
          <Icon.Download width={12} height={12}/>Export
        </button>
        <CommandChip label={slice.info.package} groups={[
          {label: 'Running', commands: slice.info.commands?.runner},
          {label: 'Same attach with the frida CLI', commands: slice.info.commands?.cli},
        ]}/>
        <button className='btn sm' onClick={() => store.clearFridaSession(id)}>Clear</button>
        {!slice.ended && slice.info.status === 'running'
          ? <button className='btn sm danger' onClick={() => store.stopFridaSession(id)}><Icon.Stop/>Stop</button>
          : <button className='btn sm' onClick={() => store.removeFridaSession(id)}><Icon.Trash width={11} height={11}/>Remove</button>}
      </div>

      <div className='frida-console-toolbar'>
        <SearchInput value={searchInput} onChange={setSearchInput} placeholder='Search session output' style={{width: 240}}/>
        <div style={{flex: 1}}/>
        {FRIDA_CATS.map(c => (
          <button key={c.key} className={`btn sm${cats.includes(c.key) ? ' primary' : ''}`}
                  title={c.title} onClick={() => toggleCat(c.key)}>{c.label}</button>
        ))}
        <button className='btn sm' title='Clear the search and show every kind again'
                disabled={!filtering} onClick={() => { setSearchInput(''); setSearch(''); setCats(ALL_CATS); }}>Reset</button>
      </div>

      <div className='frida-console-viewport'>
        <div ref={wrap} className='frida-console' onScroll={e => {
          const el = e.currentTarget;
          const bottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
          // Scrolling up is an explicit "let me read this", so it takes over
          // from auto-scroll instead of fighting it. Our own scroll-to-bottom
          // lands at the bottom, so it never trips this.
          if (!bottom && atBottom.current && follow) stopFollowing();
          // Scrolling the whole way back down means "I'm caught up" — same
          // intent as pressing the jump pill.
          if (bottom && !follow) setFollow(true);
          atBottom.current = bottom;
        }}>
          {slice.messages.length === 0 && <div className='muted' style={{padding: 12, fontSize: 12}}>Waiting for output…</div>}
          {slice.messages.length > 0 && rows.length === 0 && (
            <div className='muted' style={{padding: 12, fontSize: 12}}>No matching lines. Clear the search, or re-enable a kind above.</div>
          )}
          {rows.map((r, i) => <FridaLogLine key={r.m.seq || i} row={r} q={search}/>)}
        </div>
        {!follow && (
          <button className='logcat-jump' onClick={resumeFollow}>
            <Icon.ChevronDown width={13} height={13}/>
            {missed > 0 ? `${missed} new line${missed === 1 ? '' : 's'}` : 'Jump to latest'}
          </button>
        )}
      </div>
    </div>
  );
}

function FridaLogLine({row, q}: {row: FridaRow; q: string}) {
  const t = new Date(row.m.time).toLocaleTimeString(undefined, {hour12: false});
  return (
    <div className={`frida-line ${row.cat}`}>
      <span className='t'>{t}</span>
      <span className='k'>{row.tag}</span>
      <span className='msg'>{highlight(row.text, q)}</span>
    </div>
  );
}

function exportFridaLog(rows: FridaRow[], info: adb.FridaSessionInfo) {
  const header = `# adbq frida session export — ${info.package} — ${new Date().toISOString()}\n`
    + `# mode=${info.mode} · frida ${info.runtime} · ${rows.length} lines\n\n`;
  const text = header + rows.map(r => `${new Date(r.m.time).toISOString()}  ${r.tag.padEnd(8)} ${r.text}`).join('\n');
  void saveTextAs({
    title: 'Export Frida session log',
    suggestedName: `frida-${info.package}-${fileStamp()}.txt`,
    content: text,
  });
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
