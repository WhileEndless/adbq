import React, {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import {adb} from '../wailsjs/go/models';
import * as API from '../wailsjs/go/main/App';
import {Icon} from './icons';
import {Badge, CodeBlock, CommandPreview, ConfirmHost, commandToast, IconBtn, Modal, PromptHost, ToastHost, confirmDialog, showToast, promptDialog, useTheme, ThemeMode} from './ui';
import {invalidateJadxInfo, jadxInfo} from './lib/jadx';
import {EventsOn} from '../wailsjs/runtime/runtime';
import {StoreProvider} from './store';
import {logcatStore} from './logcatStore';
import {TasksTray} from './tasks';
import {Screen} from './types';
import markUrl from './assets/brand-mark.png';
import {OverviewScreen} from './screens/Overview';
import {LogcatScreen} from './screens/Logcat';
import {ShellScreen} from './screens/Shell';
import {AppsScreen} from './screens/Apps';
import {FilesScreen} from './screens/Files';
import {ForwardsScreen} from './screens/Forwards';
import {FridaScreen} from './screens/Frida';
import {NetworkScreen} from './screens/Network';
import {CaptureScreen} from './screens/Capture';
import {IptablesScreen} from './screens/Iptables';
import {ProcessesScreen} from './screens/Processes';
import {EmulatorsScreen} from './screens/Emulators';
import {ProfileSelector, ProfileEditor, ApplyConfirm, PastDevices, deviceKey} from './screens/Profiles';
import {deviceKey as cacheKey, getCached, prefetchData, useDeviceData} from './cache';
import {useScrcpyActive, useScrcpyAvailable} from './lib/scrcpy';
import {usePoll} from './lib/poll';

// prefetchDeviceData warms the shared cache when a device appears online, so
// opening a screen is instant even the first time. Keys/shapes MUST match what
// each screen reads via useDeviceData.
//
// Kept deliberately small. Warming everything meant a burst of roughly thirty
// `adb shell` processes the moment a device was plugged in, all at once — and
// adb serialises badly under that load: forty concurrent shells against a
// physical device took over three minutes to drain, against ~55ms each when issued
// one at a time. A plug-in storm is not free just because it is short; it is
// the worst possible moment to saturate the transport, because that is when the
// user is waiting for the first screen to paint.
//
// So only the two cheap reads are warmed. GetNetworkInfo (five to seven round
// trips) and the iptables probe are left to their screens, which now cache the
// results for minutes anyway.
function prefetchDeviceData(d: adb.Device) {
  const id = d.id;
  prefetchData(cacheKey('forwards', id), async () => {
    const [f, r] = await Promise.all([API.ListForwards(id), API.ListReverses(id)]);
    return {fwd: f || [], rev: r || []};
  });
  prefetchData(cacheKey('storage', id, 'stats'), () => API.GetStats(id));
}

// The installed package list changes only when adbq installs or uninstalls
// something — and it invalidates the cache when it does (see app_invalidate.go),
// so this is a backstop for changes made outside adbq rather than the mechanism
// that keeps the list correct. `pm list packages` is slow enough that a short
// TTL here was costing a device call per screen change for nothing.
export const APPS_STALE_MS = 10 * 60_000;

const ACCENTS = ['#a07cf7', '#7aa2ff', '#5ed29a', '#e9b454', '#ec6a73', '#c5a3ff'];

// ErrorBoundary keeps a render crash from blanking the whole window (a black
// screen with no recourse). It shows the error and a Reload affordance instead.
// The JS stack is minified in a release build and names nothing useful, so we
// keep React's component stack too — that one survives minification and points
// straight at the component that threw.
class ErrorBoundary extends React.Component<{children: React.ReactNode}, {error: Error | null; where: string}> {
  constructor(props: {children: React.ReactNode}) { super(props); this.state = {error: null, where: ''}; }
  static getDerivedStateFromError(error: Error) { return {error}; }
  componentDidCatch(error: Error, info: React.ErrorInfo) {
    console.error('UI crash:', error, info);
    this.setState({where: info.componentStack || ''});
  }
  render() {
    if (this.state.error) {
      const err = this.state.error;
      const report = [
        String(err?.message || err),
        '',
        String(err?.stack || ''),
        '',
        'Component stack:' + (this.state.where || ' (unavailable)'),
      ].join('\n');
      // The first "at X" line of the component stack is the failing component.
      const culprit = (this.state.where.match(/\s*at\s+(\w+)/) || [])[1];
      return (
        <div style={{padding: 40, maxWidth: 640, color: 'var(--text)'}}>
          <h2 style={{marginTop: 0}}>Something went wrong{culprit ? ` in ${culprit}` : ''}</h2>
          <pre style={{whiteSpace: 'pre-wrap', wordBreak: 'break-word', color: 'var(--err)', fontSize: 12, background: 'var(--bg-inset)', padding: 12, borderRadius: 6, maxHeight: 260, overflow: 'auto'}}>
            {report}
          </pre>
          <div style={{display: 'flex', gap: 8, marginTop: 14}}>
            <button className='btn primary' onClick={() => window.location.reload()}>Reload</button>
            <button className='btn' onClick={() => navigator.clipboard?.writeText(report)}>Copy report</button>
            <button className='btn' onClick={() => this.setState({error: null, where: ''})}>Dismiss</button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

export default function App() {
  return (
    <StoreProvider>
      <ErrorBoundary>
        <AppInner/>
      </ErrorBoundary>
    </StoreProvider>
  );
}

// Screens that describe this computer rather than an attached device. They stay
// reachable with nothing plugged in — otherwise the one screen that can start an
// emulator would be hidden behind having a device already.
const HOST_SCREENS: Screen[] = ['emulators'];

function AppInner() {
  const {mode: themeMode, setMode: setTheme, theme, accent, setAccent} = useTheme();
  const [devices, setDevices] = useState<adb.Device[]>([]);
  const [activeId, setActiveId] = useState<string>('');
  const [screen, setScreen] = useState<Screen>('overview');
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [connectOpen, setConnectOpen] = useState(false);
  const [connectAddr, setConnectAddr] = useState('192.168.1.10:5555');

  // ─── Device profiles ──────────────────────────────────────────────────────
  const [profilesVersion, setProfilesVersion] = useState(0);
  const [editor, setEditor] = useState<{profile: adb.Profile | null} | null>(null);
  const [pastOpen, setPastOpen] = useState(false);
  // FIFO queue of pending apply prompts; the head is the one shown. A queue (vs
  // a single value) prevents two devices reconnecting at once from clobbering
  // each other's prompt.
  const [applyQueue, setApplyQueue] = useState<{serial: string; profileId: string; key: string}[]>([]);
  const apply = applyQueue[0] || null;
  const prevOnline = useRef<Record<string, boolean>>({});
  const knownIds = useRef<string[]>([]);
  const pendingApply = useRef<Set<string>>(new Set());

  const bump = () => setProfilesVersion(v => v + 1);
  const enqueueApply = useCallback((serial: string, profileId: string, key: string) => {
    setApplyQueue(q => q.some(e => e.key === key) ? q : [...q, {serial, profileId, key}]);
  }, []);
  const dequeueApply = useCallback(() => {
    setApplyQueue(q => {
      if (q.length) pendingApply.current.delete(q[0].key);
      return q.slice(1);
    });
  }, []);
  const switchProfile = useCallback((serial: string, profileId: string) => {
    const dev = devices.find(d => d.id === serial);
    const key = deviceKey(dev) || serial;
    API.BindDeviceProfile(serial, profileId).then(bump).catch(() => {});
    enqueueApply(serial, profileId, key);
  }, [devices, enqueueApply]);
  const captureProfile = useCallback((serial: string, suggested: string) => {
    promptDialog({title: 'Capture profile from device', label: 'Profile name', defaultValue: suggested})
      .then(name => {
        if (!name) return;
        API.CaptureProfileFromDevice(serial, name)
          .then(p => { bump(); setEditor({profile: p}); showToast({title: 'Captured current settings', kind: 'ok'}); })
          .catch(e => showToast({title: 'Capture failed', body: String(e), kind: 'err'}));
      });
  }, []);

  // applyDevices installs a device list, whatever produced it.
  const applyDevices = useCallback((devs: adb.Device[] | null) => {
    const list = devs || [];
    // Free the logcat buffer and the backend feed of any device that has gone
    // away. Otherwise an unplugged phone keeps an adb process and a periodic
    // on-device poll alive, plus its ring buffer, for the rest of the session.
    // Tracked in a ref rather than inside the state updater, which React may
    // run more than once.
    for (const id of knownIds.current) {
      if (!list.some(d => d.id === id)) logcatStore.release(id);
    }
    knownIds.current = list.map(d => d.id);
    setDevices(list);
    setActiveId(prev => prev && list.some(d => d.id === prev) ? prev : (list[0]?.id || ''));
  }, []);

  const reload = useCallback(() => {
    API.ListDevices()
      .then(applyDevices)
      .catch(e => {
        // Don't spam the user with toasts when the device transiently goes
        // offline (usb replug, screen sleep). Just leave the list as-is.
        const msg = String(e);
        if (!msg.includes('device offline') && !msg.includes('not found')) {
          // eslint-disable-next-line no-console
          console.warn('ListDevices error:', msg);
        }
      });
  }, [applyDevices]);

  // Global handler for stray API rejections — keeps the UI alive instead of
  // bubbling to the React error boundary when a device drops.
  useEffect(() => {
    const onRej = (e: PromiseRejectionEvent) => {
      const msg = String(e.reason || '');
      // Only swallow adb *transport* drops so the UI stays alive when a device
      // disconnects. A bare "not found" must NOT be swallowed — that hides real
      // feature errors like "iptables not found" that screens should surface.
      if (msg.includes('device offline') || /device( '[^']*')? not found/.test(msg) || msg.includes('connection closed')) {
        e.preventDefault();
      }
    };
    window.addEventListener('unhandledrejection', onRej);
    return () => window.removeEventListener('unhandledrejection', onRej);
  }, []);
  // The device list is pushed, not polled. The adb server tells the backend the
  // instant a transport appears or goes away, and the backend forwards that
  // here — so a plugged-in phone shows up immediately instead of up to five
  // seconds later, and an idle app asks adb nothing at all.
  //
  // No timer here as a safety net: the backend owns the fallback (it polls when
  // the push subscription is down) and emits the same event either way. Two
  // independent fallbacks would double the work in exactly the degraded case
  // they exist for.
  useEffect(() => {
    reload();
    return EventsOn('devices:changed', (devs: adb.Device[]) => applyDevices(devs));
  }, [reload, applyDevices]);

  // Profile auto-apply: whenever a device becomes available (plugged in,
  // reconnected, or already connected when the app opened), if it has a bound
  // profile, prompt to apply it (confirm-first — never silent, always
  // dismissable). `was !== true` = "wasn't already known-online", covering a
  // freshly plugged device (first appearance `undefined`) as well as a
  // reconnect (`false`); the steady state (`true`) is left alone.
  useEffect(() => {
    for (const d of devices) {
      const key = deviceKey(d);
      const was = prevOnline.current[key];
      // Record the device only on first sight or an online-state change, not on
      // every 5s poll (each call writes devices.json).
      if (was === undefined || was !== d.online) {
        API.RegisterDevice(d).catch(() => {});
      }
      if (d.online && was !== true) {
        // Warm the cacheable screens for an instant open later.
        prefetchDeviceData(d);
        // Prompt to apply the bound profile, if any.
        if (!pendingApply.current.has(key)) {
          pendingApply.current.add(key);
          API.LookupDeviceProfile(key)
            .then(pid => {
              if (pid) enqueueApply(d.id, pid, key);
              else pendingApply.current.delete(key);
            })
            .catch(() => pendingApply.current.delete(key));
        }
      }
      prevOnline.current[key] = d.online;
    }
  }, [devices, enqueueApply]);

  const device = useMemo(() => devices.find(d => d.id === activeId) || devices[0], [devices, activeId]);

  // Sidebar badge counts. These come out of the shared cache rather than their
  // own fetches: the previous version re-ran ListApps and ListForwards on every
  // screen change, uncached, for two numbers — and `pm list packages` is one of
  // the slowest calls adbq makes. The Apps and Forwards screens already populate
  // these exact keys, and the backend invalidates them on install/uninstall and
  // on forward changes, so the badges stay correct without asking again.
  const appsCount = useDeviceData(
    device?.id ? cacheKey('apps', device.id, 'user') : null,
    () => API.ListApps(device!.id, true),
    {staleMs: APPS_STALE_MS},
  ).data?.length ?? 0;
  const forwardsData = getCached<{fwd: unknown[]; rev: unknown[]}>(
    device?.id ? cacheKey('forwards', device.id) : '');
  const forwardsCount = (forwardsData?.fwd?.length ?? 0) + (forwardsData?.rev?.length ?? 0);

  // Global keyboard shortcuts: Cmd/Ctrl+1..9 jump to the device screen at that
  // index, and Cmd/Ctrl+0 to Emulators — the host screen sits outside the
  // numbering, the way a browser's 0 means "the last one" rather than the
  // tenth. Numeric keys are ignored when a text input has focus so they don't
  // hijack typing.
  useEffect(() => {
    const order: Screen[] = [
      'overview', 'logcat', 'shell', 'processes', 'apps',
      'files', 'frida', 'forwards', 'network',
    ];
    const onKey = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || e.altKey || e.shiftKey) return;
      if (e.key === '0') {
        e.preventDefault();
        setScreen('emulators');
        return;
      }
      const n = parseInt(e.key, 10);
      if (Number.isNaN(n) || n < 1 || n > order.length) return;
      const target = order[n - 1];
      if (!target) return;
      e.preventDefault();
      setScreen(target);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const ScreenComp = {
    overview:   OverviewScreen,
    logcat:     LogcatScreen,
    shell:      ShellScreen,
    apps:       AppsScreen,
    files:      FilesScreen,
    forwards:   ForwardsScreen,
    frida:      FridaScreen,
    network:    NetworkScreen,
    capture:    CaptureScreen,
    iptables:   IptablesScreen,
    processes:  ProcessesScreen,
    emulators:  EmulatorsScreen,
  }[screen];

  return (
    <div className='app'>
      <Titlebar theme={theme} setTheme={setTheme} onOpenSettings={() => setSettingsOpen(true)} themeMode={themeMode}
                profileSelector={
                  <ProfileSelector device={device} refreshKey={profilesVersion}
                                   onSwitch={switchProfile}
                                   onEdit={p => setEditor({profile: p})}
                                   onNew={() => setEditor({profile: null})}
                                   onCapture={() => device && captureProfile(device.id, (device.model || 'Device') + ' profile')}
                                   onManage={() => setPastOpen(true)}/>
                }/>
      <DeviceTabs devices={devices} activeId={activeId} onSelect={setActiveId}
                  onAdd={() => setConnectOpen(true)}
                  onClose={(id) => API.DisconnectDevice(id).then(async () => {
                    // Only a network device has something to disconnect; a USB
                    // one just drops out of the list, and claiming a command ran
                    // would be a small lie.
                    if (id.includes(':')) {
                      const c = await API.ConnectCommands(id).catch(() => null);
                      showToast({title: 'Disconnected', body: id, kind: 'ok', mono: true, actions: commandToast(c?.disconnect)});
                    }
                    reload();
                  })}/>
      <Sidebar device={device} screen={screen} setScreen={setScreen} counts={{apps: appsCount, forwards: forwardsCount}}/>
      <main className='main'>
        {device || HOST_SCREENS.includes(screen)
          ? <ScreenComp device={device as adb.Device} setScreen={setScreen as any}/>
          : <div style={{padding: 40, textAlign: 'center'}}>
              <img src={markUrl} alt='' width={72} height={72} style={{opacity: .9, marginBottom: 12}}/>
              <div style={{fontSize: 16, marginBottom: 6}}>No devices connected</div>
              <div className='muted' style={{marginBottom: 14}}>Plug in a USB device, connect over Wi-Fi, or start an emulator.</div>
              <div style={{display: 'flex', gap: 6, justifyContent: 'center'}}>
                <button className='btn primary' onClick={() => setConnectOpen(true)}><Icon.Plus/>Connect</button>
                <button className='btn' onClick={() => setScreen('emulators')}><Icon.Phone/>Emulators</button>
              </div>
            </div>}
      </main>

      <Settings open={settingsOpen} onClose={() => setSettingsOpen(false)}
                themeMode={themeMode} setTheme={setTheme} accent={accent} setAccent={setAccent}/>

      <Modal open={connectOpen} onClose={() => setConnectOpen(false)} title='Connect over Wi-Fi'
             footer={<>
               <button className='btn' onClick={() => setConnectOpen(false)}>Cancel</button>
               <button className='btn primary' onClick={() =>
                 API.ConnectTCP(connectAddr).then(o => { showToast({title: 'adb connect', body: o, kind: 'ok', mono: true}); setConnectOpen(false); reload(); })
                   .catch(e => showToast({title: 'Connect failed', body: String(e), kind: 'err'}))}>
                 Connect
               </button>
             </>}>
        <div className='field'>
          <label>Address</label>
          <input className='input mono' value={connectAddr} onChange={e => setConnectAddr(e.target.value)} placeholder='192.168.1.10:5555'/>
          <ConnectCommand addr={connectAddr}/>
        </div>
        <div className='muted' style={{marginTop: 10, fontSize: 11}}>
          Device must already be paired (developer options → Wireless debugging) or have ADB over network enabled.
        </div>
      </Modal>

      {editor && (
        <ProfileEditor initial={editor.profile} device={device}
                       onClose={() => setEditor(null)}
                       onSaved={bump}/>
      )}
      {apply && (
        <ApplyConfirm key={apply.key} serial={apply.serial} profileId={apply.profileId}
                      reload={reload}
                      onApplied={bump}
                      onClose={dequeueApply}/>
      )}
      {pastOpen && (
        <PastDevices onClose={() => setPastOpen(false)}
                     onApply={(serial, profileId) => {
                       const k = deviceKey(devices.find(d => d.id === serial)) || serial;
                       enqueueApply(serial, profileId, k);
                     }}/>
      )}

      <ToastHost/>
      <ConfirmHost/>
      <PromptHost/>
      <TasksTray/>
    </div>
  );
}

function Titlebar({theme, setTheme, themeMode, onOpenSettings, profileSelector}: {theme: string; themeMode: ThemeMode; setTheme: (t: ThemeMode) => void; onOpenSettings: () => void; profileSelector?: React.ReactNode}) {
  const [v, setV] = useState('');
  useEffect(() => { API.Version().then(setV).catch(() => {}); }, []);
  return (
    <div className='titlebar'>
      <div className='brand'><img src={markUrl} alt='' width={18} height={18} className='brand-mark'/>adbq</div>
      <span className='meta-line muted'>ADB Manager</span>
      {v && <span className='mono subtle' style={{fontSize: 10.5, marginLeft: 6}}>{v}</span>}
      <div className='titlebar-spacer'/>
      {profileSelector}
      <IconBtn title={`Theme: ${themeMode}`} onClick={() => setTheme(themeMode === 'dark' ? 'light' : themeMode === 'light' ? 'system' : 'dark')}>
        {theme === 'dark' ? <Icon.Moon width={14} height={14}/> : <Icon.Sun width={14} height={14}/>}
      </IconBtn>
      <IconBtn title='Settings' onClick={onOpenSettings}><Icon.Settings width={14} height={14}/></IconBtn>
    </div>
  );
}

function ScrcpyButton({serial}: {serial: string}) {
  const active = useScrcpyActive(serial);
  const available = useScrcpyAvailable();
  if (!available) return null;
  return (
    <button className={`iconbtn${active ? ' active' : ''}`} title={active ? 'Stop scrcpy mirror' : 'Mirror screen with scrcpy'}
            onClick={(e) => {
              e.stopPropagation();
              if (active) { API.StopScrcpy(serial).catch(() => {}); }
              else        { API.StartScrcpy(serial).catch(err => showToast({title: 'scrcpy failed', body: String(err), kind: 'err'})); }
            }}>
      <Icon.Monitor width={13} height={13}/>
    </button>
  );
}

function DeviceTabs({devices, activeId, onSelect, onClose, onAdd}:{devices: adb.Device[]; activeId: string; onSelect:(id:string)=>void; onClose:(id:string)=>void; onAdd:()=>void}) {
  return (
    <div className='devicetabs'>
      {devices.map(d => (
        <div key={d.id} className={`devicetab${d.id === activeId ? ' active' : ''}`} onClick={() => onSelect(d.id)}>
          <span className={`led ${d.online ? 'online' : d.state === 'unauthorized' ? 'unauth' : 'offline'}`}/>
          <span className='col'>
            <div className='name'>{d.label || d.model || d.id}</div>
            <div className='sub'>{d.via} · {d.id}</div>
          </span>
          {d.online && d.id === activeId && <ScrcpyButton serial={d.id}/>}
          {d.via === 'Wi-Fi' && (
            <span className='x' onClick={(e) => { e.stopPropagation(); onClose(d.id); }}><Icon.X width={11} height={11}/></span>
          )}
        </div>
      ))}
      <div className='devicetab add' onClick={onAdd}><Icon.Plus width={13} height={13}/>Connect</div>
    </div>
  );
}

function Sidebar({device, screen, setScreen, counts}:{device?: adb.Device; screen: Screen; setScreen: (s: Screen) => void; counts: {forwards: number; apps: number}}) {
  const items: {key: Screen; label: string; icon: React.ReactNode; count?: number}[] = [
    {key: 'overview',   label: 'Overview',  icon: <Icon.Activity/>},
    {key: 'logcat',     label: 'Logcat',    icon: <Icon.Monitor/>},
    {key: 'shell',      label: 'Shell',     icon: <Icon.Terminal/>},
    {key: 'processes',  label: 'Processes', icon: <Icon.Cpu/>},
    {key: 'apps',       label: 'Apps',         icon: <Icon.Grid/>, count: counts.apps},
    {key: 'files',      label: 'Files',        icon: <Icon.Folder/>},
    {key: 'frida',      label: 'Frida',        icon: <Icon.Zap/>},
    {key: 'forwards',   label: 'ADB Forwards', icon: <Icon.Arrows/>, count: counts.forwards},
    {key: 'network',    label: 'Network',      icon: <Icon.Wifi/>},
    {key: 'capture',    label: 'Capture',      icon: <Icon.Globe/>},
    {key: 'iptables',   label: 'iptables',     icon: <Icon.Shield/>},
  ];
  return (
    <aside className='sidebar'>
      {device && (
        <div className='devicecard'>
          <div className='model'>
            <span>{device.label || device.model || 'Device'}</span>
            {device.root ? <Badge kind='accent'>root</Badge> : <Badge>user</Badge>}
          </div>
          <div className='serial'>{device.id}</div>
          <div className='badges'>
            <Badge>{device.via}</Badge>
            {device.androidVersion && <Badge>Android {device.androidVersion}</Badge>}
            {device.online ? <Badge kind='ok'><span className='dot'/>online</Badge> : <Badge kind='warn'>offline</Badge>}
          </div>
        </div>
      )}
      <div className='group'>
        <div className='label'>Device</div>
        {items.map(it => (
          <div key={it.key} className={`nav${screen === it.key ? ' active' : ''}`} onClick={() => setScreen(it.key)}>
            <span className='icon'>{it.icon}</span>
            <span>{it.label}</span>
            {it.count !== undefined && it.count > 0 && <span className='count'>{it.count}</span>}
          </div>
        ))}
      </div>
      <div className='group host'>
        <div className='label'>Host</div>
        <div className={`nav${screen === 'emulators' ? ' active' : ''}`} onClick={() => setScreen('emulators')}>
          <span className='icon'><Icon.Phone/></span>
          <span>Emulators</span>
        </div>
      </div>
      <div className='footer'>
        <span>adbq · {import.meta.env.MODE}</span>
      </div>
    </aside>
  );
}

// ConnectCommand shows the `adb connect` the dialog will run, live as the
// address is typed. From Go like every other command (CLAUDE.md §4.1).
function ConnectCommand({addr}: {addr: string}) {
  const [cmds, setCmds] = useState<string[]>([]);
  useEffect(() => {
    let live = true;
    if (!addr.trim()) { setCmds([]); return; }
    API.ConnectCommands(addr.trim())
      .then(c => { if (live) setCmds(c.connect ?? []); })
      .catch(() => { if (live) setCmds([]); });
    return () => { live = false; };
  }, [addr]);
  return <CommandPreview commands={cmds} defaultOpen/>;
}

function Settings({open, onClose, themeMode, setTheme, accent, setAccent}:{open: boolean; onClose:()=>void; themeMode: ThemeMode; setTheme:(t:ThemeMode)=>void; accent: string; setAccent:(c:string)=>void}) {
  const [version, setVersion] = useState('');
  useEffect(() => { if (open) API.ADBVersion().then(setVersion).catch(() => {}); }, [open]);
  return (
    <Modal open={open} onClose={onClose} title='Settings' width={520}
           footer={<button className='btn primary' onClick={onClose}>Done</button>}>
      <div className='field' style={{marginBottom: 16}}>
        <label>Theme</label>
        <div style={{display: 'flex', gap: 6}}>
          {(['light', 'dark', 'system'] as ThemeMode[]).map(t => (
            <button key={t} className={`btn sm${themeMode === t ? ' primary' : ''}`} onClick={() => setTheme(t)} style={{textTransform: 'capitalize'}}>{t}</button>
          ))}
        </div>
      </div>
      <div className='field' style={{marginBottom: 16}}>
        <label>Accent</label>
        <div style={{display: 'flex', gap: 8}}>
          {ACCENTS.map(c => (
            <button key={c} onClick={() => setAccent(c)}
                    style={{width: 28, height: 28, borderRadius: 14, background: c, border: accent === c ? '3px solid var(--text)' : '1px solid var(--border)', cursor: 'pointer'}}/>
          ))}
        </div>
      </div>
      <div className='card' style={{marginBottom: 16}}>
        <div className='card-body'>
          <div className='spread'><span className='muted'>adb version</span><span className='mono subtle' style={{fontSize: 11}}>{(version || '').split('\n')[0]}</span></div>
        </div>
      </div>
      <JadxSettings open={open}/>
      <AdbLoadPanel open={open}/>
    </Modal>
  );
}

// AdbLoadPanel shows how many adb processes adbq has started. adbq is a wrapper
// around a CLI, so process creation — not parsing, not rendering — is what it
// actually costs, and the per-second figure is the one number that says whether
// a change to a read path helped. It lives in Settings rather than a debug
// build because the honest measurement is the one taken on a real phone with a
// real ROM, which is exactly where a profiler is not available.
//
// It polls only while the Settings modal is open, and counts its own polling:
// ADBStats itself spawns nothing, so the figure stays truthful.
function AdbLoadPanel({open}: {open: boolean}) {
  const [stats, setStats] = useState<adb.ADBStats | null>(null);
  const [tracking, setTracking] = useState<adb.TrackerState | null>(null);
  const tick = useCallback(() => {
    API.ADBStats().then(setStats).catch(() => {});
    API.DeviceTracking().then(setTracking).catch(() => {});
  }, []);
  useEffect(() => { if (open) tick(); }, [open, tick]);
  usePoll(tick, 1000, open);
  if (!stats) return null;
  const top = stats.topCommands ?? [];
  const busiest = Math.max(1, ...top.map(c => c.count));
  return (
    <div className='card' style={{marginTop: 16}}>
      <div className='card-body'>
        <div className='spread' style={{alignItems: 'baseline', marginBottom: 10}}>
          <strong style={{fontSize: 12}}>adb load</strong>
          <button className='btn sm' onClick={() => API.ResetADBStats().then(() => API.ADBStats().then(setStats))}>
            Reset window
          </button>
        </div>
        <div className='spread'>
          <span className='muted'>processes / second</span>
          <span className='mono' style={{fontSize: 13, fontWeight: 600}}>{stats.perSecond.toFixed(2)}</span>
        </div>
        <div className='spread'>
          <span className='muted'>one-shot spawns</span>
          <span className='mono subtle' style={{fontSize: 11}}>
            {stats.spawns.toLocaleString()} in {Math.round(stats.windowSeconds)}s
          </span>
        </div>
        <div className='spread'>
          <span className='muted'>live streams</span>
          <span className='mono subtle' style={{fontSize: 11}}>{stats.streams}</span>
        </div>
        <div className='spread'>
          <span className='muted'>device tracking</span>
          <span className='mono subtle' style={{fontSize: 11}}>
            {stats.trackingDevices
              ? `push${tracking?.longForm === false ? ' (short form)' : ''}`
              : 'polling (fallback)'}
          </span>
        </div>
        {!stats.trackingDevices && tracking?.lastError && (
          <div className='spread'>
            <span className='muted'>last tracking error</span>
            <span className='mono subtle' style={{fontSize: 10.5, maxWidth: 260, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'}}
                  title={tracking.lastError}>{tracking.lastError}</span>
          </div>
        )}
        {top.length > 0 && (
          <div style={{marginTop: 10, borderTop: '1px solid var(--border)', paddingTop: 8}}>
            {top.map(c => (
              <div key={c.command} style={{display: 'flex', alignItems: 'center', gap: 8, marginBottom: 2}}>
                <span className='mono subtle' style={{fontSize: 10.5, flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'}}>
                  {c.command}
                </span>
                <span style={{width: 90, height: 5, background: 'var(--bg-inset)', borderRadius: 3, overflow: 'hidden', flexShrink: 0}}>
                  <span style={{display: 'block', height: '100%', width: `${(c.count / busiest) * 100}%`, background: 'var(--accent)'}}/>
                </span>
                <span className='mono subtle' style={{fontSize: 10.5, width: 46, textAlign: 'right'}}>{c.count.toLocaleString()}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// jadx is host-wide state, like the SDK path — one installation serves every
// device — so it is managed here rather than in the Apps screen, which only ever
// needs the button and the first-run consent dialog.
function JadxSettings({open}: {open: boolean}) {
  const [info, setInfo] = useState<adb.JadxInfo | null>(null);
  const [busy, setBusy] = useState('');
  const [stage, setStage] = useState('');
  const [staged, setStaged] = useState('');

  const refresh = useCallback(() => {
    invalidateJadxInfo();
    jadxInfo(true).then(setInfo).catch(() => setInfo(null));
  }, []);

  useEffect(() => {
    if (!open) return;
    jadxInfo().then(setInfo).catch(() => setInfo(null));
    API.StagedApkDir().then(setStaged).catch(() => {});
    const off = EventsOn('jadx:progress', (p: any) => setStage(String(p?.stage ?? '')));
    return () => { off(); setStage(''); };
  }, [open]);

  const download = async () => {
    if (!info) return;
    const ok = await confirmDialog({
      title: 'Download jadx?',
      body: [...(info.disclosures ?? []), '', `Source: ${info.asset}`, `SHA-256: ${info.sha256}`].join('\n'),
      confirmLabel: 'Download and verify',
    });
    if (!ok) return;
    setBusy('download');
    try {
      setInfo(await API.DownloadJadx());
      invalidateJadxInfo();
    } catch (e) {
      showToast({title: 'Download failed', body: String(e), kind: 'err'});
    } finally {
      setBusy(''); setStage(''); refresh();
    }
  };

  // Updating is deliberately a manual act: adbq stays on the version it ships a
  // digest for, and anything newer is only installed after the user has seen
  // which version it is and what it hashes to.
  const checkForUpdate = async () => {
    setBusy('check');
    try {
      const rel = await API.JadxLatest();
      if (!rel.newer && info?.version === rel.version) {
        showToast({title: `jadx ${rel.version} is the newest release`, body: 'Nothing to do.', kind: 'ok'});
        return;
      }
      if (!rel.sha256) {
        showToast({
          title: `jadx ${rel.version} cannot be verified`,
          body: 'That release publishes no checksum, so adbq will not download it. Install it yourself and point adbq at it below.',
          kind: 'err', ttl: 9000,
        });
        return;
      }
      const ok = await confirmDialog({
        title: `Install jadx ${rel.version}?`,
        body: [
          `adbq ships pinned to ${info?.pinnedVersion ?? ''}. This installs a newer release instead.`,
          '',
          `Version: ${rel.version}${rel.published ? ` (published ${rel.published.slice(0, 10)})` : ''}`,
          `Source: ${rel.asset}`,
          `SHA-256 (published by GitHub): ${rel.sha256}`,
          '',
          'The download is verified against that digest before anything is unpacked.',
        ].join('\n'),
        confirmLabel: 'Download and verify',
      });
      if (!ok) return;
      setBusy('download');
      setInfo(await API.UpdateJadx(rel));
      invalidateJadxInfo();
    } catch (e) {
      showToast({title: 'Could not check for a newer release', body: String(e), kind: 'err'});
    } finally {
      setBusy(''); setStage(''); refresh();
    }
  };

  const remove = async () => {
    const ok = await confirmDialog({
      title: 'Remove the downloaded jadx?',
      body: 'Only the copy adbq downloaded is deleted. It can be downloaded again at any time.',
      confirmLabel: 'Remove', danger: true,
    });
    if (!ok) return;
    setBusy('remove');
    try {
      setInfo(await API.RemoveJadx());
      invalidateJadxInfo();
    } catch (e) {
      showToast({title: 'Could not remove jadx', body: String(e), kind: 'err'});
    } finally {
      setBusy(''); refresh();
    }
  };

  const pick = async (what: 'jadx' | 'java') => {
    const p = what === 'jadx' ? await API.PickJadxPath() : await API.PickJavaPath();
    if (!p) return;
    try {
      setInfo(what === 'jadx' ? await API.SetJadxPath(p) : await API.SetJavaPath(p));
      invalidateJadxInfo();
    } catch (e) {
      showToast({title: 'Could not use that path', body: String(e), kind: 'err'});
    }
  };

  const clear = async (what: 'jadx' | 'java') => {
    setInfo(what === 'jadx' ? await API.SetJadxPath('') : await API.SetJavaPath(''));
    invalidateJadxInfo();
  };

  return (
    <div className='card'>
      <div className='card-header'>
        <span className='title' style={{fontSize: 12}}>jadx (decompiler)</span>
        <Badge kind={info?.installed ? (info.java ? 'ok' : 'warn') : 'muted'}>
          {info?.installed ? (info.kind === 'managed' ? `downloaded ${info.version}` : 'your own install') : 'not installed'}
        </Badge>
      </div>
      <div className='card-body' style={{display: 'flex', flexDirection: 'column', gap: 8, fontSize: 12}}>
        <div className='spread'>
          <span className='muted'>Launcher</span>
          <span className='mono subtle' style={{fontSize: 11}}>{info?.bin || '—'}</span>
        </div>
        <div className='spread'>
          <span className='muted'>Java</span>
          <span className='mono subtle' style={{fontSize: 11}}>
            {info?.java ? `${info.javaVersion} · ${info.javaSource}` : '—'}
          </span>
        </div>
        {info && !info.java && (
          <div className='muted' style={{fontSize: 11, color: 'var(--danger)'}}>{info.javaError}</div>
        )}
        {stage && <div className='muted' style={{fontSize: 11}}>{stage}…</div>}
        {info && !info.installed && (
          <CodeBlock multiline>{`${info.source}\nversion ${info.pinnedVersion}\nSHA-256 ${info.sha256}`}</CodeBlock>
        )}
        <div style={{display: 'flex', gap: 6, flexWrap: 'wrap'}}>
          {info?.kind !== 'external' && (
            <button className='btn sm' disabled={busy !== '' || info?.installed} onClick={download}>Download</button>
          )}
          <button className='btn sm' disabled={busy !== ''} onClick={checkForUpdate}>Check for a newer release</button>
          {info?.kind === 'managed' && (
            <button className='btn sm' disabled={busy !== ''} onClick={remove}>Remove</button>
          )}
          <button className='btn sm' disabled={busy !== ''} onClick={() => pick('jadx')}>Use my own…</button>
          {info?.kind === 'external' && (
            <button className='btn sm' disabled={busy !== ''} onClick={() => clear('jadx')}>Back to auto-detect</button>
          )}
          <button className='btn sm' disabled={busy !== ''} onClick={() => pick('java')}>Set Java…</button>
        </div>
        {staged && (
          <div className='spread' style={{marginTop: 4}}>
            <span className='muted' style={{fontSize: 11}}>APKs copied here for analysis</span>
            <button className='btn sm' disabled={busy !== ''} onClick={() => {
              API.ClearStagedApks()
                .then(() => showToast({title: 'Staged APKs cleared', kind: 'ok'}))
                .catch(e => showToast({title: 'Could not clear them', body: String(e), kind: 'err'}));
            }}>Clear</button>
          </div>
        )}
      </div>
    </div>
  );
}
