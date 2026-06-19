// Network — Overview, Proxy, Hosts, DNS, Capture, Connections
function NetworkScreen({ device, devState, setDevState }) {
  const [tab, setTab] = useState("overview");

  const tabs = [
    { id: "overview",    name: "Overview",     icon: "Network" },
    { id: "proxy",       name: "Proxy",        icon: "Shield" },
    { id: "hosts",       name: "Host overrides", icon: "Globe", count: (devState.hosts || []).length },
    { id: "dns",         name: "DNS log",      icon: "Logcat", badge: "live" },
    { id: "capture",     name: "Capture",      icon: "Image" },
    { id: "connections", name: "Connections",  icon: "Forward", count: MOCK.NET_CONNECTIONS.length },
  ];

  return (
    <div className="screen">
      <div className="screen-header">
        <h1><Icons.Network size={17} /> Network</h1>
        <span className="subtitle muted">{device.wifi} · {device.ip} · {device.mac}</span>
        <div className="spacer" />
        <button className="btn ghost" onClick={() => showToast({ title: "Refreshed", body: "ip addr · ip route · resolvectl", kind: "info", mono: true })}>
          <Icons.Refresh className="icon" /> Refresh
        </button>
      </div>

      <div style={{ borderBottom: "1px solid var(--border)", padding: "0 18px", display: "flex", gap: 0, overflowX: "auto" }}>
        {tabs.map(t => {
          const Cmp = Icons[t.icon];
          return (
            <button key={t.id} onClick={() => setTab(t.id)}
                    style={{
                      padding: "10px 12px",
                      borderBottom: tab === t.id ? "2px solid var(--accent)" : "2px solid transparent",
                      marginBottom: -1,
                      color: tab === t.id ? "var(--text)" : "var(--text-muted)",
                      fontWeight: tab === t.id ? 600 : 500,
                      fontSize: 12.5,
                      display: "inline-flex", alignItems: "center", gap: 7,
                      whiteSpace: "nowrap",
                    }}>
              <Cmp size={13} /> {t.name}
              {t.badge === "live" && <span className="badge ok" style={{ padding: "0 5px", fontSize: 9 }}><span className="pulse" style={{ width: 4, height: 4 }} /></span>}
              {t.count != null && <span className="count" style={{ background: tab === t.id ? "var(--accent-soft)" : "var(--bg-inset)" }}>{t.count}</span>}
            </button>
          );
        })}
      </div>

      <div className="screen-body" style={{ display: "flex", flexDirection: "column", paddingTop: 16, paddingBottom: tab === "dns" || tab === "connections" || tab === "capture" ? 16 : 20, overflow: tab === "dns" || tab === "connections" ? "hidden" : "auto" }}>
        {tab === "overview"    && <NetOverview device={device} />}
        {tab === "proxy"       && <NetProxy device={device} devState={devState} setDevState={setDevState} />}
        {tab === "hosts"       && <NetHosts device={device} devState={devState} setDevState={setDevState} />}
        {tab === "dns"         && <NetDns device={device} />}
        {tab === "capture"     && <NetCapture device={device} devState={devState} setDevState={setDevState} />}
        {tab === "connections" && <NetConnections device={device} />}
      </div>
    </div>
  );
}

// ─── Overview ───────────────────────────────────────────────────────────────
function NetOverview({ device }) {
  return (
    <>
      <div className="grid-4" style={{ marginBottom: 14 }}>
        <NetCard icon="Wifi"   label="Wi-Fi SSID" value={device.wifi} />
        <NetCard icon="Globe"  label="Device IP"  value={device.ip} />
        <NetCard icon="Code"   label="MAC"        value={device.mac} />
        <NetCard icon="Bolt"   label="Gateway"    value="192.168.1.1" />
      </div>

      <div className="card">
        <div className="card-header">
          <Icons.Network size={13} className="muted" />
          <span className="title">Interfaces</span>
        </div>
        <table className="table">
          <thead>
            <tr>
              <th style={{ paddingLeft: 14 }}>Iface</th>
              <th>IPv4</th>
              <th>IPv6</th>
              <th>State</th>
              <th>RX / TX</th>
              <th className="actions">Notes</th>
            </tr>
          </thead>
          <tbody>
            <tr><td style={{ paddingLeft: 14 }} className="mono">wlan0</td><td className="mono">{device.ip}/24</td><td className="mono muted">fe80::9e6b:ff:fea1:427e/64</td><td><span className="badge ok"><span className="dot" />UP</span></td><td className="mono muted">218 MB / 41 MB</td><td className="actions muted mono">5 GHz · -52 dBm</td></tr>
            <tr><td style={{ paddingLeft: 14 }} className="mono">rmnet_data0</td><td className="mono">100.64.12.4/30</td><td className="mono muted">—</td><td><span className="badge ok"><span className="dot" />UP</span></td><td className="mono muted">8 MB / 1.2 MB</td><td className="actions muted mono">LTE · 87 dBm</td></tr>
            <tr><td style={{ paddingLeft: 14 }} className="mono">lo</td><td className="mono">127.0.0.1/8</td><td className="mono">::1/128</td><td><span className="badge ok"><span className="dot" />UP</span></td><td className="mono muted">12 MB / 12 MB</td><td className="actions muted">loopback</td></tr>
            <tr><td style={{ paddingLeft: 14 }} className="mono">tun0</td><td className="mono muted">—</td><td className="mono muted">—</td><td><span className="badge"><span className="dot" />DOWN</span></td><td className="mono muted">—</td><td className="actions muted">VPN</td></tr>
          </tbody>
        </table>
      </div>
    </>
  );
}
function NetCard({ icon, label, value }) {
  const Cmp = Icons[icon];
  return (
    <div className="netcard">
      <div className="iconwrap"><Cmp size={18} /></div>
      <div style={{ minWidth: 0 }}>
        <div className="label">{label}</div>
        <div className="value" style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{value}</div>
      </div>
    </div>
  );
}

// ─── Proxy ──────────────────────────────────────────────────────────────────
function NetProxy({ device, devState, setDevState }) {
  const proxy = devState.proxy;
  const set = (patch) => setDevState({ proxy: { ...proxy, ...patch } });

  return (
    <div className="grid-2">
      <div className="card">
        <div className="card-header">
          <Icons.Shield size={13} className="muted" />
          <span className="title">Global HTTP / HTTPS proxy</span>
          <div className="spacer" style={{ flex: 1 }} />
          <span className={`badge ${proxy.enabled ? "ok" : ""}`}>
            <span className={proxy.enabled ? "dot" : ""} /> {proxy.enabled ? "active" : "off"}
          </span>
          <div className={`switch ${proxy.enabled ? "on" : ""}`} onClick={() => {
            const next = !proxy.enabled;
            set({ enabled: next });
            showToast({
              title: next ? "Proxy enabled" : "Proxy disabled",
              body: next ? `settings put global http_proxy ${proxy.host}:${proxy.port}` : "settings put global http_proxy :0",
              kind: next ? "ok" : "info", mono: true,
            });
          }} />
        </div>
        <div className="card-body col" style={{ gap: 12 }}>
          <div className="grid-2">
            <div className="field">
              <label>Host</label>
              <input className="input mono" value={proxy.host} onChange={e => set({ host: e.target.value })} placeholder="10.0.0.5" />
            </div>
            <div className="field">
              <label>Port</label>
              <input className="input mono" value={proxy.port} onChange={e => set({ port: e.target.value })} />
            </div>
          </div>
          <div className="field">
            <label>Exclude hosts</label>
            <input className="input mono" value={proxy.exclude} onChange={e => set({ exclude: e.target.value })} placeholder="localhost,127.0.0.1,*.internal" />
          </div>
          <div className="row" style={{ gap: 6, flexWrap: "wrap" }}>
            <span className="muted" style={{ fontSize: 11 }}>Quick set:</span>
            {[
              ["mitmproxy",   "127.0.0.1", "8080"],
              ["Burp local",  "127.0.0.1", "8888"],
              ["LAN Burp",    "192.168.1.10", "8080"],
              ["Charles",     "127.0.0.1", "8888"],
            ].map(([n, h, p]) => (
              <button key={n} className="tag-pill" style={{ cursor: "pointer" }}
                      onClick={() => set({ host: h, port: p })}>{n}</button>
            ))}
          </div>
          <div className="mono" style={{
            background: "var(--terminal-bg)", border: "1px solid var(--border)",
            borderRadius: 6, padding: "8px 10px", fontSize: 11.5, color: "var(--text-muted)",
            overflow: "auto", whiteSpace: "nowrap",
          }}>
            <span className="prompt-user">$</span> adb -s {device.id} shell settings put global http_proxy {proxy.host || "PROXY_HOST"}:{proxy.port}
          </div>
          <div className="row" style={{ gap: 6 }}>
            <button className="btn primary" onClick={() => showToast({ title: "Applied", body: "settings put global http_proxy ...", kind: "ok", mono: true })}>
              Apply changes
            </button>
            <button className="btn" onClick={() => showToast({ title: "CA cert installed", body: "mv burp-cert.0 /system/etc/security/cacerts/", kind: "ok", mono: true })}>
              <Icons.Shield className="icon" /> Install CA cert
            </button>
            <button className="btn ghost" onClick={() => set({ host: "", port: "8080", exclude: "localhost,127.0.0.1", enabled: false })}>
              Reset
            </button>
          </div>
        </div>
      </div>

      <div className="col" style={{ gap: 14 }}>
        <ClipboardSyncCard />
        <div className="card">
          <div className="card-header">
            <Icons.Shield size={13} className="muted" />
            <span className="title">CA certificates</span>
          </div>
          <div className="card-body col" style={{ gap: 8 }}>
            <CertRow name="mitmproxy CA"     fp="9E:6B:00:A1:42:7E:91:F3" trusted={true}  store="user" />
            <CertRow name="PortSwigger CA"   fp="A8:8C:3E:12:8B:1F:00:42" trusted={true}  store="system" />
            <CertRow name="Charles Proxy CA" fp="44:22:F1:7A:91:0E:CD:88" trusted={false} store="—" />
            <button className="btn" style={{ marginTop: 4, justifyContent: "center" }}>
              <Icons.Upload className="icon" /> Push new CA (.der / .pem)
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
function ClipboardSyncCard() {
  const [on, setOn] = useState(true);
  return (
    <div className="card">
      <div className="card-header">
        <Icons.Clipboard size={13} className="muted" />
        <span className="title">Clipboard sync</span>
        <div className="spacer" style={{ flex: 1 }} />
        <div className={`switch ${on ? "on" : ""}`} onClick={() => setOn(!on)} />
      </div>
      <div className="card-body col" style={{ gap: 10 }}>
        <div className="muted" style={{ fontSize: 11.5 }}>
          Two-way clipboard between host and device. {on ? "Active — polled every 800ms." : "Paused."}
        </div>
        <div className="spread">
          <span className="muted" style={{ fontSize: 11 }}>Last from device</span>
          <span className="mono">2m ago</span>
        </div>
        <div className="mono" style={{
          padding: "8px 10px", background: "var(--bg-inset)", borderRadius: 6, fontSize: 11.5,
          color: "var(--text-muted)", wordBreak: "break-all",
        }}>
          https://api.example.com/v2/scan/result/8c12a-payload-9f02
        </div>
        <div className="row" style={{ gap: 6 }}>
          <button className="btn sm"><Icons.Download className="icon" /> Pull</button>
          <button className="btn sm"><Icons.Upload className="icon" /> Push</button>
        </div>
      </div>
    </div>
  );
}
function CertRow({ name, fp, trusted, store }) {
  return (
    <div className="spread" style={{ padding: "5px 0" }}>
      <div className="row" style={{ gap: 10, minWidth: 0 }}>
        <Icons.Shield size={14} style={{ color: trusted ? "var(--ok)" : "var(--text-subtle)" }} />
        <div style={{ minWidth: 0 }}>
          <div style={{ fontWeight: 500, fontSize: 12 }}>{name}</div>
          <div className="mono subtle" style={{ fontSize: 10.5, overflow: "hidden", textOverflow: "ellipsis" }}>{fp}</div>
        </div>
      </div>
      <div className="row" style={{ gap: 6 }}>
        {trusted ? <span className={`badge ${store === "system" ? "accent" : "ok"}`}>{store}</span> : <span className="badge">not installed</span>}
        <button className="iconbtn"><Icons.Trash size={11} /></button>
      </div>
    </div>
  );
}

Object.assign(window, { NetworkScreen });
