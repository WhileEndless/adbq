// Settings modal — preferences + saved devices
function SettingsModal({ open, onClose, theme, setTheme, accent, setAccent }) {
  const [section, setSection] = useState("general");
  const [saved, setSaved] = useState(() => listSavedDevices());
  const refresh = () => setSaved(listSavedDevices());

  if (!open) return null;
  return (
    <div className="modal-backdrop" onMouseDown={onClose}>
      <div className="modal" style={{ width: 720, height: 540, display: "flex", flexDirection: "column" }} onMouseDown={e => e.stopPropagation()}>
        <div className="modal-header">
          <div className="title row" style={{ gap: 8 }}><Icons.Settings size={14} /> Settings</div>
          <button className="iconbtn" onClick={onClose}><Icons.Close size={13} /></button>
        </div>
        <div style={{ display: "grid", gridTemplateColumns: "180px 1fr", flex: 1, minHeight: 0 }}>
          <div style={{ borderRight: "1px solid var(--border)", padding: 8, display: "flex", flexDirection: "column", gap: 1 }}>
            {[
              ["general",   "General",       "Settings"],
              ["appearance", "Appearance",   "Sun"],
              ["adb",       "ADB server",    "Terminal"],
              ["devices",   "Saved devices", "Phone"],
              ["shortcuts", "Shortcuts",     "Code"],
              ["about",     "About",         "Shield"],
            ].map(([id, name, icon]) => {
              const Cmp = Icons[icon];
              const active = section === id;
              return (
                <div key={id} onClick={() => setSection(id)}
                     className={`nav ${active ? "active" : ""}`}
                     style={{
                       display: "flex", alignItems: "center", gap: 8, padding: "6px 8px",
                       borderRadius: 6, cursor: "pointer", fontSize: 12.5,
                       background: active ? "var(--accent-soft)" : undefined,
                       color: active ? "var(--accent-strong)" : "var(--text-muted)",
                     }}>
                  <Cmp size={14} /> {name}
                </div>
              );
            })}
          </div>
          <div style={{ overflow: "auto", padding: 18 }}>
            {section === "general" && <GeneralSettings />}
            {section === "appearance" && <AppearanceSettings theme={theme} setTheme={setTheme} accent={accent} setAccent={setAccent} />}
            {section === "adb" && <AdbSettings />}
            {section === "devices" && <DevicesSettings saved={saved} onForget={(id) => { forgetDevice(id); refresh(); showToast({ title: "Forgotten", body: id, kind: "info", mono: true }); }} />}
            {section === "shortcuts" && <ShortcutsSettings />}
            {section === "about" && <AboutSettings />}
          </div>
        </div>
      </div>
    </div>
  );
}

function SettingRow({ title, sub, children }) {
  return (
    <div className="spread" style={{ padding: "10px 0", borderBottom: "1px solid var(--border)" }}>
      <div style={{ minWidth: 0 }}>
        <div style={{ fontSize: 13, fontWeight: 500 }}>{title}</div>
        {sub && <div className="muted" style={{ fontSize: 11.5, marginTop: 2 }}>{sub}</div>}
      </div>
      <div>{children}</div>
    </div>
  );
}

function GeneralSettings() {
  const [autoConnect, setAutoConnect] = useState(true);
  const [startMin, setStartMin] = useState(false);
  const [tailMax, setTailMax] = useState(10000);
  return (
    <div>
      <div className="muted" style={{ fontSize: 11, textTransform: "uppercase", letterSpacing: "0.04em", fontWeight: 600, marginBottom: 4 }}>General</div>
      <SettingRow title="Auto-connect known devices" sub="Reconnect saved devices when they appear">
        <div className={`switch ${autoConnect ? "on" : ""}`} onClick={() => setAutoConnect(!autoConnect)} />
      </SettingRow>
      <SettingRow title="Start minimized" sub="Launch into the system tray on login">
        <div className={`switch ${startMin ? "on" : ""}`} onClick={() => setStartMin(!startMin)} />
      </SettingRow>
      <SettingRow title="Logcat buffer cap" sub="Lines kept in memory per device">
        <input className="input mono" type="number" value={tailMax} onChange={e => setTailMax(+e.target.value)} style={{ width: 100 }} />
      </SettingRow>
      <SettingRow title="Default screenshot folder" sub="Where Overview › Screenshot saves to">
        <button className="btn">~/adbq-shots/</button>
      </SettingRow>
    </div>
  );
}

function AppearanceSettings({ theme, setTheme, accent, setAccent }) {
  const accents = ["#a07cf7", "#7aa2ff", "#5ed29a", "#e9b454", "#ec6a73", "#c5a3ff"];
  return (
    <div>
      <div className="muted" style={{ fontSize: 11, textTransform: "uppercase", letterSpacing: "0.04em", fontWeight: 600, marginBottom: 4 }}>Appearance</div>
      <SettingRow title="Theme" sub="Follow your OS, or override per-device">
        <div className="row" style={{ background: "var(--bg-elev)", border: "1px solid var(--border)", borderRadius: 6, padding: 2 }}>
          {[["light", "Light"], ["dark", "Dark"], ["system", "System"]].map(([v, n]) => (
            <button key={v} onClick={() => setTheme(v)}
                    style={{
                      padding: "4px 12px", borderRadius: 4, fontSize: 12, fontWeight: 500,
                      background: theme === v ? "var(--panel)" : "transparent",
                      color: theme === v ? "var(--text)" : "var(--text-muted)",
                      border: theme === v ? "1px solid var(--border)" : "1px solid transparent",
                    }}>{n}</button>
          ))}
        </div>
      </SettingRow>
      <SettingRow title="Accent color" sub="Used for highlights and active states">
        <div className="row" style={{ gap: 6 }}>
          {accents.map(c => (
            <div key={c} onClick={() => setAccent(c)}
                 style={{
                   width: 22, height: 22, borderRadius: 6,
                   background: c, cursor: "pointer",
                   boxShadow: accent === c ? `0 0 0 2px var(--bg), 0 0 0 4px ${c}` : "none",
                 }} />
          ))}
        </div>
      </SettingRow>
      <SettingRow title="Density" sub="Vertical padding throughout the UI">
        <div className="row" style={{ background: "var(--bg-elev)", border: "1px solid var(--border)", borderRadius: 6, padding: 2 }}>
          {[["compact", "Compact"], ["balanced", "Balanced"], ["comfy", "Comfy"]].map(([v, n]) => (
            <button key={v} style={{
              padding: "4px 12px", borderRadius: 4, fontSize: 12, fontWeight: 500,
              background: v === "balanced" ? "var(--panel)" : "transparent",
              color: v === "balanced" ? "var(--text)" : "var(--text-muted)",
              border: v === "balanced" ? "1px solid var(--border)" : "1px solid transparent",
            }}>{n}</button>
          ))}
        </div>
      </SettingRow>
    </div>
  );
}

function AdbSettings() {
  return (
    <div>
      <div className="muted" style={{ fontSize: 11, textTransform: "uppercase", letterSpacing: "0.04em", fontWeight: 600, marginBottom: 4 }}>ADB server</div>
      <SettingRow title="adb binary" sub="Path to the adb executable">
        <input className="input mono" defaultValue="/opt/homebrew/bin/adb" style={{ width: 220 }} />
      </SettingRow>
      <SettingRow title="adb server port" sub="ADB control socket — default 5037">
        <input className="input mono" defaultValue="5037" style={{ width: 100 }} />
      </SettingRow>
      <SettingRow title="Restart adb-server on launch" sub="adb kill-server && adb start-server">
        <div className={`switch on`} />
      </SettingRow>
      <SettingRow title="Wireless ADB pairing" sub="Android 11+ · enables pairing dialog">
        <div className={`switch on`} />
      </SettingRow>
      <SettingRow title="mDNS discovery" sub="Auto-detect devices on the same network">
        <div className={`switch on`} />
      </SettingRow>
      <div style={{ marginTop: 14 }}>
        <button className="btn"><Icons.Refresh className="icon" /> Restart adb-server</button>
      </div>
    </div>
  );
}

function DevicesSettings({ saved, onForget }) {
  const all = MOCK.DEVICES;
  const persistedIds = new Set(saved.map(s => s.id));
  return (
    <div>
      <div className="muted" style={{ fontSize: 11, textTransform: "uppercase", letterSpacing: "0.04em", fontWeight: 600, marginBottom: 8 }}>Saved devices</div>
      <div className="muted" style={{ fontSize: 11.5, marginBottom: 10 }}>
        Per-device settings (proxy, custom hosts, frida config, labels) are stored locally and re-applied when a device reconnects.
      </div>
      <div className="col" style={{ gap: 6 }}>
        {all.map(d => {
          const s = saved.find(x => x.id === d.id);
          return (
            <div key={d.id} className="spread" style={{
              padding: 10, borderRadius: 8,
              background: "var(--panel-2)", border: "1px solid var(--border)",
            }}>
              <div className="row" style={{ gap: 10, minWidth: 0 }}>
                <span className={`led online`} style={{ width: 8, height: 8, borderRadius: 4, background: d.online ? "var(--ok)" : "var(--text-subtle)" }} />
                <div style={{ minWidth: 0 }}>
                  <div className="row" style={{ gap: 8 }}>
                    <span style={{ fontWeight: 600, fontSize: 13 }}>{s?.label || d.label}</span>
                    {s ? <span className="badge accent">saved</span> : <span className="badge">unsaved</span>}
                    {s?.hasProxy && <span className="badge info"><Icons.Shield size={9} /> proxy</span>}
                    {s?.hostsCount > 0 && <span className="badge"><Icons.Globe size={9} /> {s.hostsCount} hosts</span>}
                  </div>
                  <div className="mono subtle" style={{ fontSize: 11 }}>{d.id} · {d.via}</div>
                </div>
              </div>
              {s && (
                <button className="btn ghost danger sm" onClick={() => onForget(d.id)}>
                  <Icons.Trash className="icon" /> Forget
                </button>
              )}
            </div>
          );
        })}
        {saved.filter(s => !all.find(a => a.id === s.id)).map(s => (
          <div key={s.id} className="spread" style={{
            padding: 10, borderRadius: 8, opacity: 0.65,
            background: "var(--panel-2)", border: "1px solid var(--border)",
          }}>
            <div className="row" style={{ gap: 10 }}>
              <span style={{ width: 8, height: 8, borderRadius: 4, background: "var(--text-subtle)" }} />
              <div>
                <div className="row" style={{ gap: 8 }}>
                  <span style={{ fontWeight: 600, fontSize: 13 }}>{s.label}</span>
                  <span className="badge">offline</span>
                </div>
                <div className="mono subtle" style={{ fontSize: 11 }}>{s.id}</div>
              </div>
            </div>
            <button className="btn ghost danger sm" onClick={() => onForget(s.id)}><Icons.Trash className="icon" /> Forget</button>
          </div>
        ))}
      </div>
    </div>
  );
}

function ShortcutsSettings() {
  const groups = [
    ["Navigation", [
      ["Open device picker", "⌘ K"],
      ["Next device tab", "⌘ ⇥"],
      ["Switch to Logcat", "⌘ 1"],
      ["Switch to Shell", "⌘ 2"],
      ["Switch to Apps", "⌘ 3"],
      ["Switch to Files", "⌘ 4"],
    ]],
    ["Logcat", [
      ["Pause / resume", "Space"],
      ["Clear buffer", "⌘ ⌫"],
      ["Focus search", "⌘ F"],
      ["Export", "⌘ E"],
    ]],
    ["Shell", [
      ["New session", "⌘ T"],
      ["Become root", "⌘ ⇧ R"],
      ["Clear", "⌘ L"],
    ]],
  ];
  return (
    <div>
      <div className="muted" style={{ fontSize: 11, textTransform: "uppercase", letterSpacing: "0.04em", fontWeight: 600, marginBottom: 4 }}>Keyboard shortcuts</div>
      {groups.map(([gname, items]) => (
        <div key={gname} style={{ marginTop: 12 }}>
          <div style={{ fontWeight: 600, fontSize: 12, marginBottom: 6 }}>{gname}</div>
          {items.map(([name, k]) => (
            <div key={name} className="spread" style={{ padding: "5px 0", fontSize: 12.5 }}>
              <span>{name}</span>
              <span className="kbd">{k}</span>
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}

function AboutSettings() {
  return (
    <div style={{ textAlign: "center", padding: "20px 0" }}>
      <div style={{ width: 56, height: 56, margin: "0 auto 12px", borderRadius: 14, background: "var(--accent)", display: "flex", alignItems: "center", justifyContent: "center", color: "white", fontWeight: 700, fontSize: 22 }}>a</div>
      <div style={{ fontWeight: 600, fontSize: 16 }}>adbq</div>
      <div className="muted" style={{ fontSize: 12, marginTop: 2 }}>ADB Manager · v0.1.0</div>
      <div className="muted" style={{ fontSize: 11, marginTop: 14, fontFamily: "var(--font-mono)" }}>
        Built with Wails v2 · Go 1.22.3<br />
        WebView2 / WebKit · Babel + React 18
      </div>
      <div className="muted" style={{ fontSize: 11, marginTop: 16 }}>
        adb-server 1.0.41 · running on 127.0.0.1:5037
      </div>
    </div>
  );
}

Object.assign(window, { SettingsModal });
