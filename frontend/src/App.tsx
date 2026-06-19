import React, {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import {adb} from '../wailsjs/go/models';
import * as API from '../wailsjs/go/main/App';
import {Icon} from './icons';
import {Badge, ConfirmHost, IconBtn, Modal, PromptHost, ToastHost, showToast, promptDialog, useTheme, ThemeMode} from './ui';
import {StoreProvider} from './store';
import {TasksTray} from './tasks';
import {Screen} from './types';
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
import {ProfileSelector, ProfileEditor, ApplyConfirm, PastDevices} from './screens/Profiles';

const ACCENTS = ['#a07cf7', '#7aa2ff', '#5ed29a', '#e9b454', '#ec6a73', '#c5a3ff'];

export default function App() {
  return (
    <StoreProvider>
      <AppInner/>
    </StoreProvider>
  );
}

function AppInner() {
  const {mode: themeMode, setMode: setTheme, theme, accent, setAccent} = useTheme();
  const [devices, setDevices] = useState<adb.Device[]>([]);
  const [activeId, setActiveId] = useState<string>('');
  const [screen, setScreen] = useState<Screen>('overview');
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [connectOpen, setConnectOpen] = useState(false);
  const [connectAddr, setConnectAddr] = useState('192.168.1.10:5555');
  const [counts, setCounts] = useState<{forwards: number; apps: number}>({forwards: 0, apps: 0});

  // ─── Device profiles ──────────────────────────────────────────────────────
  const [profilesVersion, setProfilesVersion] = useState(0);
  const [editor, setEditor] = useState<{profile: adb.Profile | null} | null>(null);
  const [pastOpen, setPastOpen] = useState(false);
  const [apply, setApply] = useState<{serial: string; profileId: string} | null>(null);
  const prevOnline = useRef<Record<string, boolean>>({});
  const pendingApply = useRef<Set<string>>(new Set());

  const bump = () => setProfilesVersion(v => v + 1);
  const switchProfile = useCallback((serial: string, profileId: string) => {
    API.BindDeviceProfile(serial, profileId).then(bump).catch(() => {});
    setApply({serial, profileId});
  }, []);
  const captureProfile = useCallback((serial: string, suggested: string) => {
    promptDialog({title: 'Capture profile from device', label: 'Profile name', defaultValue: suggested})
      .then(name => {
        if (!name) return;
        API.CaptureProfileFromDevice(serial, name)
          .then(p => { bump(); setEditor({profile: p}); showToast({title: 'Captured current settings', kind: 'ok'}); })
          .catch(e => showToast({title: 'Capture failed', body: String(e), kind: 'err'}));
      });
  }, []);

  const reload = useCallback(() => {
    API.ListDevices()
      .then(devs => {
        const list = devs || [];
        setDevices(list);
        setActiveId(prev => prev && list.some(d => d.id === prev) ? prev : (list[0]?.id || ''));
      })
      .catch(e => {
        // Don't spam the user with toasts when the device transiently goes
        // offline (usb replug, screen sleep). Just leave the list as-is.
        const msg = String(e);
        if (!msg.includes('device offline') && !msg.includes('not found')) {
          // eslint-disable-next-line no-console
          console.warn('ListDevices error:', msg);
        }
      });
  }, []);

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
  useEffect(() => {
    reload();
    const t = setInterval(reload, 5000);
    return () => clearInterval(t);
  }, [reload]);

  // Profile auto-apply: on a genuine offline→online transition, if the device
  // has a bound profile, prompt to apply it (confirm-first, never silent). Also
  // keep the device history fresh. First sight (undefined→online) only records
  // state — we don't prompt on app launch for already-connected devices.
  useEffect(() => {
    for (const d of devices) {
      const key = d.hardwareSerial || d.id;
      API.RegisterDevice(d).catch(() => {});
      const was = prevOnline.current[key];
      if (d.online && was === false && !pendingApply.current.has(key) && !apply) {
        pendingApply.current.add(key);
        API.LookupDeviceProfile(key)
          .then(pid => {
            if (pid) setApply({serial: d.id, profileId: pid});
            else pendingApply.current.delete(key);
          })
          .catch(() => pendingApply.current.delete(key));
      }
      prevOnline.current[key] = d.online;
    }
  }, [devices, apply]);

  const device = useMemo(() => devices.find(d => d.id === activeId) || devices[0], [devices, activeId]);

  useEffect(() => {
    if (!device?.id) return;
    Promise.all([API.ListForwards(device.id), API.ListReverses(device.id)])
      .then(([f, r]) => setCounts(c => ({...c, forwards: (f?.length || 0) + (r?.length || 0)})));
    API.ListApps(device.id, true).then(a => setCounts(c => ({...c, apps: a?.length || 0}))).catch(() => {});
  }, [device?.id, screen]);

  // Global keyboard shortcuts: Cmd/Ctrl+1..9 jump to the sidebar item at
  // that index. Numeric keys are ignored when a text input has focus so they
  // don't hijack typing.
  useEffect(() => {
    const order: Screen[] = [
      'overview', 'logcat', 'shell', 'processes', 'apps',
      'files', 'frida', 'forwards', 'network',
      'capture', 'iptables',
    ];
    const onKey = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || e.altKey || e.shiftKey) return;
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
                  onClose={(id) => API.DisconnectDevice(id).then(reload)}/>
      <Sidebar device={device} screen={screen} setScreen={setScreen} counts={counts}/>
      <main className='main'>
        {device
          ? <ScreenComp device={device} setScreen={setScreen as any}/>
          : <div style={{padding: 40, textAlign: 'center'}}>
              <div style={{fontSize: 16, marginBottom: 6}}>No devices connected</div>
              <div className='muted' style={{marginBottom: 14}}>Plug in a USB device or connect over Wi-Fi.</div>
              <button className='btn primary' onClick={() => setConnectOpen(true)}><Icon.Plus/>Connect</button>
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
        <ApplyConfirm serial={apply.serial} profileId={apply.profileId}
                      reload={reload}
                      onApplied={bump}
                      onClose={() => {
                        const k = devices.find(d => d.id === apply.serial)?.hardwareSerial || apply.serial;
                        pendingApply.current.delete(k);
                        setApply(null);
                      }}/>
      )}
      {pastOpen && (
        <PastDevices onClose={() => setPastOpen(false)}
                     onApply={(serial, profileId) => setApply({serial, profileId})}/>
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
      <div className='brand'><span className='dot'/>adbq</div>
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
  const [active, setActive] = useState(false);
  const [available, setAvailable] = useState<boolean | null>(null);
  useEffect(() => { API.ScrcpyAvailable().then(setAvailable).catch(() => setAvailable(false)); }, []);
  useEffect(() => {
    if (!serial) return;
    const tick = () => API.ScrcpyActive(serial).then(setActive).catch(() => {});
    tick();
    const t = setInterval(tick, 2500);
    return () => clearInterval(t);
  }, [serial]);
  if (!available) return null;
  return (
    <button className={`iconbtn${active ? ' active' : ''}`} title={active ? 'Stop scrcpy mirror' : 'Mirror screen with scrcpy'}
            onClick={(e) => {
              e.stopPropagation();
              if (active) { API.StopScrcpy(serial); setActive(false); }
              else        { API.StartScrcpy(serial).then(() => setActive(true)).catch(err => showToast({title: 'scrcpy failed', body: String(err), kind: 'err'})); }
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
      <div className='footer'>
        <span>adbq · {import.meta.env.MODE}</span>
      </div>
    </aside>
  );
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
      <div className='card'>
        <div className='card-body'>
          <div className='spread'><span className='muted'>adb version</span><span className='mono subtle' style={{fontSize: 11}}>{(version || '').split('\n')[0]}</span></div>
        </div>
      </div>
    </Modal>
  );
}
