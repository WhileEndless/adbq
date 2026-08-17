import React, {useEffect, useRef, useState} from 'react';
import {adb} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {Icon} from '../icons';
import {Badge, CommandChip, CommandPreview, Dropdown, commandToast, confirmDialog, showToast} from '../ui';
import {getCached, mutateData} from '../cache';
import {pickApkAndInstall} from '../lib/apk';

export function OverviewScreen({device, setScreen}: {device: adb.Device; setScreen?: (s: string) => void}) {
  // Seed from cache for an instant first paint when reopening Overview; the poll
  // below keeps it live and writes each sample back to the cache.
  const [stats, setStats] = useState<adb.Stats | null>(() => getCached<adb.Stats>(`stats:${device?.id}`) ?? null);
  const [history, setHistory] = useState<{cpu: number[]; mem: number[]; batt: number[]}>({cpu: [], mem: [], batt: []});

  useEffect(() => {
    if (!device?.id) return;
    let alive = true;
    setStats(getCached<adb.Stats>(`stats:${device.id}`) ?? null);
    const tick = () => API.GetStats(device.id).then(s => {
      if (!alive) return;
      setStats(s);
      mutateData(`stats:${device.id}`, s);
      const memPct = s.memTotalKb > 0 ? (1 - s.memAvailKb / s.memTotalKb) * 100 : 0;
      setHistory(h => ({
        cpu: [...h.cpu.slice(-29), s.cpuPercent || 0],
        mem: [...h.mem.slice(-29), memPct],
        batt: [...h.batt.slice(-29), s.batteryLevel],
      }));
    }).catch(() => {});
    tick();
    const t = setInterval(tick, 2500);
    return () => { alive = false; clearInterval(t); };
  }, [device?.id]);

  // The commands behind this screen's buttons. From Go, so the reboot dialog
  // shows the line that runs rather than one typed out here (CLAUDE.md §4.1).
  const [cmds, setCmds] = useState<adb.DeviceCommands | null>(null);
  useEffect(() => {
    if (!device?.id) { setCmds(null); return; }
    let live = true;
    API.DeviceCommands(device.id, 30)
      .then(c => { if (live) setCmds(c); })
      .catch(() => { if (live) setCmds(null); });
    return () => { live = false; };
  }, [device?.id]);

  const memPct = stats && stats.memTotalKb > 0 ? Math.round((1 - stats.memAvailKb / stats.memTotalKb) * 100) : 0;
  const storagePct = stats && stats.storageTotalKb > 0 ? Math.round((1 - stats.storageFreeKb / stats.storageTotalKb) * 100) : 0;

  function rebootCmd(mode: string): string[] {
    if (mode === 'recovery') return cmds?.rebootRecovery ?? [];
    if (mode === 'bootloader') return cmds?.rebootBootloader ?? [];
    // fastboot and anything else share the plain form with the mode appended,
    // which only Go knows how to spell — fall back to no preview rather than
    // guessing at it.
    return mode === '' ? (cmds?.reboot ?? []) : [];
  }

  function doReboot(mode: string) {
    confirmDialog({
      title: `Reboot ${mode || 'device'}?`,
      body: <CommandPreview commands={rebootCmd(mode)} defaultOpen/>,
      confirmLabel: 'Reboot',
      danger: true,
    }).then(ok => {
      if (!ok) return;
      API.Reboot(device.id, mode)
        .then(() => showToast({title: 'Rebooting', body: mode || 'system', kind: 'ok', mono: true}))
        .catch(e => showToast({title: 'Reboot failed', body: String(e), kind: 'err'}));
    });
  }

  return (
    <div className='screen'>
      <div className='screen-header'>
        <h1>{device.label || device.model || device.id}
          {device.root ? <Badge kind='accent'><Icon.Shield width={11} height={11}/>{device.rootMethod || 'root'}</Badge> : <Badge>unrooted</Badge>}
          {device.online ? <Badge kind='ok'><span className='dot'/> online</Badge> : <Badge kind='warn'>offline</Badge>}
        </h1>
        <span className='subtitle mono'>{device.id} · {device.via}</span>
        <div className='spacer' style={{flex: 1}}/>
        <button className='btn' onClick={() => takeShot(device.id, cmds?.screenshot)}>
          <Icon.Camera/> Screenshot
        </button>
        <Dropdown trigger={<button className='btn'><Icon.Refresh/>Reboot</button>} items={[
          {label: 'Reboot device', onClick: () => doReboot('')},
          {label: 'Reboot to recovery',   onClick: () => doReboot('recovery')},
          {label: 'Reboot to bootloader', onClick: () => doReboot('bootloader')},
          {label: 'Reboot to fastboot',   onClick: () => doReboot('fastboot')},
          {label: '', onClick: () => {}, divider: true},
          {label: 'Power off', danger: true, onClick: () => {
            confirmDialog({
              title: 'Power off device?',
              body: <CommandPreview commands={cmds?.powerOff ?? []} defaultOpen/>,
              confirmLabel: 'Power off', danger: true,
            }).then(ok => {
              if (ok) API.PowerOffDevice(device.id).catch(e => showToast({title: 'Power off failed', body: String(e), kind: 'err'}));
            });
          }},
        ]}/>
      </div>

      <div className='screen-body'>
        <div className='grid-4' style={{marginBottom: 14}}>
          <Stat label='Battery'
                value={stats ? `${stats.batteryLevel}%` : '—'}
                sub={stats ? `${stats.batteryTemp.toFixed(1)}°C${stats.batteryVoltage ? ' · ' + stats.batteryVoltage + ' mV' : ''}${stats.charging ? ' · charging' : ''}` : ''}
                series={history.batt}/>
          <Stat label='RAM'
                value={stats ? `${memPct}%` : '—'}
                sub={stats ? `${fmtKB(stats.memTotalKb - stats.memAvailKb)} used of ${fmtKB(stats.memTotalKb)}` : ''}
                series={history.mem}/>
          <Stat label='CPU'
                value={stats ? `${(stats.cpuPercent || 0).toFixed(1)}%` : '—'}
                sub={stats ? `loadavg ${stats.loadAvg1.toFixed(2)} · ${fmtUptime(stats.uptimeSeconds)}` : ''}
                series={history.cpu}/>
          <Stat label='Storage'
                value={stats && stats.storageTotalKb > 0 ? `${storagePct}%` : '—'}
                sub={stats && stats.storageTotalKb > 0 ? `${fmtKB(stats.storageFreeKb)} free of ${fmtKB(stats.storageTotalKb)}` : '/data'}/>
        </div>

        <div className='grid-2' style={{gap: 14, marginBottom: 14}}>
          <div className='card'>
            <div className='card-header'>
              <div className='title'>Quick actions</div>
              <div style={{flex: 1}}/>
              {/* One reference view for the panel instead of a command under
                  every button — the panel stays a panel. */}
              <CommandChip label='Quick actions' groups={[
                {label: 'Screenshot', commands: cmds?.screenshot},
                {label: 'Screen record', commands: cmds?.screenRecord, note: 'Stopping early is what finalises the MP4.'},
                {label: 'Mirror (scrcpy)', commands: cmds?.scrcpy},
                {label: 'Wi-Fi adb', commands: cmds?.tcpip},
                {label: 'Restart adbd', commands: cmds?.restartAdbd},
                {label: 'Reboot', commands: cmds?.reboot},
                {label: 'Power off', commands: cmds?.powerOff},
              ]}/>
            </div>
            <div className='card-body' style={{display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 6}}>
              <QA icon={<Icon.Camera/>} label='Screenshot' onClick={() => takeShot(device.id, cmds?.screenshot)}/>
              <QA icon={<Icon.Download/>} label='Save as…' onClick={() =>
                API.SaveScreenshotAs(device.id).then(p => p && showToast({
                  title: 'Saved', body: p, kind: 'ok', mono: true,
                  actions: [
                    {label: 'Open', onClick: () => API.OpenPath(p)},
                    {label: 'Reveal', onClick: () => API.RevealPath(p)},
                  ],
                }))}/>
              <QA icon={<Icon.Monitor/>} label='Screen record (30s)' onClick={() => {
                showToast({title: 'Recording…', body: 'will stop after 30s', kind: 'info'});
                API.ScreenRecord(device.id, 30).then(p => p && showToast({
                  title: 'Recording saved', body: p, kind: 'ok', mono: true,
                  actions: [
                    {label: 'Open', onClick: () => API.OpenPath(p)},
                    {label: 'Reveal', onClick: () => API.RevealPath(p)},
                    ...commandToast(cmds?.screenRecord),
                  ],
                })).catch(e => showToast({title: 'Recording failed', body: String(e), kind: 'err'}));
              }}/>
              <QA icon={<Icon.Terminal/>} label='Open shell' onClick={() => setScreen?.('shell')}/>
              <ScrcpyAction device={device} cmd={cmds?.scrcpy}/>
              <QA icon={<Icon.Upload/>} label='Install APK / APKS' onClick={() =>
                pickApkAndInstall(device.id)}/>
              <QA icon={<Icon.Wifi/>} label='Wi-Fi adb' onClick={async () => {
                const ok = await confirmDialog({
                  title: 'Enable Wi-Fi adb (tcpip 5555)?',
                  body: <>
                    Afterwards connect with <span className='mono'>adb connect {device.ip || '<ip>'}:5555</span>.
                    <CommandPreview commands={cmds?.tcpip ?? []} defaultOpen/>
                  </>,
                  confirmLabel: 'Enable',
                });
                if (!ok) return;
                try {
                  await API.TcpipMode(device.id, 5555);
                  // The body names the address to connect to, not a command:
                  // the command to copy is the one that just ran.
                  showToast({
                    title: 'Wi-Fi adb enabled', body: `${device.ip || '<device ip>'}:5555`,
                    kind: 'ok', mono: true, actions: commandToast(cmds?.tcpip),
                  });
                } catch (e) {
                  showToast({title: 'tcpip failed', body: String(e), kind: 'err'});
                }
              }}/>
              <QA icon={<Icon.Refresh/>} label='Restart adbd' onClick={() =>
                confirmDialog({
                  title: 'Restart adbd?',
                  body: <>
                    Drops the current adb connection. Replug or reconnect afterwards.
                    <CommandPreview commands={cmds?.restartAdbd ?? []} defaultOpen/>
                  </>,
                  danger: true, confirmLabel: 'Restart adbd',
                }).then(ok => {
                  if (!ok) return;
                  API.RestartAdbd(device.id)
                    .then(() => showToast({
                      title: 'adbd restarting', body: 'reconnect in 2-3s',
                      kind: 'info', mono: true, actions: commandToast(cmds?.restartAdbd),
                    }))
                    .catch(e => showToast({title: 'Restart failed', body: String(e), kind: 'err'}));
                })}/>
            </div>
          </div>
          <div className='card'>
            <div className='card-header'>
              <div className='title'>Root</div>
              <div style={{flex: 1}}/>
              <CommandChip label='Re-test root' commands={cmds?.rootProbe}/>
            </div>
            <div className='card-body'>
              <Kv k='Status' v={device.root ? 'rooted' : 'unrooted'}/>
              <Kv k='Method' v={device.rootMethod || '—'}/>
              <Kv k='Detected via' v={device.root ? 'su / magisk / id' : 'no su; no magisk dir'}/>
              <div style={{marginTop: 10, display: 'flex', gap: 6}}>
                <button className='btn' onClick={() =>
                  API.RootSignals(device.id)
                    .then(o => showToast({title: 'Re-tested root', body: o || '(no signals)', kind: 'info', mono: true}))
                    .catch(e => showToast({title: 'Re-test failed', body: String(e), kind: 'err'}))}>
                  <Icon.Refresh/>Re-test
                </button>
              </div>
            </div>
          </div>
        </div>

        <div className='grid-2' style={{gap: 14}}>
          <div className='card'>
            <div className='card-header'><div className='title'>Device</div></div>
            <div className='card-body'>
              <Kv k='Manufacturer' v={device.manufacturer}/>
              <Kv k='Model'        v={device.model}/>
              <Kv k='Product'      v={device.product} mono/>
              <Kv k='Android'      v={`${device.androidVersion} (SDK ${device.sdk})`}/>
              <Kv k='Build'        v={device.buildId} mono/>
              <Kv k='Kernel'       v={device.kernel} mono/>
              <Kv k='ABI'          v={device.arch} mono/>
              <Kv k='Hardware'     v={device.cpu} mono/>
            </div>
          </div>
          <div className='card'>
            <div className='card-header'><div className='title'>Connectivity</div></div>
            <div className='card-body'>
              <Kv k='Transport' v={device.via} mono/>
              <Kv k='Serial'    v={device.id} mono/>
              <Kv k='IP'        v={device.ip} mono/>
              <Kv k='Wi-Fi'     v={device.wifi}/>
              <Kv k='MAC'       v={device.mac} mono/>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function ScrcpyAction({device, cmd}: {device: adb.Device; cmd?: string[] | null}) {
  const [available, setAvailable] = useState<boolean | null>(null);
  const [active, setActive] = useState(false);
  useEffect(() => {
    API.ScrcpyAvailable().then(setAvailable).catch(() => setAvailable(false));
  }, []);
  useEffect(() => {
    if (!device?.id) return;
    const tick = () => API.ScrcpyActive(device.id).then(setActive).catch(() => {});
    tick();
    const t = setInterval(tick, 2000);
    return () => clearInterval(t);
  }, [device?.id]);
  if (available === null) return <QA icon={<Icon.Monitor/>} label='scrcpy' onClick={() => {}}/>;
  if (available === false) {
    return (
      <QA icon={<Icon.Monitor/>} label='Install scrcpy' onClick={() => {
        showToast({
          title: 'scrcpy not installed',
          body: 'brew install scrcpy  ·  https://github.com/Genymobile/scrcpy',
          kind: 'info', mono: true, ttl: 8000,
        });
      }}/>
    );
  }
  return (
    <QA icon={active ? <Icon.Stop/> : <Icon.Monitor/>} label={active ? 'Stop scrcpy' : 'Mirror (scrcpy)'} onClick={() => {
      if (active) {
        API.StopScrcpy(device.id).then(() => { setActive(false); showToast({title: 'scrcpy stopped', kind: 'ok'}); });
      } else {
        API.StartScrcpy(device.id)
          .then(() => { setActive(true); showToast({title: 'scrcpy starting', kind: 'ok', actions: commandToast(cmd)}); })
          .catch(e => showToast({title: 'scrcpy failed', body: String(e), kind: 'err'}));
      }
    }}/>
  );
}

function takeShot(serial: string, cmd?: string[] | null) {
  API.TakeScreenshot(serial)
    .then(p => showToast({
      title: 'Screenshot saved', body: p, kind: 'ok', mono: true,
      actions: [
        {label: 'Open', onClick: () => API.OpenPath(p)},
        {label: 'Reveal', onClick: () => API.RevealPath(p)},
        ...commandToast(cmd),
      ],
    }))
    .catch(e => showToast({title: 'Screenshot failed', body: String(e), kind: 'err'}));
}

function fmtUptime(s: number) {
  if (!s) return '';
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return `up ${d}d ${h}h`;
  if (h > 0) return `up ${h}h ${m}m`;
  return `up ${m}m`;
}

function fmtKB(kb: number) {
  return kb >= 1024 * 1024 ? (kb / 1024 / 1024).toFixed(1) + ' GB' : Math.round(kb / 1024) + ' MB';
}

function Stat({label, value, sub, pct, series}: {label: string; value: string; sub?: string; pct?: number; series?: number[]}) {
  return (
    <div className='stat'>
      <div className='stat-label'>{label}</div>
      <div className='stat-value'>{value}</div>
      <div className='stat-sub'>{sub}</div>
      {pct !== undefined && <div className='bar' style={{marginTop: 4}}><div className='fill' style={{width: Math.min(100, Math.max(0, pct)) + '%'}}/></div>}
      {series && series.length > 1 && <Spark series={series}/>}
    </div>
  );
}

function Spark({series}: {series: number[]}) {
  if (series.length < 2) return null;
  const W = 200, H = 36;
  const max = Math.max(1, ...series);
  const min = Math.min(0, ...series);
  const norm = (v: number) => H - ((v - min) / Math.max(1, max - min)) * H;
  const step = W / (series.length - 1);
  const path = series.map((v, i) => `${i === 0 ? 'M' : 'L'}${i * step},${norm(v).toFixed(1)}`).join(' ');
  return (
    <svg className='spark' viewBox={`0 0 ${W} ${H}`} preserveAspectRatio='none'>
      <path d={path} fill='none' stroke='var(--accent)' strokeWidth='1.4'/>
      <path d={path + ` L${W},${H} L0,${H} Z`} fill='var(--accent-soft)'/>
    </svg>
  );
}

function QA({icon, label, onClick}: {icon: React.ReactNode; label: string; onClick: () => void}) {
  return (
    <button className='btn' style={{flexDirection: 'column', padding: '12px 6px', gap: 4, height: 64}} onClick={onClick}>
      <span style={{color: 'var(--accent)'}}>{icon}</span>
      <span style={{fontSize: 11}}>{label}</span>
    </button>
  );
}

function Kv({k, v, mono}: {k: string; v?: string; mono?: boolean}) {
  return (
    <div className='spread' style={{padding: '5px 0'}}>
      <span className='muted'>{k}</span>
      <span className={mono ? 'mono' : ''} style={{textAlign: 'right', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: '60%'}}>{v || '—'}</span>
    </div>
  );
}
