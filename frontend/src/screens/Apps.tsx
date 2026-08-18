import React, {useEffect, useMemo, useRef, useState} from 'react';
import {adb} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {Icon} from '../icons';
import {Badge, CommandChip, CommandPreview, commandToast, confirmDialog, DataAge, Modal, SearchInput, showToast} from '../ui';
import {useStore} from '../store';
import {pickApkAndInstall} from '../lib/apk';
import {ensureJadx, jadxInfo, jadxLabel} from '../lib/jadx';
import {sdkLabel, rootUnavailableReason} from '../lib/android';
import {deviceKey as cacheKey, getOrFetch, useDeviceData} from '../cache';
import {APPS_STALE_MS} from '../App';

// App details (permissions, version, sizes) move only when the app itself is
// installed, updated or cleared — all of which invalidate the apps domain from
// the backend, so this TTL is only a backstop for changes made outside adbq.
const APP_DETAIL_STALE_MS = 10 * 60_000;

export function AppsScreen({device, setScreen}: {device: adb.Device; setScreen?: (s: string) => void}) {
  const [q, setQ] = useState('');
  const [onlyUser, setOnlyUser] = useState(true);
  const [sel, setSel] = useState<adb.App | null>(null);
  const [detail, setDetail] = useState<adb.AppDetail | null>(null);

  const store = useStore();
  // One cache, one key shape. This list used to be fetched three ways — here at
  // 60s, by Logcat at 30s under the SAME key (so whichever screen loaded first
  // decided the TTL), and uncached by the sidebar badge on every screen change.
  const listKey = device?.id ? cacheKey('apps', device.id, onlyUser ? 'user' : 'all') : null;
  const {data: appsData, loading, refreshing, refresh, fetchedAt} = useDeviceData(
    listKey, () => API.ListApps(device.id, onlyUser), {staleMs: APPS_STALE_MS},
  );
  const apps = appsData ?? [];

  // Use an epoch counter so a stale DescribeApp resolve doesn't overwrite
  // detail for a newly-selected app.
  const reqId = useRef(0);
  useEffect(() => {
    if (!sel) { setDetail(null); return; }
    const my = ++reqId.current;
    // Namespaced under the apps domain so an install/uninstall/clear drops the
    // per-app detail too, not just the list it came from.
    getOrFetch(cacheKey('apps', device.id, 'describe', sel.pkg), () => API.DescribeApp(device.id, sel.pkg), APP_DETAIL_STALE_MS)
      .then(d => { if (my === reqId.current) setDetail(d ?? null); })
      .catch(() => { if (my === reqId.current) setDetail(null); });
  }, [sel?.pkg, device?.id]);

  // Live "is this app running" probe. Polls every 3s while the detail
  // panel is open so the Launch/Kill button reflects reality without the
  // user having to refresh by hand.
  const [running, setRunning] = useState<adb.AppRunning | null>(null);
  useEffect(() => {
    if (!sel) { setRunning(null); return; }
    let cancelled = false;
    const probe = () => API.IsAppRunning(device.id, sel.pkg)
      .then(r => { if (!cancelled) setRunning(r); })
      .catch(() => { if (!cancelled) setRunning(null); });
    probe();
    const t = setInterval(probe, 3000);
    return () => { cancelled = true; clearInterval(t); };
  }, [sel?.pkg, device?.id]);
  // What each action in this panel will run. Fetched per selection because the
  // export step is named after the app's version and the root steps carry the
  // `su` form this device accepts — neither is guessable in the frontend.
  const [cmds, setCmds] = useState<adb.AppCommands | null>(null);
  useEffect(() => {
    if (!sel) { setCmds(null); return; }
    let live = true;
    API.AppCommands(device.id, sel.pkg)
      .then(c => { if (live) setCmds(c); })
      .catch(() => { if (live) setCmds(null); });
    return () => { live = false; };
  }, [sel?.pkg, device?.id]);

  function refreshRunning() {
    if (!sel) return;
    API.IsAppRunning(device.id, sel.pkg).then(setRunning).catch(() => setRunning(null));
  }

  const filtered = useMemo(() => {
    const Q = q.toLowerCase();
    return apps.filter(a => !Q || a.pkg.toLowerCase().includes(Q) || (a.name||'').toLowerCase().includes(Q));
  }, [apps, q]);

  function doAction(action: (s: string, p: string) => Promise<string>, label: string) {
    if (!sel) return;
    action(device.id, sel.pkg)
      .then(o => showToast({title: label, body: o || sel.pkg, kind: 'ok', mono: true}))
      .catch(e => showToast({title: label + ' failed', body: String(e), kind: 'err'}));
  }

  return (
    <div className='screen'>
      <div className='screen-header'>
        <h1>Apps <span className='subtitle mono'>{filtered.length} of {apps.length}</span></h1>
        <SearchInput value={q} onChange={setQ} placeholder='Search package/name'/>
        <button className={`btn sm${onlyUser ? ' primary' : ''}`} onClick={() => setOnlyUser(true)}>User</button>
        <button className={`btn sm${!onlyUser ? ' primary' : ''}`} onClick={() => setOnlyUser(false)}>All</button>
        <div className='spacer' style={{flex: 1}}/>
        <DataAge fetchedAt={fetchedAt}/>
        <button className='btn' onClick={refresh}><Icon.Refresh className={refreshing ? 'spin' : ''}/>Reload</button>
        <button className='btn primary' title='Single .apk, or a split bundle as .apks / .xapk'
                onClick={() => pickApkAndInstall(device.id)}>
          <Icon.Upload/>Install APK / APKS
        </button>
      </div>
      <div className='apps-layout' style={{flex: 1, minHeight: 0}}>
        <div className='apps-list'>
          {loading && <div className='muted' style={{padding: 16}}>Loading…</div>}
          {filtered.map(a => (
            <div key={a.pkg} className={`app-row${sel?.pkg === a.pkg ? ' selected' : ''}`} onClick={() => setSel(a)}>
              <AppIconImg serial={device.id} pkg={a.pkg} name={a.name}/>
              <div style={{minWidth: 0}}>
                <div className='name'>{a.name || a.pkg}</div>
                <div className='pkg'>{a.pkg}</div>
              </div>
              <div className='meta'>{a.system ? <Badge>system</Badge> : <Badge kind='accent'>user</Badge>}</div>
            </div>
          ))}
        </div>
        <div className='app-detail'>
          {sel ? (
            <>
              <div style={{display: 'flex', gap: 12, marginBottom: 14}}>
                <AppIconImg serial={device.id} pkg={sel.pkg} name={sel.name} size={56} fontSize={22}/>
                <div style={{minWidth: 0}}>
                  <div style={{fontWeight: 600, fontSize: 15}}>{sel.name || sel.pkg}</div>
                  <div className='pkg mono'>{sel.pkg}</div>
                  <div style={{marginTop: 6, display: 'flex', gap: 6, flexWrap: 'wrap', alignItems: 'center'}}>
                    {detail?.v && <Badge>v{detail.v}</Badge>}
                    {sel.system ? <Badge>system</Badge> : <Badge kind='accent'>user</Badge>}
                    {detail?.uid && <Badge kind='info'>UID {detail.uid}</Badge>}
                    {running?.running
                      ? <Badge kind='ok'>running · pid {running.pid || '?'}</Badge>
                      : running && <Badge>stopped</Badge>}
                    <CommandChip label={sel.pkg} groups={[
                      {label: 'Launch', commands: cmds?.launch},
                      {label: 'Kill', commands: cmds?.forceStop},
                      {label: 'Clear data', commands: cmds?.clear},
                      {label: 'Export data', commands: cmds?.exportData},
                      {label: 'Uninstall', commands: cmds?.uninstall},
                    ]}/>
                  </div>
                </div>
              </div>
              <div style={{display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 6, marginTop: 14}}>
                {running?.running
                  ? <button className='btn danger' onClick={async () => {
                      if (!sel) return;
                      // Force-stop kills *all* of the app's processes — same affordance as
                      // Settings → App info → Force stop. Optimistic state flip so the
                      // button doesn't appear stuck; reconciled by the next probe tick.
                      setRunning({running: false, pid: 0});
                      try {
                        const out = await API.ForceStopApp(device.id, sel.pkg);
                        showToast({title: 'Killed', body: out || sel.pkg, kind: 'ok', mono: true, actions: commandToast(cmds?.forceStop)});
                      } catch (e) { showToast({title: 'Kill failed', body: String(e), kind: 'err'}); }
                      refreshRunning();
                    }}><Icon.Stop/>Kill{running.pid ? ` (pid ${running.pid})` : ''}</button>
                  : <button className='btn primary' onClick={async () => {
                      if (!sel) return;
                      setRunning({running: true, pid: 0});
                      try {
                        const out = await API.LaunchApp(device.id, sel.pkg);
                        showToast({title: 'Launch', body: out || sel.pkg, kind: 'ok', mono: true, actions: commandToast(cmds?.launch)});
                      } catch (e) { showToast({title: 'Launch failed', body: String(e), kind: 'err'}); }
                      // Apps take a moment to wire up their main process; give
                      // pidof time to see the PID before reconciling.
                      setTimeout(refreshRunning, 600);
                    }}><Icon.Play/>Launch</button>}
                <button className='btn' onClick={async () => {
                  if (!sel) return;
                  // "Restart" only shows when the app is running — kill then launch
                  // so the user can reproduce a cold-start without two clicks.
                  if (!running?.running) {
                    showToast({title: 'Restart skipped', body: 'app is not running', kind: 'info'});
                    return;
                  }
                  try {
                    await API.ForceStopApp(device.id, sel.pkg);
                    await new Promise(r => setTimeout(r, 250));
                    await API.LaunchApp(device.id, sel.pkg);
                    showToast({title: 'Restarted', body: sel.pkg, kind: 'ok', mono: true});
                  } catch (e) { showToast({title: 'Restart failed', body: String(e), kind: 'err'}); }
                  setTimeout(refreshRunning, 700);
                }} disabled={!running?.running}><Icon.Refresh/>Restart</button>
                <button className='btn' onClick={async () => {
                  if (!sel) return;
                  const ok = await confirmDialog({
                    title: `Clear data for ${sel.name || sel.pkg}?`,
                    body: <>
                      Wipes app preferences, cache, and database. The app will start fresh.
                      <CommandPreview commands={cmds?.clear ?? []} defaultOpen/>
                    </>,
                    confirmLabel: 'Clear', danger: true,
                  });
                  if (ok) doAction(API.ClearApp, 'Clear data');
                }}>Clear data</button>
                {setScreen && (
                  <button className='btn' onClick={() => {
                    if (!sel) return;
                    // Queue a `cd` into the app's data dir; the Shell screen
                    // opens a fresh session and types this on first paint.
                    // Root is required to enter /data/data/PKG on most builds,
                    // so we explicitly request root when the device supports it.
                    store.queueShellCmd({
                      serial: device.id,
                      cmd: `cd /data/data/${sel.pkg} && ls -la`,
                      root: !!device.root,
                    });
                    showToast({title: 'Opening shell', body: `cd /data/data/${sel.pkg}`, kind: 'info', mono: true});
                    setScreen('shell');
                  }}><Icon.Terminal/>Open shell</button>
                )}
                <button className='btn' onClick={() =>
                  API.ExportAppDataWithPicker(device.id, sel.pkg)
                    .then(() => showToast({title: 'Data export started', body: 'Watch the Tasks panel for progress', kind: 'info', actions: commandToast(cmds?.exportData)}))
                    .catch(e => showToast({title: 'Export failed', body: String(e), kind: 'err'}))
                } disabled={!device.root}>
                  <Icon.Download/>Export data{!device.root && ' (root)'}
                </button>
              </div>
              <ApkToolsSection device={device} pkg={sel.pkg}/>
              <FridaAppSection device={device} pkg={sel.pkg} running={!!running?.running} setScreen={setScreen}/>
              {detail && <>
                <DetailSection title='Versions' defaultOpen>
                  <Detail k='Version' v={detail.v ? `${detail.v}${detail.versionCode ? `  ·  code ${detail.versionCode}` : ''}` : detail.versionCode}/>
                  <Detail k='Target SDK' v={sdkLabel(detail.targetSdk)}/>
                  <Detail k='Min SDK'    v={sdkLabel(detail.minSdk)}/>
                  <Detail k='Compile SDK' v={sdkLabel(detail.compileSdk)}/>
                </DetailSection>
                <DetailSection title='State (user 0)'>
                  <Detail k='Enabled'    v={detail.enabled}/>
                  <Detail k='Installed'  v={detail.installed}/>
                  <Detail k='Stopped'    v={detail.stopped}/>
                  <Detail k='Suspended'  v={detail.suspended}/>
                  <Detail k='Not launched' v={detail.notLaunched}/>
                  <Detail k='Instant'    v={detail.instant}/>
                </DetailSection>
                <DetailSection title='Install'>
                  <Detail k='Installer'     v={detail.installer || (detail.system ? '—' : 'Sideload')}/>
                  <Detail k='First install' v={detail.firstInstall}/>
                  <Detail k='Last update'   v={detail.lastUpdate}/>
                  <Detail k='APK modified'  v={detail.timeStamp}/>
                  <Detail k='Install loc'   v={installLocLabel(detail.installLocation)}/>
                </DetailSection>
                <DetailSection title='Code'>
                  <Detail k='Code path'   v={detail.path} mono copy/>
                  <Detail k='Data dir'    v={detail.dataDir} mono copy/>
                  <Detail k='Native libs' v={detail.nativeLibraryDir} mono copy/>
                  <Detail k='Primary ABI' v={detail.primaryAbi}/>
                  <Detail k='Secondary ABI' v={detail.secondaryAbi}/>
                  {detail.splits && detail.splits.length > 0 && <SplitsRow splits={detail.splits}/>}
                  {detail.supportsScreens && detail.supportsScreens.length > 0 && <ScreensRow screens={detail.supportsScreens}/>}
                </DetailSection>
                {(detail.flags?.length || detail.privateFlags?.length) ? (
                  <DetailSection title='Flags'>
                    <FlagsRow flags={detail.flags || []} priv={detail.privateFlags || []}/>
                  </DetailSection>
                ) : null}
                <DetailSection title='Signature'>
                  <Detail k='Cert'           v={detail.signature} mono copy/>
                  <Detail k='Signing scheme' v={detail.apkSigningVersion ? `v${detail.apkSigningVersion}` : ''}/>
                </DetailSection>
                {detail.gids && detail.gids.length > 0 && (
                  <DetailSection title='Groups'>
                    <Detail k='GIDs' v={detail.gids.join(', ')} mono/>
                  </DetailSection>
                )}
              </>}
              <PermissionsPanel detail={detail}/>
              <button className='btn danger' style={{marginTop: 6, width: '100%'}}
                      onClick={async () => {
                        if (!sel) return;
                        const ok = await confirmDialog({
                          title: `Uninstall ${sel.name || sel.pkg}?`,
                          body: <>
                            <span className='mono'>{sel.pkg}</span>
                            <CommandPreview commands={cmds?.uninstall ?? []} defaultOpen/>
                          </>,
                          confirmLabel: 'Uninstall', danger: true,
                        });
                        if (ok) {
                          API.UninstallApp(device.id, sel.pkg)
                            .then(() => showToast({title: 'Uninstall started', body: 'See Tasks panel', kind: 'info'}));
                        }
                      }}>
                <Icon.Trash/>Uninstall
              </button>
            </>
          ) : <div className='muted' style={{padding: 20}}>Select an app</div>}
        </div>
      </div>
    </div>
  );
}

// FridaAppSection ties an app to its Frida script binding and the one-click
// "Start/Attach with Frida" orchestration. Bindings are package-keyed
// (device-independent), so what you attach here shows up on every device and in
// the Frida → App Scripts tab.
function FridaAppSection({device, pkg, running, setScreen}: {device: adb.Device; pkg: string; running: boolean; setScreen?: (s: string) => void}) {
  const store = useStore();
  const [binding, setBinding] = useState<adb.AppScripts | null>(null);
  const [manage, setManage] = useState(false);
  const [busy, setBusy] = useState('');

  const reload = () => { API.GetAppFridaScripts(pkg).then(setBinding).catch(() => setBinding(null)); };
  useEffect(() => { reload(); }, [pkg]);

  const count = binding?.scriptIds?.length || 0;

  // One button here can push a frida-server, make it executable and daemonize it
  // before the session even starts, so the commands behind that belong in reach
  // (CLAUDE.md §4.1). The session's own host-side command shows up on the
  // session, where its job file exists.
  const [fcmds, setFcmds] = useState<adb.FridaCommands | null>(null);
  useEffect(() => {
    if (!device?.id) { setFcmds(null); return; }
    let live = true;
    API.FridaCommands(device.id, '', 0)
      .then(c => { if (live) setFcmds(c); })
      .catch(() => { if (live) setFcmds(null); });
    return () => { live = false; };
  }, [device?.id]);

  async function launch(mode: 'spawn' | 'attach', restart: boolean) {
    setBusy(mode + (restart ? '-restart' : ''));
    try {
      if (restart && running) {
        await API.ForceStopApp(device.id, pkg);
        await new Promise(r => setTimeout(r, 300));
      }
      // StartAppWithFrida orchestrates the prerequisites backend-side (ensure a
      // frida-server is running, detect its exact version, build/resolve a
      // matching host runtime) and starts the session; we then adopt it into the
      // store so its log stream is subscribed + backfilled.
      const info = await API.StartAppWithFrida(device.id, pkg, mode);
      store.adoptFridaSession(info);
      showToast({title: 'Frida session started', body: `${mode} · ${pkg}`, kind: 'ok', mono: true});
      store.requestFridaTab('sessions');
      setScreen?.('frida');
    } catch (e) {
      showToast({title: 'Could not start Frida', body: String(e), kind: 'err'});
    } finally {
      setBusy('');
    }
  }

  return (
    <div className='card' style={{marginTop: 10}}>
      <div className='card-header'>
        <span className='title' style={{fontSize: 12, display: 'flex', alignItems: 'center', gap: 6}}><Icon.Zap width={13} height={13}/>Frida</span>
        {count > 0 && <Badge kind='accent'>{count} script{count === 1 ? '' : 's'}</Badge>}
        <div style={{flex: 1}}/>
        <CommandChip label='Frida prerequisites' groups={[
          {label: 'Install a server', commands: fcmds?.install, note: 'Only when the device has no matching frida-server yet.'},
          {label: 'Start it', commands: fcmds?.start},
          {label: 'Stop it', commands: fcmds?.stop},
        ]}/>
        <button className='btn sm' onClick={() => setManage(true)}>Manage scripts</button>
      </div>
      <div className='card-body'>
        {!device.root && (
          <div style={{fontSize: 11, color: 'var(--warn)', marginBottom: 8}}>
            Frida needs a rooted device running frida-server. {rootUnavailableReason(device)}
            {' '}On an unrooted device, use frida-gadget instead.
          </div>
        )}
        <div style={{display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 6}}>
          <button className='btn primary' disabled={!device.root || !!busy} onClick={() => launch('spawn', false)}>
            {busy === 'spawn' ? '…starting' : <><Icon.Play/>Start with Frida</>}
          </button>
          <button className='btn' disabled={!device.root || !!busy} onClick={() => launch('spawn', true)} title='Force-stop then spawn under Frida'>
            {busy === 'spawn-restart' ? '…restarting' : <><Icon.Refresh/>Restart with Frida</>}
          </button>
          <button className='btn' disabled={!device.root || !running || !!busy} onClick={() => launch('attach', false)} title={running ? 'Attach to the running process' : 'App is not running'}>
            {busy === 'attach' ? '…attaching' : <>Attach</>}
          </button>
          <div style={{fontSize: 11, color: 'var(--text-subtle)', alignSelf: 'center'}}>
            {count === 0 ? 'No scripts attached — runs bare.' : `${binding?.mode || 'spawn'} mode`}
          </div>
        </div>
      </div>
      {manage && <ManageFridaScriptsModal pkg={pkg} onClose={() => { setManage(false); reload(); }}/>}
    </div>
  );
}

// ManageFridaScriptsModal picks which library scripts attach to a package and the
// default launch mode. Persisted package-keyed (device-independent).
function ManageFridaScriptsModal({pkg, onClose}: {pkg: string; onClose: () => void}) {
  const [scripts, setScripts] = useState<adb.FridaScript[]>([]);
  const [sel, setSel] = useState<Record<string, boolean>>({});
  const [mode, setMode] = useState<'spawn' | 'attach'>('spawn');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    Promise.all([API.ListFridaScripts(), API.GetAppFridaScripts(pkg)])
      .then(([list, binding]) => {
        setScripts(list || []);
        const chosen: Record<string, boolean> = {};
        (binding?.scriptIds || []).forEach(id => { chosen[id] = true; });
        setSel(chosen);
        setMode((binding?.mode as 'spawn' | 'attach') || 'spawn');
      })
      .catch(e => showToast({title: 'Load failed', body: String(e), kind: 'err'}));
  }, [pkg]);

  function save() {
    const ids = scripts.filter(s => sel[s.id]).map(s => s.id);
    setSaving(true);
    API.SetAppFridaScripts(pkg, ids, mode, '')
      .then(() => { showToast({title: 'Saved', body: `${ids.length} script(s) for ${pkg}`, kind: 'ok'}); onClose(); })
      .catch(e => showToast({title: 'Save failed', body: String(e), kind: 'err'}))
      .finally(() => setSaving(false));
  }

  return (
    <Modal open onClose={onClose} title={`Frida scripts · ${pkg}`} width={560}
      footer={<><button className='btn' onClick={onClose}>Cancel</button><button className='btn primary' onClick={save} disabled={saving}>{saving ? '…saving' : 'Save'}</button></>}>
      <div style={{display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10}}>
        <span className='muted' style={{fontSize: 12}}>Launch mode:</span>
        <button className={`btn sm${mode === 'spawn' ? ' primary' : ''}`} onClick={() => setMode('spawn')}>spawn (cold start)</button>
        <button className={`btn sm${mode === 'attach' ? ' primary' : ''}`} onClick={() => setMode('attach')}>attach (running)</button>
      </div>
      {scripts.length === 0 ? (
        <div className='muted' style={{padding: 16, textAlign: 'center', fontSize: 12}}>
          No scripts in the library yet. Create or import some in Frida → Scripts.
        </div>
      ) : (
        <div style={{display: 'flex', flexDirection: 'column', gap: 4, maxHeight: 360, overflow: 'auto'}}>
          {scripts.map(s => (
            <label key={s.id} style={{display: 'flex', alignItems: 'center', gap: 8, padding: '6px 8px', borderRadius: 5, background: 'var(--bg-inset)', cursor: 'pointer'}}>
              <input type='checkbox' checked={!!sel[s.id]} onChange={e => setSel(p => ({...p, [s.id]: e.target.checked}))}/>
              <div style={{minWidth: 0, flex: 1}}>
                <div style={{fontSize: 12, fontWeight: 600}}>{s.name}</div>
                {s.description && <div className='subtle' style={{fontSize: 11, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'}}>{s.description}</div>}
              </div>
              {s.origin === 'codeshare' && !s.trusted && <Badge kind='warn'>untrusted</Badge>}
            </label>
          ))}
        </div>
      )}
    </Modal>
  );
}

// ─── APK: export, decompile, binaries ──────────────────────────────────
//
// One section, because all three actions work off the same thing: the APKs that
// make up the install. They were three separate blocks, each with its command
// listing open, which is most of what made this panel unreadable.
//
// App Bundle installs are several APKs (base + config splits), and that shapes
// every action here. Exporting only the base produces a file that fails to
// install later with INSTALL_FAILED_MISSING_SPLIT, so those become one .apks
// archive; the decompiler instead gets the parts, all at once, because the
// bundle is not an input it reads and a feature split's code would otherwise be
// missing.
//
// Installing lives in the screen header, not here: it targets the device, not
// the app you happen to have selected.
function ApkToolsSection({device, pkg}: {device: adb.Device; pkg: string}) {
  const [set, setSet] = useState<adb.ApkSet | null>(null);
  const [plan, setPlan] = useState<adb.JadxOpenPlan | null>(null);
  const [binPlan, setBinPlan] = useState<adb.BinaryPlan | null>(null);
  const [info, setInfo] = useState<adb.JadxInfo | null>(null);
  const [busy, setBusy] = useState('');
  const [err, setErr] = useState('');

  useEffect(() => {
    let live = true;
    setSet(null); setPlan(null); setBinPlan(null); setErr('');
    API.ApkSetOf(device.id, pkg)
      .then(s => { if (live) setSet(s); })
      .catch(e => { if (live) setErr(String(e)); });
    API.PlanJadxOpen(device.id, pkg)
      .then(p => { if (live) setPlan(p); })
      .catch(() => {});
    API.PlanAppBinaries(device.id, pkg)
      .then(p => { if (live) setBinPlan(p); })
      .catch(() => {});
    jadxInfo()
      .then(i => { if (live) setInfo(i); })
      .catch(() => {});
    return () => { live = false; };
  }, [device.id, pkg]);

  // Go marshals a nil slice as JSON null, so never read .length off these directly.
  const splits = set?.splits ?? [];
  const split = !!set?.split;
  const count = set ? splits.length + 1 : 0;

  const exportApks = () => {
    setBusy('apk');
    API.ExportApks(device.id, pkg)
      .then(id => id && showToast({title: 'Export started', body: 'Watch the Tasks panel for progress', kind: 'info'}))
      .catch(e => showToast({title: 'Export failed', body: String(e), kind: 'err'}))
      .finally(() => setBusy(''));
  };

  const openJadx = async () => {
    setBusy('jadx');
    try {
      const ready = await ensureJadx();
      if (!ready) return;
      setInfo(ready);
      const id = await API.OpenInJadx(device.id, pkg);
      if (id) {
        showToast({
          title: 'Opening in jadx',
          body: plan?.staged
            ? 'The APKs are already on this computer'
            : 'Copying the APKs first — watch the Tasks panel',
          kind: 'info',
        });
      }
    } catch (e) {
      showToast({title: 'Could not open jadx', body: String(e), kind: 'err'});
    } finally {
      setBusy('');
      API.PlanJadxOpen(device.id, pkg).then(setPlan).catch(() => {});
    }
  };

  const exportBinaries = () => {
    setBusy('bin');
    API.ExportAppBinaries(device.id, pkg)
      .then(id => id && showToast({title: 'Collecting binaries', body: 'Watch the Tasks panel for progress', kind: 'info'}))
      .catch(e => showToast({title: 'Collection failed', body: String(e), kind: 'err'}))
      .finally(() => setBusy(''));
  };

  return (
    <div className='app-detail-section'>
      <div className='app-detail-section-title' style={{display: 'flex', alignItems: 'center', gap: 8}}>
        APK
        <div style={{flex: 1}}/>
        <CommandChip label='APK' groups={[
          {label: split ? 'Export .apks' : 'Export .apk', commands: set?.commands},
          {label: 'Open in jadx', commands: plan?.commands},
          {label: 'Download binaries', commands: binPlan?.commands},
        ]}/>
      </div>
      {err && <div className='muted' style={{fontSize: 12, color: 'var(--danger)'}}>Could not read the APK layout: {err}</div>}
      {set && (
        <>
          <div className='app-detail-row'>
            <span className='app-detail-k'>Layout</span>
            <span className='app-detail-v'>
              {split ? <>App Bundle · base + {splits.length} split APK(s)</> : <>Single APK</>}
              {plan?.staged ? ' · copied here' : ''}
            </span>
          </div>
          <div className='app-detail-row'>
            <span className='app-detail-k'>File</span>
            <span className='app-detail-v mono'>{set.suggested}</span>
          </div>

          <button className='btn' style={{width: '100%', marginTop: 8}} disabled={busy !== ''} onClick={exportApks}>
            <Icon.Download/>{busy === 'apk' ? '…exporting' : `Export ${split ? '.apks' : '.apk'}`}
          </button>
          {split && (
            <div className='muted' style={{fontSize: 11, marginTop: 6}}>
              All {count} APKs go into one archive. Installing only the base would fail with INSTALL_FAILED_MISSING_SPLIT.
            </div>
          )}

          <button className='btn' style={{width: '100%', marginTop: 12}} disabled={busy !== ''} onClick={openJadx}>
            <Icon.Layers/>{busy === 'jadx' ? '…opening' : 'Open in jadx'}
          </button>
          <div className='muted' style={{fontSize: 11, marginTop: 6}}>
            {jadxLabel(info)}
            {info?.installed && !info.java && <> · <span style={{color: 'var(--danger)'}}>no Java runtime</span></>}
            {split && <> · all {count} APKs open in one session</>}
          </div>

          <button className='btn' style={{width: '100%', marginTop: 12}} disabled={busy !== ''} onClick={exportBinaries}>
            <Icon.Cpu/>{busy === 'bin' ? '…collecting' : 'Download binaries'}
          </button>
          <div className='muted' style={{fontSize: 11, marginTop: 6}}>
            Native libraries, shipped executables and the runtime blobs beside them, in one zip.
          </div>
        </>
      )}
    </div>
  );
}

function DetailSection({title, children, defaultOpen}: {title: string; children: React.ReactNode; defaultOpen?: boolean}) {
  // Sections only render when they contain non-empty Detail rows, so we peek
  // at children and bail when everything resolved to null. Without this the
  // headings would dangle over empty space for system apps that don't expose
  // e.g. compileSdk or signatures.
  const arr = React.Children.toArray(children).filter(c => c !== null && c !== undefined);
  const [open, setOpen] = useSectionOpen(title, !!defaultOpen);
  if (arr.length === 0) return null;
  return (
    <div className='app-detail-section'>
      <div className='app-detail-section-title' onClick={() => setOpen(!open)}
           style={{cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 6, userSelect: 'none'}}>
        <span style={{display: 'inline-block', transition: 'transform .12s', transform: open ? 'rotate(90deg)' : 'none', opacity: .7}}>›</span>
        {title}
      </div>
      {open && children}
    </div>
  );
}

// useSectionOpen persists a section's state per title, so the panel comes back
// the way the user left it instead of re-collapsing on every app they click.
function useSectionOpen(key: string, initial: boolean): [boolean, (v: boolean) => void] {
  const storeKey = 'adbq.appdetail.' + key;
  const [open, setOpen] = useState(() => {
    try {
      const v = localStorage.getItem(storeKey);
      return v === null ? initial : v === '1';
    } catch {
      return initial;
    }
  });
  const set = (v: boolean) => {
    setOpen(v);
    try { localStorage.setItem(storeKey, v ? '1' : '0'); } catch { /* private mode: state is per-session */ }
  };
  return [open, set];
}

function Detail({k, v, mono, copy}: {k: string; v?: string; mono?: boolean; copy?: boolean}) {
  if (!v) return null;
  return (
    <div className='app-detail-row'>
      <span className='app-detail-k'>{k}</span>
      <span className={`app-detail-v${mono ? ' mono' : ''}`}>
        {v}
        {copy && (
          <button className='app-detail-copy' title='Copy' onClick={() => { void navigator.clipboard?.writeText(v); showToast({title: 'Copied', body: v, kind: 'ok', mono: true}); }}>
            <Icon.Clipboard/>
          </button>
        )}
      </span>
    </div>
  );
}

function installLocLabel(loc?: string): string | undefined {
  if (!loc) return undefined;
  switch (loc) {
    case '0': return 'Auto';
    case '1': return 'Internal';
    case '2': return 'External (SD)';
    default:  return loc;
  }
}

function SplitsRow({splits}: {splits: string[]}) {
  const [open, setOpen] = useState(false);
  const head = splits.slice(0, 3);
  const more = splits.length - head.length;
  return (
    <div className='app-detail-row'>
      <span className='app-detail-k'>Splits</span>
      <span className='app-detail-v'>
        <div style={{display: 'flex', flexWrap: 'wrap', gap: 4}}>
          {(open ? splits : head).map(s => <Badge key={s}>{s}</Badge>)}
          {!open && more > 0 && <button className='btn sm' onClick={() => setOpen(true)}>+{more} more</button>}
        </div>
      </span>
    </div>
  );
}

function ScreensRow({screens}: {screens: string[]}) {
  return (
    <div className='app-detail-row'>
      <span className='app-detail-k'>Screens</span>
      <span className='app-detail-v' style={{display: 'flex', flexWrap: 'wrap', gap: 4}}>
        {screens.map(s => <Badge key={s}>{s}</Badge>)}
      </span>
    </div>
  );
}

function FlagsRow({flags, priv}: {flags: string[]; priv: string[]}) {
  const all = [
    ...flags.map(f => ({name: f, kind: flagKind(f), priv: false})),
    ...priv.map(f => ({name: f, kind: flagKind(f), priv: true})),
  ];
  if (all.length === 0) return null;
  return (
    <div className='app-detail-row'>
      <span className='app-detail-k'>Flags</span>
      <span className='app-detail-v' style={{display: 'flex', flexWrap: 'wrap', gap: 4}}>
        {all.map(f => <Badge key={(f.priv ? 'p:' : '') + f.name} kind={f.kind}>{f.name}</Badge>)}
      </span>
    </div>
  );
}

function flagKind(f: string): 'warn' | 'err' | 'info' | undefined {
  if (f === 'DEBUGGABLE' || f === 'TEST_ONLY') return 'warn';
  if (f === 'ALLOW_BACKUP' || f === 'ALLOW_CLEAR_USER_DATA') return 'info';
  if (f === 'SYSTEM' || f === 'PRIVILEGED') return 'info';
  return undefined;
}

function PermissionsPanel({detail}: {detail: adb.AppDetail | null}) {
  const [show, setShow] = useState<'requested' | 'granted'>('granted');
  if (!detail) return null;
  const granted = detail.grantedPerms || [];
  const requested = detail.requestedPerms || [];
  if (granted.length === 0 && requested.length === 0) return null;
  const list = show === 'granted' ? granted : requested.map(name => ({name, granted: false}));
  return (
    <div style={{marginTop: 14}}>
      <div className='spread' style={{marginBottom: 6}}>
        <span className='muted' style={{fontSize: 12, fontWeight: 500}}>Permissions ({granted.length} granted · {requested.length} requested)</span>
        <div style={{display: 'flex', gap: 4}}>
          <button className={`btn sm${show === 'granted' ? ' primary' : ''}`} onClick={() => setShow('granted')}>granted</button>
          <button className={`btn sm${show === 'requested' ? ' primary' : ''}`} onClick={() => setShow('requested')}>requested</button>
        </div>
      </div>
      <div style={{maxHeight: 280, overflow: 'auto', display: 'flex', flexDirection: 'column', gap: 3, paddingRight: 4}}>
        {list.length === 0 && <div className='muted' style={{fontSize: 11, padding: 8}}>None.</div>}
        {list.map(p => {
          const short = p.name.replace(/^android\.permission\./, '');
          const isDangerous = DANGEROUS.has(short);
          return (
            <div key={p.name} style={{
              display: 'flex', alignItems: 'center', gap: 8, fontSize: 11,
              padding: '5px 8px', borderRadius: 5,
              background: 'var(--bg-inset)',
              borderLeft: `3px solid ${p.granted ? 'var(--ok)' : isDangerous ? 'var(--err)' : 'var(--border-strong)'}`,
            }}>
              <span style={{flex: 1, fontFamily: 'var(--font-mono)', wordBreak: 'break-all'}}>{short}</span>
              {isDangerous && !p.granted && <Badge kind='warn'>dangerous</Badge>}
              {p.granted ? <Badge kind='ok'>granted</Badge> : show === 'granted' ? <Badge>denied</Badge> : null}
            </div>
          );
        })}
      </div>
    </div>
  );
}

const DANGEROUS = new Set([
  'CAMERA', 'RECORD_AUDIO', 'READ_CONTACTS', 'WRITE_CONTACTS', 'READ_CALENDAR',
  'WRITE_CALENDAR', 'READ_SMS', 'SEND_SMS', 'RECEIVE_SMS', 'READ_CALL_LOG',
  'WRITE_CALL_LOG', 'CALL_PHONE', 'READ_PHONE_STATE', 'ACCESS_FINE_LOCATION',
  'ACCESS_COARSE_LOCATION', 'ACCESS_BACKGROUND_LOCATION', 'READ_EXTERNAL_STORAGE',
  'WRITE_EXTERNAL_STORAGE', 'MANAGE_EXTERNAL_STORAGE', 'BODY_SENSORS',
  'BLUETOOTH_CONNECT', 'BLUETOOTH_SCAN', 'POST_NOTIFICATIONS', 'SYSTEM_ALERT_WINDOW',
]);

// AppIconImg lazily fetches the real app icon (from APK via aapt2 or zip
// scan, cached server-side). Falls back to a colored letter tile when no
// icon can be extracted or while loading.
const iconCache = new Map<string, string>(); // pkg → data uri (or '' for none)

function AppIconImg({serial, pkg, name, size = 36, fontSize = 14}: {serial: string; pkg: string; name?: string; size?: number; fontSize?: number}) {
  const cached = iconCache.get(pkg);
  const [src, setSrc] = useState<string>(cached ?? '');
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (cached !== undefined) return;
    let cancelled = false;
    const io = new IntersectionObserver((entries) => {
      if (entries.some(e => e.isIntersecting)) {
        io.disconnect();
        API.AppIcon(serial, pkg).then(d => {
          if (cancelled) return;
          iconCache.set(pkg, d || '');
          setSrc(d || '');
        }).catch(() => { if (!cancelled) iconCache.set(pkg, ''); });
      }
    }, {rootMargin: '200px'});
    if (ref.current) io.observe(ref.current);
    return () => { cancelled = true; io.disconnect(); };
  }, [serial, pkg]);
  if (src) {
    return <img src={src} alt={name || pkg} width={size} height={size} style={{borderRadius: Math.round(size * 0.25), background: 'var(--bg-inset)'}} loading='lazy'/>;
  }
  return (
    <div ref={ref} className='app-icon' style={{width: size, height: size, fontSize, background: hashColor(pkg)}}>
      {(name || pkg)[0].toUpperCase()}
    </div>
  );
}

function hashColor(s: string) {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0;
  const colors = ['#4285F4', '#a07cf7', '#1ed760', '#25D366', '#611f69', '#34a853', '#5865F2', '#ff7139', '#e1306c', '#5e6ad2'];
  return colors[Math.abs(h) % colors.length];
}
