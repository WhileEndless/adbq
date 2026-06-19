// Port forward / reverse manager — real state, add/remove
function ForwardsScreen({ device, devState, setDevState }) {
  const [mode, setMode] = useState("forward");
  const [showAdd, setShowAdd] = useState(false);
  const [confirm, setConfirm] = useState(null);

  const forwards = devState.forwards || MOCK.FORWARDS;
  const reverses = devState.reverses || MOCK.REVERSES;
  const rows = mode === "forward" ? forwards : reverses;

  const addRow = (row) => {
    if (mode === "forward") setDevState({ forwards: [...forwards, row] });
    else setDevState({ reverses: [...reverses, row] });
    showToast({ title: "Forward added", body: `${row.local} ${mode === "forward" ? "→" : "←"} ${row.remote}`, kind: "ok", mono: true });
  };
  const remove = (i) => {
    if (mode === "forward") setDevState({ forwards: forwards.filter((_, idx) => idx !== i) });
    else setDevState({ reverses: reverses.filter((_, idx) => idx !== i) });
    showToast({ title: "Removed", kind: "info" });
  };

  return (
    <div className="screen">
      <div className="screen-header">
        <h1><Icons.Forward size={17} /> Forwards</h1>
        <span className="subtitle muted">host ⇄ device tunnels for {device.label}</span>
        <div className="spacer" />
        <button className="btn ghost" onClick={() => showToast({ title: "Reloaded", body: "adb forward --list", kind: "info", mono: true })}>
          <Icons.Refresh className="icon" /> Refresh
        </button>
        <button className="btn ghost" onClick={() => setConfirm("killall")}>
          <Icons.Trash className="icon" /> Remove all
        </button>
        <button className="btn primary" onClick={() => setShowAdd(true)}>
          <Icons.Plus className="icon" /> New {mode === "forward" ? "forward" : "reverse"}
        </button>
      </div>

      <div className="screen-body">
        <div className="row" style={{ marginBottom: 14, background: "var(--bg-elev)", border: "1px solid var(--border)", borderRadius: 8, padding: 3, width: "fit-content" }}>
          <SegBtn active={mode === "forward"} onClick={() => setMode("forward")}>
            <Icons.Forward size={12} /> Forward <span className="subtle">{forwards.length}</span>
          </SegBtn>
          <SegBtn active={mode === "reverse"} onClick={() => setMode("reverse")}>
            <Icons.Reverse size={12} /> Reverse <span className="subtle">{reverses.length}</span>
          </SegBtn>
        </div>

        <div className="card">
          <div className="card-header">
            {mode === "forward" ? <Icons.Forward size={13} className="muted" /> : <Icons.Reverse size={13} className="muted" />}
            <span className="title">{mode === "forward" ? "Host → Device" : "Device → Host"}</span>
            <span className="muted" style={{ fontSize: 11, marginLeft: 6 }}>
              {mode === "forward"
                ? "Listens on your PC, relays to the device"
                : "Device opens connection, host listens — useful for Metro, mitmproxy"}
            </span>
          </div>
          {rows.length === 0 ? (
            <div className="card-body" style={{ textAlign: "center", color: "var(--text-subtle)", padding: 32 }}>
              <Icons.Forward size={26} />
              <div style={{ marginTop: 8, fontSize: 13 }}>No {mode === "forward" ? "forwards" : "reverses"} yet</div>
              <button className="btn primary" style={{ marginTop: 12 }} onClick={() => setShowAdd(true)}>
                <Icons.Plus className="icon" /> Create one
              </button>
            </div>
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th style={{ paddingLeft: 14 }}>{mode === "forward" ? "Local (host)" : "Remote (device)"}</th>
                  <th style={{ width: 32, textAlign: "center" }}></th>
                  <th>{mode === "forward" ? "Remote (device)" : "Local (host)"}</th>
                  <th>Process</th>
                  <th>Added</th>
                  <th className="actions"></th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r, i) => (
                  <tr key={i}>
                    <td style={{ paddingLeft: 14 }} className="mono">
                      <span className="badge accent" style={{ fontFamily: "var(--font-mono)" }}>
                        <Icons.Globe size={9} /> {mode === "forward" ? r.local : r.remote}
                      </span>
                    </td>
                    <td style={{ textAlign: "center", color: "var(--text-subtle)" }}>
                      {mode === "forward" ? "→" : "←"}
                    </td>
                    <td className="mono">
                      <span className="badge" style={{ fontFamily: "var(--font-mono)" }}>
                        <Icons.Phone size={9} /> {mode === "forward" ? r.remote : r.local}
                      </span>
                    </td>
                    <td className="muted">{r.proc}</td>
                    <td className="muted mono">{r.added}</td>
                    <td className="actions">
                      <div className="row" style={{ justifyContent: "flex-end", gap: 2 }}>
                        <button className="iconbtn" title="Test connection"
                                onClick={() => showToast({ title: "Connected", body: `${mode === "forward" ? r.local : r.remote} OK · 4ms`, kind: "ok", mono: true })}>
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
        </div>

        <div className="grid-2" style={{ marginTop: 14 }}>
          <div className="card">
            <div className="card-header">
              <Icons.Bolt size={13} className="muted" />
              <span className="title">Quick presets</span>
            </div>
            <div className="card-body col" style={{ gap: 6 }}>
              {[
                { label: "Chrome DevTools",  from: "tcp:9222",  to: "localabstract:chrome_devtools_remote", reverse: false, proc: "Chrome DevTools" },
                { label: "Frida server",     from: "tcp:27042", to: "tcp:27042", reverse: false, proc: "frida-server" },
                { label: "Metro bundler",    from: "tcp:8081",  to: "tcp:8081",  reverse: true,  proc: "Metro bundler" },
                { label: "mitmproxy",        from: "tcp:8080",  to: "tcp:8080",  reverse: true,  proc: "mitmproxy" },
                { label: "Node --inspect",   from: "tcp:9229",  to: "tcp:9229",  reverse: false, proc: "node inspector" },
                { label: "Scrcpy",           from: "tcp:27183", to: "localabstract:scrcpy",     reverse: false, proc: "scrcpy" },
              ].map(p => (
                <div key={p.label} className="spread" style={{ padding: "5px 0" }}>
                  <div className="row" style={{ gap: 8 }}>
                    <span style={{ fontSize: 12, fontWeight: 500 }}>{p.label}</span>
                    {p.reverse && <span className="badge">reverse</span>}
                  </div>
                  <div className="row mono" style={{ gap: 8, color: "var(--text-muted)", fontSize: 11 }}>
                    <span>{p.from}</span>
                    <span className="subtle">{p.reverse ? "←" : "→"}</span>
                    <span>{p.to}</span>
                    <button className="iconbtn" style={{ width: 22, height: 22 }} title="Add"
                            onClick={() => {
                              setMode(p.reverse ? "reverse" : "forward");
                              addRow({
                                local: p.from,
                                remote: p.to,
                                proc: p.proc,
                                added: "just now",
                              });
                            }}>
                      <Icons.Plus size={11} />
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </div>

          <div className="card">
            <div className="card-header">
              <Icons.Code size={13} className="muted" />
              <span className="title">Raw adb commands</span>
              <div className="spacer" style={{ flex: 1 }} />
              <button className="iconbtn" title="Copy all"
                      onClick={() => showToast({ title: "Copied", body: "adb commands on clipboard", kind: "ok" })}>
                <Icons.Copy size={12} />
              </button>
            </div>
            <div className="card-body" style={{ fontFamily: "var(--font-mono)", fontSize: 11.5, color: "var(--text-muted)" }}>
              {forwards.map((f, i) => (
                <div key={i}><span className="prompt-user mono">$</span> adb -s {device.id} forward {f.local} {f.remote}</div>
              ))}
              {reverses.length > 0 && <div className="divider" />}
              {reverses.map((r, i) => (
                <div key={i}><span className="prompt-user mono">$</span> adb -s {device.id} reverse {r.remote} {r.local}</div>
              ))}
              {forwards.length === 0 && reverses.length === 0 && (
                <div className="subtle">No active tunnels.</div>
              )}
            </div>
          </div>
        </div>
      </div>

      {showAdd && <AddForwardModal mode={mode} onClose={() => setShowAdd(false)} onAdd={addRow} />}
      <Confirm open={confirm === "killall"} onClose={() => setConfirm(null)}
               title="Remove all tunnels?" danger confirmLabel="Remove all"
               body={`This will run 'adb forward --remove-all' on ${device.label}.`}
               onConfirm={() => {
                 setDevState({ forwards: [], reverses: [] });
                 showToast({ title: "All tunnels removed", kind: "ok" });
               }} />
    </div>
  );
}

function SegBtn({ active, onClick, children }) {
  return (
    <button onClick={onClick}
            style={{
              padding: "5px 12px", borderRadius: 5, fontSize: 12, fontWeight: 500,
              background: active ? "var(--panel)" : "transparent",
              boxShadow: active ? "var(--shadow-sm)" : "none",
              border: active ? "1px solid var(--border)" : "1px solid transparent",
              color: active ? "var(--text)" : "var(--text-muted)",
              display: "inline-flex", alignItems: "center", gap: 6,
            }}>
      {children}
    </button>
  );
}

function AddForwardModal({ mode, onClose, onAdd }) {
  const [protocol, setProtocol] = useState("tcp");
  const [localPort, setLocalPort] = useState("8080");
  const [remotePort, setRemotePort] = useState("8080");
  const [label, setLabel] = useState("");
  const submit = () => {
    onAdd({
      local: `${protocol}:${localPort}`,
      remote: `${protocol}:${remotePort}`,
      proc: label || "—",
      added: "just now",
    });
    onClose();
  };
  return (
    <Modal open onClose={onClose} width={460}
           title={`New ${mode === "forward" ? "forward" : "reverse"}`}
           footer={<>
             <button className="btn ghost" onClick={onClose}>Cancel</button>
             <button className="btn primary" onClick={submit}><Icons.Plus className="icon" /> Add</button>
           </>}>
      <div className="col" style={{ gap: 12 }}>
        <div className="field">
          <label>Protocol</label>
          <div className="row" style={{ gap: 4 }}>
            {["tcp", "localabstract", "localreserved", "jdwp"].map(p => (
              <SegBtn key={p} active={p === protocol} onClick={() => setProtocol(p)}>{p}</SegBtn>
            ))}
          </div>
        </div>
        <div className="grid-2">
          <div className="field">
            <label>{mode === "forward" ? "Local port (host)" : "Remote port (device)"}</label>
            <input className="input mono" value={localPort} onChange={e => setLocalPort(e.target.value)} />
          </div>
          <div className="field">
            <label>{mode === "forward" ? "Remote port (device)" : "Local port (host)"}</label>
            <input className="input mono" value={remotePort} onChange={e => setRemotePort(e.target.value)} />
          </div>
        </div>
        <div className="field">
          <label>Label (optional)</label>
          <input className="input" value={label} onChange={e => setLabel(e.target.value)} placeholder="e.g. Chrome DevTools" />
        </div>
        <div className="mono" style={{ background: "var(--terminal-bg)", border: "1px solid var(--border)",
                                         borderRadius: 6, padding: "8px 10px", fontSize: 11.5, color: "var(--text-muted)" }}>
          <span className="prompt-user">$</span> adb {mode === "reverse" ? "reverse" : "forward"} {protocol}:{localPort} {protocol}:{remotePort}
        </div>
      </div>
    </Modal>
  );
}

Object.assign(window, { ForwardsScreen, SegBtn });
