// Network — Hosts / DNS / Capture / Connections subsections

// ─── Host overrides (DNS) ───────────────────────────────────────────────────
// Real talk: /system/etc/hosts is on the verified-boot system partition and
// CANNOT be modified on a stock device. The realistic options are:
//   1. Magisk module overlay  (root, persistent)        — recommended on rooted
//   2. Bind-mount tmpfs hosts (root, until reboot)      — fastest to apply
//   3. Private DNS resolver   (no root, Android 9+)     — host runs DoT server
// This panel exposes all three and explains the trade-offs.
const HOST_STRATEGIES = {
  magisk: {
    label: "Magisk module",
    sub: "root · persistent",
    needsRoot: true,
    persistent: true,
    explain: "Generates a systemless Magisk module that overlays /system/etc/hosts. Survives reboot. Recommended for rooted devices with Magisk.",
    cmd: (host, ip) => `# adbq generates /data/adb/modules/adbq-hosts/system/etc/hosts and reloads Magisk`,
  },
  bindmount: {
    label: "Bind-mount",
    sub: "root · lost on reboot",
    needsRoot: true,
    persistent: false,
    explain: "Writes /data/local/tmp/hosts.adbq then bind-mounts it over /system/etc/hosts. Fast, no Magisk needed, but resets on reboot. adbq re-applies automatically when the device reconnects.",
    cmd: (host, ip) => `mount --bind /data/local/tmp/hosts.adbq /system/etc/hosts`,
  },
  privatedns: {
    label: "Private DNS",
    sub: "no root · Android 9+",
    needsRoot: false,
    persistent: true,
    explain: "Runs a local DNS-over-TLS resolver on your host, then points the device's Private DNS at it via `settings put global private_dns_specifier`. No root, but only works on Wi-Fi and adds ~5ms per query.",
    cmd: (host, ip) => `settings put global private_dns_specifier adbq.local`,
  },
};

function NetHosts({ device, devState, setDevState }) {
  const hosts = devState.hosts || [];
  const strategy = devState.hostsStrategy || (device.root ? "magisk" : "privatedns");
  const setStrategy = (s) => setDevState({ hostsStrategy: s });

  const [showAdd, setShowAdd] = useState(false);
  const strat = HOST_STRATEGIES[strategy];
  const blocked = strat.needsRoot && !device.root;

  const remove = (i) => {
    setDevState({ hosts: hosts.filter((_, idx) => idx !== i) });
    showToast({ title: "Removed mapping", kind: "info" });
  };
  const toggle = (i) => {
    const next = hosts.map((h, idx) => idx === i ? { ...h, enabled: !h.enabled } : h);
    setDevState({ hosts: next });
  };

  return (
    <>
      <div className="card" style={{ marginBottom: 14 }}>
        <div className="card-header">
          <Icons.Shield size={13} className="muted" />
          <span className="title">Strategy</span>
          <span className="muted" style={{ fontSize: 11, marginLeft: 6 }}>
            How adbq applies your overrides on this device
          </span>
        </div>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 0, borderTop: "1px solid var(--border)" }}>
          {Object.entries(HOST_STRATEGIES).map(([k, s], i) => {
            const active = strategy === k;
            const disabled = s.needsRoot && !device.root;
            return (
              <div key={k}
                   onClick={() => !disabled && setStrategy(k)}
                   style={{
                     padding: 14,
                     borderRight: i < 2 ? "1px solid var(--border)" : "none",
                     background: active ? "var(--accent-soft)" : undefined,
                     cursor: disabled ? "not-allowed" : "pointer",
                     opacity: disabled ? 0.5 : 1,
                     position: "relative",
                   }}>
                <div className="row" style={{ gap: 8, marginBottom: 4 }}>
                  <span style={{ fontWeight: 600, fontSize: 13, color: active ? "var(--accent-strong)" : undefined }}>
                    {s.label}
                  </span>
                  {active && <span className="badge accent">selected</span>}
                </div>
                <div className="mono subtle" style={{ fontSize: 10.5, marginBottom: 6 }}>{s.sub}</div>
                <div className="muted" style={{ fontSize: 11.5, lineHeight: 1.45 }}>{s.explain}</div>
                {disabled && (
                  <div className="badge warn" style={{ marginTop: 8 }}>
                    <Icons.Lock size={9} /> requires root
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>

      <div className="spread" style={{ marginBottom: 12 }}>
        <div className="muted" style={{ fontSize: 12, maxWidth: 560 }}>
          {hosts.length === 0
            ? "Mappings you add are saved per-device and applied when you click Apply."
            : `${hosts.filter(h => h.enabled).length} of ${hosts.length} mappings active. Apply pushes them via ${strat.label}.`}
        </div>
        <div className="row" style={{ gap: 6 }}>
          <button className="btn ghost"
                  onClick={() => showToast({ title: "Pulled /etc/hosts", body: "12 stock entries (Android defaults)", kind: "info", mono: true })}>
            <Icons.Download className="icon" /> Pull current
          </button>
          <button className="btn primary" onClick={() => setShowAdd(true)}>
            <Icons.Plus className="icon" /> Add mapping
          </button>
        </div>
      </div>

      <div className="card">
        {hosts.length === 0 ? (
          <div className="card-body" style={{ textAlign: "center", padding: 36, color: "var(--text-subtle)" }}>
            <Icons.Globe size={28} />
            <div style={{ marginTop: 10, fontSize: 13 }}>No mappings yet</div>
            <div style={{ fontSize: 11.5, marginTop: 4, maxWidth: 360, margin: "4px auto 0" }}>
              Add a hostname → IP mapping. The device will resolve that hostname to your chosen IP via the strategy above.
            </div>
            <button className="btn primary" style={{ marginTop: 12 }} onClick={() => setShowAdd(true)}>
              <Icons.Plus className="icon" /> Add your first mapping
            </button>
          </div>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th style={{ paddingLeft: 14, width: 60 }}>On</th>
                <th>Host</th>
                <th>Resolves to</th>
                <th>Note</th>
                <th className="actions"></th>
              </tr>
            </thead>
            <tbody>
              {hosts.map((h, i) => (
                <tr key={i} style={{ opacity: h.enabled ? 1 : 0.55 }}>
                  <td style={{ paddingLeft: 14 }}>
                    <div className={`switch ${h.enabled ? "on" : ""}`} onClick={() => toggle(i)} style={{ width: 26, height: 16 }} />
                  </td>
                  <td className="mono">{h.host}</td>
                  <td className="mono">
                    <span style={{ color: "var(--text-subtle)" }}>→ </span>
                    <span className="badge accent" style={{ fontFamily: "var(--font-mono)" }}>{h.ip}</span>
                  </td>
                  <td className="muted">{h.note || "—"}</td>
                  <td className="actions">
                    <div className="row" style={{ justifyContent: "flex-end", gap: 2 }}>
                      <button className="iconbtn" title="Test resolve"
                              onClick={() => showToast({ title: "Test resolve", body: `nslookup ${h.host} → ${h.ip} (via ${strat.label})`, kind: "ok", mono: true })}>
                        <Icons.Bolt size={12} />
                      </button>
                      <button className="iconbtn" title="Remove" onClick={() => remove(i)}>
                        <Icons.Trash size={12} />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {hosts.length > 0 && (
          <div style={{ padding: "10px 14px", borderTop: "1px solid var(--border)", display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
            <span className="muted" style={{ fontSize: 11 }}>
              Strategy: <span style={{ color: "var(--text)" }}>{strat.label}</span>
              {!strat.persistent && <span className="badge warn" style={{ marginLeft: 6 }}>resets on reboot</span>}
              {blocked && <span className="badge err" style={{ marginLeft: 6 }}>requires root</span>}
            </span>
            <div className="spacer" style={{ flex: 1 }} />
            <button className="btn ghost" disabled={blocked}
                    onClick={() => showToast({ title: "Reverted", body: "umount /system/etc/hosts · DNS reset to default", kind: "info", mono: true })}>
              <Icons.Refresh className="icon" /> Revert on device
            </button>
            <button className="btn primary" disabled={blocked}
                    onClick={() => showToast({
                      title: blocked ? "Cannot apply" : "Applied",
                      body: blocked ? "device is not rooted" : `${strat.label} · ${hosts.filter(h => h.enabled).length} active mappings`,
                      kind: blocked ? "err" : "ok", mono: true,
                    })}>
              <Icons.Upload className="icon" /> Apply
            </button>
          </div>
        )}
      </div>

      <div className="card" style={{ marginTop: 14 }}>
        <div className="card-header">
          <Icons.Code size={13} className="muted" />
          <span className="title">Caveats</span>
        </div>
        <div className="card-body col" style={{ gap: 8, fontSize: 12, color: "var(--text-muted)", lineHeight: 1.55 }}>
          <div>
            <span style={{ color: "var(--text)" }}>Verified boot.</span> Modern Android ships /system as read-only with dm-verity. Direct writes to <span className="mono">/system/etc/hosts</span> aren't possible — adbq uses bind-mount or a Magisk module instead.
          </div>
          <div>
            <span style={{ color: "var(--text)" }}>App-bundled DNS.</span> Apps using DNS-over-HTTPS (Chrome, Cloudflare 1.1.1.1 app, some games) bypass system DNS. Use Network → <span style={{ color: "var(--accent)" }}>Proxy</span> + a CA cert to intercept those.
          </div>
          <div>
            <span style={{ color: "var(--text)" }}>IPv6.</span> If the app issues an AAAA query and you only mapped A, the device falls back to the real resolver. Map both records for predictable behaviour.
          </div>
          <div>
            <span style={{ color: "var(--text)" }}>Persistence.</span> Bind-mount entries are lost on reboot; adbq tries to re-apply them whenever the device reconnects, but a reboot before reconnect leaks the mapping for a few seconds.
          </div>
        </div>
      </div>

      <AddHostModal open={showAdd} onClose={() => setShowAdd(false)}
                    onAdd={(h) => {
                      setDevState({ hosts: [...hosts, h] });
                      showToast({ title: "Mapping added", body: `${h.host} → ${h.ip}`, kind: "ok", mono: true });
                    }} />
    </>
  );
}
function AddHostModal({ open, onClose, onAdd }) {
  const [host, setHost] = useState("");
  const [ip, setIp] = useState("");
  const [note, setNote] = useState("");
  const submit = () => {
    if (!host || !ip) return;
    onAdd({ host, ip, note, enabled: true });
    setHost(""); setIp(""); setNote("");
    onClose();
  };
  return (
    <Modal open={open} onClose={onClose} title="New host mapping" width={460}
      footer={<>
        <button className="btn ghost" onClick={onClose}>Cancel</button>
        <button className="btn primary" disabled={!host || !ip} onClick={submit}>
          <Icons.Plus className="icon" /> Add
        </button>
      </>}>
      <div className="col" style={{ gap: 12 }}>
        <div className="field">
          <label>Hostname</label>
          <input className="input mono" value={host} onChange={e => setHost(e.target.value)} placeholder="api.staging.example.com" autoFocus />
        </div>
        <div className="field">
          <label>Resolves to (IPv4 / IPv6)</label>
          <input className="input mono" value={ip} onChange={e => setIp(e.target.value)} placeholder="10.0.0.42" />
        </div>
        <div className="field">
          <label>Note (optional)</label>
          <input className="input" value={note} onChange={e => setNote(e.target.value)} placeholder="Why this mapping exists" />
        </div>
        <div className="mono" style={{ background: "var(--terminal-bg)", border: "1px solid var(--border)",
                                       borderRadius: 6, padding: "8px 10px", fontSize: 11.5, color: "var(--text-muted)" }}>
          <span className="muted"># bind-mount mode:</span><br />
          <span className="prompt-root">root#</span> echo "{ip || "<ip>"} {host || "<host>"}" &gt;&gt; /data/local/tmp/hosts.adbq
        </div>
      </div>
    </Modal>
  );
}

// ─── DNS log ────────────────────────────────────────────────────────────────
function NetDns({ device }) {
  const [q, setQ] = useState("");
  const [errOnly, setErrOnly] = useState(false);
  const [paused, setPaused] = useState(false);
  const [visible, setVisible] = useState(50);

  useEffect(() => {
    if (paused) return;
    const id = setInterval(() => setVisible(v => Math.min(MOCK.DNS_QUERIES.length, v + 8)), 1100);
    return () => clearInterval(id);
  }, [paused]);

  const all = useMemo(() => MOCK.DNS_QUERIES.slice(0, visible), [visible]);
  const filtered = useMemo(() => all.filter(d => {
    if (errOnly && !d.err) return false;
    if (!q) return true;
    return (d.host + " " + d.answer + " " + d.proc).toLowerCase().includes(q.toLowerCase());
  }), [all, q, errOnly]);

  const cols = "110px 60px minmax(0,2.2fr) minmax(0,1.4fr) 60px minmax(0,1.6fr) 70px";

  return (
    <>
      <div className="spread" style={{ marginBottom: 12, gap: 8, flexWrap: "wrap" }}>
        <div className="row" style={{ gap: 8 }}>
          <div className="search-wrap">
            <Icons.Search size={13} />
            <input className="input" placeholder="search host, IP, app…" value={q} onChange={e => setQ(e.target.value)} style={{ width: 260 }} />
          </div>
          <button className={`btn ghost`} onClick={() => setErrOnly(!errOnly)}
                  style={{ color: errOnly ? "var(--err)" : undefined }}>
            errors only
          </button>
        </div>
        <div className="row" style={{ gap: 6 }}>
          <span className="muted" style={{ fontSize: 11 }}>{filtered.length} of {all.length} (buffer {MOCK.DNS_QUERIES.length})</span>
          <button className="btn ghost" onClick={() => setPaused(!paused)}>
            {paused ? <><Icons.Play className="icon" /> Resume</> : <><Icons.Pause className="icon" /> Pause</>}
          </button>
          <button className="btn ghost" onClick={() => { setVisible(0); showToast({ title: "DNS log cleared", kind: "info" }); }}>
            <Icons.Trash className="icon" /> Clear
          </button>
          <button className="btn" onClick={() => showToast({ title: "Exported", body: "~/adbq-logs/dns.csv", kind: "ok", mono: true })}>
            <Icons.Download className="icon" /> Export
          </button>
        </div>
      </div>

      <div className="dt" style={{ flex: 1, minHeight: 360 }}>
        <div className="dt-head" style={{ gridTemplateColumns: cols }}>
          <div>Time</div><div>Type</div><div>Host</div><div>Answer</div><div>RTT</div><div>App</div><div>Source</div>
        </div>
        {filtered.length === 0 ? (
          <div style={{ padding: 36, textAlign: "center", color: "var(--text-subtle)", fontSize: 12 }}>
            No queries match.
          </div>
        ) : (
          <VirtualList
            className="dt-body"
            items={filtered}
            itemHeight={32}
            followBottom={!paused}
            renderItem={(d) => (
              <div className={`dt-row ${d.err ? "err" : ""}`} style={{ gridTemplateColumns: cols }}>
                <div className="mono muted">{d.time}</div>
                <div className="mono"><span className="badge">{d.type}</span></div>
                <div className="mono" style={{ fontWeight: 500 }}>{d.host}</div>
                <div className="mono">
                  {d.err
                    ? <span className="badge err"><span className="dot" /> {d.answer}</span>
                    : <span style={{ color: "var(--accent)" }}>{d.answer}</span>}
                </div>
                <div className="mono muted">{d.ms}ms</div>
                <div className="mono muted">{d.proc}</div>
                <div>
                  {d.cached
                    ? <span className="badge">cache</span>
                    : <span className="badge ok"><span className="dot" /> net</span>}
                </div>
              </div>
            )}
          />
        )}
        <div className="dt-foot">
          <span className="mono">tcpdump -i any -n -s 0 udp port 53</span>
          <div className="spacer" style={{ flex: 1 }} />
          <span>only visible rows are rendered</span>
          {!paused && <span className="badge ok"><span className="pulse" /> streaming</span>}
        </div>
      </div>
    </>
  );
}

// ─── Packet capture ─────────────────────────────────────────────────────────
function NetCapture({ device, devState, setDevState }) {
  const cap = devState.capture || { iface: "wlan0", filter: "", sslDump: true };
  const [recording, setRecording] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  const [pkts, setPkts] = useState(0);

  useEffect(() => {
    if (!recording) return;
    const id = setInterval(() => {
      setElapsed(e => e + 1);
      setPkts(p => p + Math.floor(8 + Math.random() * 28));
    }, 1000);
    return () => clearInterval(id);
  }, [recording]);

  const stop = () => {
    setRecording(false);
    const name = `capture-${new Date().toISOString().slice(11, 19).replace(/:/g, "")}.pcap`;
    showToast({ title: "Capture saved", body: `${name} · ${pkts} packets · ${(pkts * 320 / 1024).toFixed(1)} KB`, kind: "ok", mono: true });
    setElapsed(0); setPkts(0);
  };

  return (
    <div className="col" style={{ gap: 14 }}>
      <div className="card">
        <div className="card-header">
          {recording
            ? <><span className="pulse" style={{ background: "var(--err)" }} /><span className="title">Recording</span></>
            : <span className="title">Packet capture</span>}
          <div className="spacer" style={{ flex: 1 }} />
          {recording && (
            <>
              <span className="badge err"><span className="dot" /> live</span>
              <span className="mono muted" style={{ fontSize: 11 }}>{pkts} pkts · {fmtDur(elapsed)}</span>
            </>
          )}
        </div>
        <div className="card-body">
          <div className="row" style={{ gap: 14, alignItems: "flex-end", flexWrap: "wrap" }}>
            <div className="field" style={{ minWidth: 130 }}>
              <label>Interface</label>
              <select className="input mono" value={cap.iface}
                      onChange={e => setDevState({ capture: { ...cap, iface: e.target.value } })}>
                {["any", "wlan0", "rmnet_data0", "lo", "tun0"].map(i => <option key={i}>{i}</option>)}
              </select>
            </div>
            <div className="field" style={{ flex: 1, minWidth: 240 }}>
              <label>BPF filter</label>
              <input className="input mono" value={cap.filter}
                     onChange={e => setDevState({ capture: { ...cap, filter: e.target.value } })}
                     placeholder="host api.example.com and tcp port 443" />
            </div>
            <div className="row" style={{ gap: 8, alignItems: "center", padding: "0 4px 6px" }}>
              <div className={`switch ${cap.sslDump ? "on" : ""}`}
                   onClick={() => setDevState({ capture: { ...cap, sslDump: !cap.sslDump } })} />
              <span style={{ fontSize: 12 }}>SSLKEYLOGFILE</span>
            </div>
            <div className="row" style={{ gap: 6 }}>
              {recording ? (
                <button className="btn danger" onClick={stop}>
                  <Icons.Stop className="icon" /> Stop
                </button>
              ) : (
                <button className="btn primary" onClick={() => setRecording(true)}>
                  <Icons.Play className="icon" /> Start capture
                </button>
              )}
            </div>
          </div>

          <div className="row" style={{ gap: 6, marginTop: 12, flexWrap: "wrap" }}>
            <span className="muted" style={{ fontSize: 11 }}>BPF presets:</span>
            {[
              ["all HTTPS", "tcp port 443"],
              ["all HTTP", "tcp port 80 or tcp port 8080"],
              ["only DNS", "udp port 53"],
              ["scanner app", "host api.example.com"],
              ["exclude local", "not host 192.168.1.0/24"],
            ].map(([n, f]) => (
              <button key={n} className="tag-pill" style={{ cursor: "pointer" }}
                      onClick={() => setDevState({ capture: { ...cap, filter: f } })}>{n}</button>
            ))}
          </div>
          <div className="mono" style={{
            marginTop: 12, padding: "8px 10px",
            background: "var(--terminal-bg)", border: "1px solid var(--border)",
            borderRadius: 6, fontSize: 11.5, color: "var(--text-muted)",
            overflow: "auto", whiteSpace: "nowrap",
          }}>
            <span className="prompt-root">root#</span> tcpdump -i {cap.iface} -s 0 -w /sdcard/capture.pcap {cap.filter || ""}
          </div>
        </div>
      </div>

      <div className="card">
        <div className="card-header">
          <Icons.File size={13} className="muted" />
          <span className="title">Saved captures</span>
          <div className="spacer" style={{ flex: 1 }} />
          <span className="muted" style={{ fontSize: 11 }}>{MOCK.CAPTURES.length} files · total {(MOCK.CAPTURES.reduce((a, c) => a + c.size, 0) / 1024 / 1024).toFixed(1)} MB</span>
        </div>
        <table className="table">
          <thead>
            <tr>
              <th style={{ paddingLeft: 14 }}>Name</th>
              <th>Packets</th>
              <th>Size</th>
              <th>Started</th>
              <th>Duration</th>
              <th>Filter</th>
              <th className="actions"></th>
            </tr>
          </thead>
          <tbody>
            {MOCK.CAPTURES.map((c, i) => (
              <tr key={i}>
                <td style={{ paddingLeft: 14 }}>
                  <span className="row" style={{ gap: 8 }}>
                    <Icons.File size={12} className="muted" />
                    <span style={{ fontWeight: 500 }}>{c.name}</span>
                  </span>
                </td>
                <td className="mono muted">{c.packets.toLocaleString()}</td>
                <td className="mono muted">{(c.size / 1024 / 1024).toFixed(2)} MB</td>
                <td className="mono muted">{c.started}</td>
                <td className="mono muted">{c.duration}</td>
                <td className="mono subtle" style={{ fontSize: 11 }}>{c.filter}</td>
                <td className="actions">
                  <div className="row" style={{ justifyContent: "flex-end", gap: 2 }}>
                    <button className="iconbtn" title="Open in Wireshark"
                            onClick={() => showToast({ title: "Opening in Wireshark…", body: c.name, kind: "info" })}>
                      <Icons.Eye size={12} />
                    </button>
                    <button className="iconbtn" title="Pull"><Icons.Download size={12} /></button>
                    <button className="iconbtn" title="Delete"><Icons.Trash size={12} /></button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
function fmtDur(s) { const m = Math.floor(s / 60); const r = s % 60; return `${m}:${r.toString().padStart(2, "0")}`; }

// ─── Connections ────────────────────────────────────────────────────────────
function NetConnections({ device }) {
  const [q, setQ] = useState("");
  const [proto, setProto] = useState("all");
  const filtered = useMemo(() => MOCK.NET_CONNECTIONS.filter(c => {
    if (proto !== "all" && c.proto !== proto) return false;
    if (!q) return true;
    return (c.local + " " + c.remote + " " + c.proc).toLowerCase().includes(q.toLowerCase());
  }), [q, proto]);

  const cols = "70px minmax(0,1.2fr) minmax(0,1.2fr) 130px minmax(0,1.3fr) minmax(0,1fr) 70px";

  return (
    <>
      <div className="spread" style={{ marginBottom: 12 }}>
        <div className="row" style={{ gap: 8 }}>
          <div className="search-wrap">
            <Icons.Search size={13} />
            <input className="input" placeholder="search ip, port, app…" value={q} onChange={e => setQ(e.target.value)} style={{ width: 260 }} />
          </div>
          <div className="row" style={{ background: "var(--bg-elev)", border: "1px solid var(--border)", borderRadius: 6, padding: 2, gap: 0 }}>
            {["all", "tcp", "udp"].map(p => (
              <button key={p} className={`iconbtn ${proto === p ? "active" : ""}`}
                      style={{ width: "auto", padding: "0 10px", fontSize: 11, fontWeight: 600, textTransform: "uppercase" }}
                      onClick={() => setProto(p)}>{p}</button>
            ))}
          </div>
        </div>
        <div className="row" style={{ gap: 6 }}>
          <span className="muted" style={{ fontSize: 11 }}>{filtered.length} of {MOCK.NET_CONNECTIONS.length} sockets</span>
          <button className="btn ghost"><Icons.Refresh className="icon" /> Refresh</button>
        </div>
      </div>

      <div className="dt" style={{ flex: 1, minHeight: 360 }}>
        <div className="dt-head" style={{ gridTemplateColumns: cols }}>
          <div>Proto</div><div>Local</div><div>Remote</div><div>State</div><div>App / PID</div><div>RX / TX</div><div></div>
        </div>
        <VirtualList
          className="dt-body"
          items={filtered}
          itemHeight={32}
          renderItem={(c) => (
            <div className="dt-row" style={{ gridTemplateColumns: cols }}>
              <div className="mono"><span className="badge">{c.proto}</span></div>
              <div className="mono">{c.local}</div>
              <div className="mono">
                <span style={{ color: "var(--text-subtle)" }}>→ </span>{c.remote}
              </div>
              <div>
                {c.state === "ESTABLISHED" ? <span className="badge ok"><span className="dot" />{c.state}</span>
                : c.state === "LISTEN"      ? <span className="badge info"><span className="dot" />{c.state}</span>
                : c.state === "TIME_WAIT"   ? <span className="badge warn">{c.state}</span>
                :                              <span className="badge">{c.state}</span>}
              </div>
              <div className="muted mono" style={{ fontSize: 11 }}>{c.proc} <span className="subtle">· {c.pid}</span></div>
              <div className="mono muted">{c.rx} / {c.tx}</div>
              <div className="row" style={{ gap: 2, justifyContent: "flex-end" }}>
                <button className="iconbtn" title="WHOIS"
                        onClick={() => showToast({ title: "whois " + c.remote.split(":")[0], body: "Amazon Technologies · us-west-2", kind: "info", mono: true })}>
                  <Icons.Eye size={12} />
                </button>
              </div>
            </div>
          )}
        />
        <div className="dt-foot">
          <span className="mono">cat /proc/net/tcp /proc/net/udp · refreshed every 2s</span>
          <div className="spacer" style={{ flex: 1 }} />
          <span>only visible rows are rendered</span>
          <span className="badge ok"><span className="pulse" /> live</span>
        </div>
      </div>
    </>
  );
}

Object.assign(window, { NetHosts, NetDns, NetCapture, NetConnections });
