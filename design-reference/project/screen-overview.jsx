// Overview / Device dashboard
function Sparkline({ data, color = "var(--accent)", height = 36 }) {
  if (!data.length) return null;
  const max = Math.max(...data);
  const min = Math.min(...data);
  const range = Math.max(1, max - min);
  const w = 100;
  const points = data.map((v, i) => {
    const x = (i / (data.length - 1)) * w;
    const y = height - ((v - min) / range) * (height - 4) - 2;
    return `${x.toFixed(2)},${y.toFixed(2)}`;
  }).join(" ");
  const last = data[data.length - 1];
  const lastX = w;
  const lastY = height - ((last - min) / range) * (height - 4) - 2;
  return (
    <svg className="spark" viewBox={`0 0 ${w} ${height}`} preserveAspectRatio="none">
      <defs>
        <linearGradient id={`g-${color.replace(/[^a-z0-9]/gi, "")}`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.55" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      <polygon points={`0,${height} ${points} ${lastX},${height}`}
               fill={`url(#g-${color.replace(/[^a-z0-9]/gi, "")})`} stroke="none" />
      <polyline points={points} fill="none" stroke={color} strokeWidth="1.5" />
      <circle cx={lastX} cy={lastY} r="1.8" fill={color} />
    </svg>
  );
}

function Stat({ label, value, sub, icon, bar, spark, color }) {
  const Cmp = icon ? Icons[icon] : null;
  return (
    <div className="stat">
      <div className="stat-label">{Cmp && <Cmp size={12} />} {label}</div>
      <div className="stat-value">{value}</div>
      {sub && <div className="stat-sub">{sub}</div>}
      {bar != null && (
        <div className="bar" style={{ marginTop: 4 }}>
          <div className="fill" style={{ width: `${bar}%`, background: color || "var(--accent)" }} />
        </div>
      )}
      {spark && <Sparkline data={spark} color={color || "var(--accent)"} />}
    </div>
  );
}

function OverviewScreen({ device }) {
  // Live-ish ticking stats
  const [tick, setTick] = useState(0);
  useEffect(() => {
    const t = setInterval(() => setTick(x => x + 1), 1500);
    return () => clearInterval(t);
  }, []);

  const cpuSpark = useMemo(() => Array.from({ length: 24 }, (_, i) => 22 + Math.sin(i / 2 + tick / 3) * 14 + Math.random() * 6), [tick]);
  const ramSpark = useMemo(() => Array.from({ length: 24 }, (_, i) => 58 + Math.cos(i / 3 + tick / 4) * 6 + Math.random() * 3), [tick]);
  const netSpark = useMemo(() => Array.from({ length: 24 }, () => Math.random() * 80 + 10), [tick]);

  const cpu = Math.round(cpuSpark[cpuSpark.length - 1]);
  const ram = Math.round(ramSpark[ramSpark.length - 1]);
  const ramMb = Math.round((ram / 100) * device.ramTotal);

  return (
    <div className="screen">
      <div className="screen-header">
        <h1>
          {device.label}
          <span className="badge ok"><span className="pulse" /> online</span>
          {device.root && <span className="badge accent"><Icons.Shield size={10} /> rooted</span>}
        </h1>
        <span className="subtitle muted">{device.via} · {device.transport}</span>
        <div className="spacer" />
        <button className="btn"
                onClick={() => showToast({ title: "Screenshot captured", body: "~/adbq-shots/2026-05-22_12-18.png", kind: "ok", mono: true })}>
          <Icons.Camera className="icon" /> Screenshot
        </button>
        <DropdownButton label="Reboot" icon="Power" items={[
          { label: "Reboot",            icon: "Refresh", right: "adb reboot",          onClick: () => showToast({ title: "Rebooting", body: "adb reboot", kind: "info", mono: true }) },
          { label: "Recovery",          icon: "Shield",  right: "adb reboot recovery", onClick: () => showToast({ title: "Booting to recovery", body: "adb reboot recovery", kind: "info", mono: true }) },
          { label: "Bootloader",        icon: "Bolt",    right: "adb reboot bootloader",onClick: () => showToast({ title: "Booting to bootloader", body: "adb reboot bootloader", kind: "info", mono: true }) },
          { label: "Fastboot",          icon: "Power",   right: "adb reboot fastboot", onClick: () => showToast({ title: "Booting to fastboot", body: "adb reboot fastboot", kind: "info", mono: true }) },
          { sep: true },
          { label: "Shutdown", icon: "Power", danger: true, right: "adb reboot -p", onClick: () => showToast({ title: "Shutting down", body: "adb reboot -p", kind: "info", mono: true }) },
        ]} />
      </div>

      <div className="screen-body">
        <div className="grid-4" style={{ marginBottom: 14 }}>
          <Stat label="Battery" icon="Battery"
                value={`${device.battery.level}%`}
                sub={`${device.battery.charging ? "charging · " : ""}${device.battery.temp.toFixed(1)}°C · ${device.battery.voltage} mV`}
                bar={device.battery.level} color="var(--ok)" />
          <Stat label="CPU" icon="Cpu" value={`${cpu}%`} sub={`${device.cpu} · ${device.arch}`} spark={cpuSpark} color="var(--accent)" />
          <Stat label="Memory" icon="Memory" value={`${(ramMb / 1024).toFixed(1)} GB`} sub={`of ${(device.ramTotal / 1024).toFixed(0)} GB · ${ram}% used`} bar={ram} color="#6fb3ff" />
          <Stat label="Network" icon="Wifi" value={`${Math.round(netSpark[netSpark.length - 1])} KB/s`} sub={`${device.wifi} · ${device.ip}`} spark={netSpark} color="#e9b454" />
        </div>

        <div className="grid-2">
          <div className="card">
            <div className="card-header">
              <Icons.Phone size={13} className="muted" />
              <span className="title">Device</span>
            </div>
            <div className="card-body">
              <DetailGrid rows={[
                ["Manufacturer", device.manufacturer],
                ["Model",        device.model],
                ["Product",      device.product],
                ["Serial",       device.id, true],
                ["Transport",    device.transport, true],
                ["Android",      `${device.androidVersion} (SDK ${device.sdk})`],
                ["Build",        device.buildId, true],
                ["Kernel",       device.kernel, true],
                ["Architecture", device.arch],
                ["Storage",      `${device.storageFree.toFixed(1)} GB free of ${device.storageTotal} GB`],
              ]} />
            </div>
          </div>

          <div className="col" style={{ gap: 14 }}>
            <div className="card">
              <div className="card-header">
                <Icons.Bolt size={13} className="muted" />
                <span className="title">Quick actions</span>
              </div>
              <div className="card-body">
                <div className="grid-2" style={{ gap: 8 }}>
                  <QuickAction icon="Camera" label="Screenshot" />
                  <QuickAction icon="Image" label="Screen record" />
                  <QuickAction icon="Clipboard" label="Pull clipboard" />
                  <QuickAction icon="Upload" label="Push clipboard" />
                  <QuickAction icon="Power" label="Reboot" />
                  <QuickAction icon="Code" label="adb shell" accent />
                </div>
              </div>
            </div>

            <div className="card">
              <div className="card-header">
                <Icons.Shield size={13} className="muted" />
                <span className="title">Root status</span>
                <div className="spacer" style={{ flex: 1 }} />
                <span className={`badge ${device.root ? "ok" : "warn"}`}>
                  <span className="dot" /> {device.root ? "verified" : "unrooted"}
                </span>
              </div>
              <div className="card-body" style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                <div className="spread">
                  <div>
                    <div className="muted" style={{ fontSize: 11 }}>Method</div>
                    <div className="mono" style={{ fontSize: 12 }}>{device.rootMethod || "—"}</div>
                  </div>
                  <div>
                    <div className="muted" style={{ fontSize: 11 }}>su binary</div>
                    <div className="mono" style={{ fontSize: 12 }}>{device.root ? "/system/bin/su" : "—"}</div>
                  </div>
                  <div>
                    <div className="muted" style={{ fontSize: 11 }}>shell euid</div>
                    <div className="mono" style={{ fontSize: 12 }}>2000 (shell)</div>
                  </div>
                </div>
                <div className="divider" />
                <div className="row" style={{ gap: 8 }}>
                  <button className="btn primary"><Icons.Terminal className="icon" /> Open root shell</button>
                  <button className="btn"><Icons.Refresh className="icon" /> Re-test su</button>
                  <span className="muted" style={{ fontSize: 11 }}>Last verified 2m ago</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function DetailGrid({ rows }) {
  return (
    <div style={{ display: "grid", gridTemplateColumns: "120px 1fr", rowGap: 7, columnGap: 16, fontSize: 12 }}>
      {rows.map(([k, v, mono]) => (
        <React.Fragment key={k}>
          <div className="muted">{k}</div>
          <div className={mono ? "mono" : ""} style={{ wordBreak: "break-all" }}>{v}</div>
        </React.Fragment>
      ))}
    </div>
  );
}

function QuickAction({ icon, label, accent }) {
  const Cmp = Icons[icon];
  return (
    <button className="btn" style={{ justifyContent: "flex-start", padding: "8px 10px" }}>
      <span className={accent ? "" : "muted"}
            style={{ color: accent ? "var(--accent)" : undefined, display: "inline-flex" }}>
        <Cmp size={14} />
      </span>
      <span>{label}</span>
    </button>
  );
}

Object.assign(window, { OverviewScreen });
