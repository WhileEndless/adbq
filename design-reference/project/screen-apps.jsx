// Apps manager
function AppsScreen({ device }) {
  const [selected, setSelected] = useState("com.example.scanner");
  const [query, setQuery] = useState("");
  const [showSystem, setShowSystem] = useState(false);
  const [confirmUninstall, setConfirmUninstall] = useState(null);
  const [showInstall, setShowInstall] = useState(false);

  const apps = useMemo(() => {
    return MOCK.APPS
      .filter(a => showSystem || !a.system)
      .filter(a => !query || (a.name + a.pkg).toLowerCase().includes(query.toLowerCase()));
  }, [query, showSystem]);

  const sel = MOCK.APPS.find(a => a.pkg === selected) || apps[0];

  return (
    <div className="screen">
      <div className="screen-header">
        <h1><Icons.Apps size={17} /> Apps</h1>
        <span className="subtitle muted">{apps.length} installed</span>
        <div className="spacer" />
        <div className="search-wrap">
          <Icons.Search size={13} />
          <input className="input" placeholder="search apps…" value={query} onChange={e => setQuery(e.target.value)} style={{ width: 220 }} />
        </div>
        <button className="btn ghost" onClick={() => setShowSystem(!showSystem)}>
          <div className={`switch ${showSystem ? "on" : ""}`} style={{ width: 22, height: 13 }} />
          system
        </button>
        <button className="btn primary" onClick={() => setShowInstall(true)}><Icons.Upload className="icon" /> Install APK</button>
      </div>

      <div className="screen-body flush" style={{ overflow: "hidden" }}>
        <div className="apps-layout">
          <div className="apps-list">
            {apps.map(a => (
              <div key={a.pkg}
                   className={`app-row ${selected === a.pkg ? "selected" : ""}`}
                   onClick={() => setSelected(a.pkg)}>
                <div className="app-icon" style={{ background: a.icon.bg, color: a.icon.color || "white" }}>{a.icon.l}</div>
                <div style={{ minWidth: 0 }}>
                  <div className="name">{a.name} {a.system && <span className="badge" style={{ marginLeft: 6 }}>system</span>}</div>
                  <div className="pkg">{a.pkg}</div>
                </div>
                <div className="meta">
                  v{a.v}<br />
                  <span className="subtle">{a.size.toFixed(1)} MB</span>
                </div>
                <div className="row" style={{ gap: 2, opacity: 0.7 }}>
                  <button className="iconbtn" title="Export APK"><Icons.Download size={12} /></button>
                </div>
              </div>
            ))}
          </div>

          {sel && (
            <div className="app-detail">
              <div className="row" style={{ gap: 12, alignItems: "flex-start" }}>
                <div className="app-icon" style={{ width: 56, height: 56, borderRadius: 14, background: sel.icon.bg, color: sel.icon.color || "white", fontSize: 22 }}>{sel.icon.l}</div>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontWeight: 600, fontSize: 15, letterSpacing: "-0.01em" }}>{sel.name}</div>
                  <div className="mono subtle" style={{ fontSize: 11, wordBreak: "break-all" }}>{sel.pkg}</div>
                  <div className="row" style={{ gap: 4, marginTop: 6 }}>
                    <span className="badge">v{sel.v}</span>
                    <span className="badge">{sel.size.toFixed(1)} MB</span>
                    {sel.system ? <span className="badge warn">system</span> : <span className="badge ok">user</span>}
                  </div>
                </div>
              </div>

              <div className="divider" style={{ margin: "14px 0" }} />

              <div className="col" style={{ gap: 6 }}>
                <button className="btn" onClick={() => showToast({ title: "Launched", body: `am start -n ${sel.pkg}/.MainActivity`, kind: "ok", mono: true })}>
                  <Icons.Play className="icon" /> Launch
                </button>
                <button className="btn" onClick={() => showToast({ title: "Force-stopped", body: `am force-stop ${sel.pkg}`, kind: "info", mono: true })}>
                  <Icons.Stop className="icon" /> Force stop
                </button>
                <button className="btn" onClick={() => showToast({ title: "APK exported", body: `~/adbq-apks/${sel.pkg}.apk (${sel.size.toFixed(1)} MB)`, kind: "ok", mono: true })}>
                  <Icons.Download className="icon" /> Export APK
                </button>
                <button className="btn" onClick={() => showToast({ title: "Cleared data", body: `pm clear ${sel.pkg}`, kind: "info", mono: true })}>
                  <Icons.Refresh className="icon" /> Clear data
                </button>
                <button className="btn danger" disabled={sel.system} onClick={() => setConfirmUninstall(sel)}>
                  <Icons.Trash className="icon" /> {sel.system ? "Cannot uninstall (system)" : "Uninstall"}
                </button>
              </div>

              <div className="divider" style={{ margin: "14px 0" }} />

              <div style={{ fontSize: 11, fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.04em", color: "var(--text-muted)", marginBottom: 8 }}>Details</div>
              <DetailGrid rows={[
                ["UID",         "10214"],
                ["Target SDK",  "34"],
                ["Min SDK",     "26"],
                ["Installer",   "com.android.vending"],
                ["First inst.", "2026-03-12"],
                ["Last update", "2026-05-18"],
                ["APK path",    `/data/app/${sel.pkg}-1/base.apk`, true],
                ["Data path",   `/data/data/${sel.pkg}`, true],
              ]} />

              <div className="divider" style={{ margin: "14px 0" }} />

              <div style={{ fontSize: 11, fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.04em", color: "var(--text-muted)", marginBottom: 8 }}>Permissions</div>
              <div className="row" style={{ flexWrap: "wrap", gap: 4 }}>
                {["INTERNET", "ACCESS_NETWORK_STATE", "CAMERA", "ACCESS_FINE_LOCATION", "BLUETOOTH_CONNECT", "READ_EXTERNAL_STORAGE"].map(p => (
                  <span key={p} className="tag-pill">{p}</span>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>

      <Confirm open={!!confirmUninstall} onClose={() => setConfirmUninstall(null)}
               title={`Uninstall ${confirmUninstall?.name}?`} danger confirmLabel="Uninstall"
               body={`This will run 'pm uninstall ${confirmUninstall?.pkg}' on ${device.label}. App data will be removed.`}
               onConfirm={() => {
                 showToast({ title: "Uninstalled", body: `pm uninstall ${confirmUninstall?.pkg}`, kind: "ok", mono: true });
                 setConfirmUninstall(null);
               }} />

      <InstallApkModal open={showInstall} onClose={() => setShowInstall(false)} device={device} />
    </div>
  );
}

function InstallApkModal({ open, onClose, device }) {
  const [path, setPath] = useState("");
  const [grantAll, setGrantAll] = useState(true);
  const [replace, setReplace] = useState(true);
  return (
    <Modal open={open} onClose={onClose} width={480}
           title="Install APK"
           footer={<>
             <button className="btn ghost" onClick={onClose}>Cancel</button>
             <button className="btn primary" disabled={!path}
                     onClick={() => {
                       showToast({ title: "Installed", body: `adb install ${path} (3.2s)`, kind: "ok", mono: true });
                       onClose();
                     }}>
               <Icons.Upload className="icon" /> Install
             </button>
           </>}>
      <div className="col" style={{ gap: 12 }}>
        <div className="field">
          <label>APK path</label>
          <div className="row" style={{ gap: 6 }}>
            <input className="input mono" placeholder="~/Downloads/app.apk" value={path} onChange={e => setPath(e.target.value)} style={{ flex: 1 }} />
            <button className="btn" onClick={() => setPath("~/Downloads/cw-scanner-2.4.2.apk")}>
              <Icons.Folder className="icon" /> Browse…
            </button>
          </div>
          <span className="muted" style={{ fontSize: 11, marginTop: 4 }}>or drop a .apk anywhere on the window</span>
        </div>
        <div className="col" style={{ gap: 4 }}>
          <div className="spread" style={{ padding: "4px 0" }}>
            <span style={{ fontSize: 12 }}>Replace existing</span>
            <div className={`switch ${replace ? "on" : ""}`} onClick={() => setReplace(!replace)} />
          </div>
          <div className="spread" style={{ padding: "4px 0" }}>
            <span style={{ fontSize: 12 }}>Grant all runtime permissions</span>
            <div className={`switch ${grantAll ? "on" : ""}`} onClick={() => setGrantAll(!grantAll)} />
          </div>
        </div>
        <div className="mono" style={{ background: "var(--terminal-bg)", border: "1px solid var(--border)",
                                         borderRadius: 6, padding: "8px 10px", fontSize: 11.5, color: "var(--text-muted)" }}>
          <span className="prompt-user">$</span> adb -s {device.id} install{replace ? " -r" : ""}{grantAll ? " -g" : ""} {path || "<path>"}
        </div>
      </div>
    </Modal>
  );
}

Object.assign(window, { AppsScreen });
