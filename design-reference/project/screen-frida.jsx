// Frida server management — real start/stop, push binary
function FridaScreen({ device, devState, setDevState }) {
  const [port, setPort] = useState(devState.frida?.port || 27042);
  const [iface, setIface] = useState(devState.frida?.iface || "0.0.0.0");
  const [active, setActive] = useState("16.4.7"); // version of running server, or null

  useEffect(() => {
    setDevState({ frida: { iface, port } });
  }, [iface, port]);

  const stop = () => {
    setActive(null);
    showToast({ title: "frida-server stopped", body: `killall frida-server on ${device.label}`, kind: "info", mono: true });
  };
  const start = (version) => {
    setActive(version);
    showToast({ title: `frida-server ${version} started`, body: `listening on ${iface}:${port}`, kind: "ok", mono: true });
  };

  return (
    <div className="screen">
      <div className="screen-header">
        <h1><Icons.Bug size={17} /> Frida</h1>
        <span className="subtitle muted">/data/local/tmp · {MOCK.FRIDA_SERVERS.length} binaries discovered</span>
        <div className="spacer" />
        <button className="btn ghost" onClick={() => showToast({ title: "Re-scanned", body: "ls -la /data/local/tmp/frida-server-*", kind: "info", mono: true })}>
          <Icons.Refresh className="icon" /> Re-scan
        </button>
        <button className="btn" onClick={() => showToast({ title: "Push frida-server", body: "open file dialog…", kind: "info" })}>
          <Icons.Upload className="icon" /> Push binary
        </button>
      </div>

      <div className="screen-body">
        <div className="card" style={{ marginBottom: 14 }}>
          <div className="card-header">
            {active
              ? <><span className="pulse" /><span className="title">Active session</span></>
              : <><span className="title">Inactive</span></>}
            <div className="spacer" style={{ flex: 1 }} />
            {active ? (
              <>
                <span className="badge ok"><span className="dot" /> running · pid 4421</span>
                <span className="badge accent">v{active}</span>
                <span className="badge"><Icons.Globe size={10} /> {iface}:{port}</span>
              </>
            ) : (
              <span className="badge"><span className="dot" /> no server running</span>
            )}
          </div>
          <div className="card-body">
            <div className="row" style={{ gap: 14, alignItems: "stretch" }}>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div className="muted" style={{ fontSize: 11, marginBottom: 4 }}>Command</div>
                <div className="mono" style={{
                  background: "var(--terminal-bg)", border: "1px solid var(--border)",
                  borderRadius: 6, padding: "8px 10px", fontSize: 11.5, color: "var(--text)",
                  whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis",
                }}>
                  <span className="prompt-root">root#</span> ./frida-server-{active || "X.X.X"}-android-arm64 -l {iface}:{port}
                </div>
              </div>
              <div className="row" style={{ gap: 6, alignItems: "flex-end" }}>
                <div className="field">
                  <label>Interface</label>
                  <input className="input mono" value={iface} onChange={e => setIface(e.target.value)} style={{ width: 110 }} />
                </div>
                <div className="field">
                  <label>Port</label>
                  <input className="input mono" type="number" value={port} onChange={e => setPort(+e.target.value)} style={{ width: 86 }} />
                </div>
                {active ? (
                  <>
                    <button className="btn danger" onClick={stop}><Icons.Stop className="icon" /> Stop</button>
                    <button className="btn" onClick={() => { stop(); setTimeout(() => start(active), 350); }}>
                      <Icons.Refresh className="icon" /> Restart
                    </button>
                  </>
                ) : (
                  <button className="btn primary" onClick={() => start("16.4.7")}>
                    <Icons.Play className="icon" /> Start
                  </button>
                )}
              </div>
            </div>
          </div>
        </div>

        <div className="card" style={{ marginBottom: 14 }}>
          <div className="card-header">
            <Icons.File size={13} className="muted" />
            <span className="title">Servers in /data/local/tmp</span>
            <div className="spacer" style={{ flex: 1 }} />
            <span className="muted" style={{ fontSize: 11 }}>{MOCK.FRIDA_SERVERS.length} binaries</span>
          </div>
          <div className="card-body col" style={{ gap: 8 }}>
            {MOCK.FRIDA_SERVERS.map(s => {
              const isActive = active === s.version;
              return (
                <div key={s.name} className={`frida-server-row ${isActive ? "active" : ""}`}>
                  <div>
                    <div className="meta-row">
                      <span style={{ fontWeight: 600, fontSize: 12.5 }}>frida-server</span>
                      <span className="badge accent">v{s.version}</span>
                      <span className="badge">{s.arch}</span>
                      {isActive && <span className="badge ok"><span className="pulse" /> running · pid 4421</span>}
                    </div>
                    <div className="filename">{s.name}</div>
                  </div>
                  <span className="mono muted" style={{ fontSize: 11, textAlign: "right" }}>{s.size.toFixed(1)} MB</span>
                  <span className="mono muted" style={{ fontSize: 11 }}>{s.perms}</span>
                  <div className="row" style={{ gap: 4 }}>
                    {isActive
                      ? <button className="btn danger sm" onClick={stop}><Icons.Stop className="icon" /> Stop</button>
                      : <button className="btn primary sm" onClick={() => start(s.version)}><Icons.Play className="icon" /> Start</button>}
                    <button className="iconbtn" title="Delete binary"
                            onClick={() => showToast({ title: "Removed", body: `rm /data/local/tmp/${s.name}`, kind: "info", mono: true })}>
                      <Icons.Trash size={12} />
                    </button>
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        <div className="grid-2">
          <div className="card">
            <div className="card-header">
              <Icons.Code size={13} className="muted" />
              <span className="title">Attached processes</span>
              <div className="spacer" style={{ flex: 1 }} />
              <button className="btn ghost sm" onClick={() => showToast({ title: "Attach to process", body: "open process picker…", kind: "info" })}>
                <Icons.Plus className="icon" /> Attach
              </button>
            </div>
            <div className="card-body col" style={{ gap: 8 }}>
              {!active ? (
                <div style={{ textAlign: "center", padding: 24, color: "var(--text-subtle)" }}>
                  Start a frida-server above to attach to processes.
                </div>
              ) : MOCK.FRIDA_PROCS.map(p => (
                <div key={p.pid} style={{
                  padding: 10, borderRadius: 8,
                  background: p.attached ? "var(--accent-soft)" : "var(--panel-2)",
                  border: "1px solid " + (p.attached ? "var(--accent)" : "var(--border)"),
                }}>
                  <div className="spread">
                    <div className="row" style={{ gap: 8, minWidth: 0 }}>
                      <Icons.Code size={13} style={{ color: p.attached ? "var(--accent)" : "var(--text-muted)" }} />
                      <span style={{ fontWeight: 500, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{p.name}</span>
                      <span className="badge">pid {p.pid}</span>
                      {p.attached && <span className="badge accent"><span className="pulse" /> attached</span>}
                    </div>
                    <div className="row" style={{ gap: 4 }}>
                      <button className="iconbtn" title="View log"><Icons.Logcat size={12} /></button>
                      <button className="iconbtn" title="Detach"><Icons.Close size={12} /></button>
                    </div>
                  </div>
                  {p.script && (
                    <div className="mono" style={{ marginTop: 6, fontSize: 11, color: "var(--text-muted)" }}>
                      <span style={{ color: "var(--accent)" }}>▸</span> {p.script}
                    </div>
                  )}
                </div>
              ))}
            </div>
          </div>

          <div className="card">
            <div className="card-header">
              <Icons.File size={13} className="muted" />
              <span className="title">Script library</span>
            </div>
            <div className="card-body col" style={{ gap: 4 }}>
              {[
                ["SSL pinning bypass", "bypass_ssl_pin.js"],
                ["Root detection bypass", "root_detect_bypass.js"],
                ["Trace Java method", "trace_method.js"],
                ["Dump SharedPreferences", "dump_prefs.js"],
                ["WebView debug enabler", "webview_debug.js"],
                ["Anti-emulator bypass", "anti_emulator.js"],
              ].map(([n, f]) => (
                <div key={f} className="spread" style={{ padding: "5px 4px", fontSize: 12 }}>
                  <div className="row" style={{ gap: 8 }}>
                    <Icons.File size={12} className="muted" /> {n}
                  </div>
                  <div className="row" style={{ gap: 8 }}>
                    <span className="mono subtle" style={{ fontSize: 10.5 }}>{f}</span>
                    <button className="btn ghost sm" disabled={!active}
                            onClick={() => showToast({ title: "Script running", body: `frida -U -l ${f}`, kind: "ok", mono: true })}>
                      <Icons.Play className="icon" />
                    </button>
                  </div>
                </div>
              ))}
              <button className="btn ghost sm" style={{ marginTop: 6, justifyContent: "center" }}>
                <Icons.Plus className="icon" /> Add script
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

Object.assign(window, { FridaScreen });
