// Files browser
function FilesScreen({ device }) {
  const [path, setPath] = useState("/data/local/tmp");
  const [selected, setSelected] = useState("frida-server-16.4.7-android-arm64");
  const [expand, setExpand] = useState({ "/": true, "/data": true, "/data/local": true });

  const sel = MOCK.FILES.find(f => f.name === selected);
  const segments = path.split("/").filter(Boolean);

  return (
    <div className="screen">
      <div className="screen-header">
        <h1><Icons.Folder size={17} /> Files</h1>
        <span className="subtitle muted">/{segments.join("/")}</span>
        <div className="spacer" />
        <button className="btn"><Icons.Upload className="icon" /> Push</button>
        <button className="btn"><Icons.Download className="icon" /> Pull</button>
        <button className="btn ghost"><Icons.Plus className="icon" /> New folder</button>
      </div>

      <div className="screen-body flush" style={{ overflow: "hidden" }}>
        <div className="files-layout">
          <div className="files-tree">
            <FileTree nodes={MOCK.TREE} depth={0} expand={expand} setExpand={setExpand} path={path} setPath={setPath} prefix="" />
          </div>

          <div className="files-main">
            <div className="path-bar">
              <Icons.ChevronRight size={11} className="muted" />
              <span className="crumb" style={{ cursor: "pointer" }} onClick={() => setPath("/")}>/</span>
              {segments.map((s, i) => (
                <React.Fragment key={i}>
                  <span className="sep">/</span>
                  <span className="crumb" onClick={() => setPath("/" + segments.slice(0, i + 1).join("/"))}>{s}</span>
                </React.Fragment>
              ))}
              <div className="spacer" style={{ flex: 1 }} />
              <span className="muted">{MOCK.FILES.length - 1} items · 113 MB</span>
            </div>

            <div style={{ overflow: "auto", flex: 1 }}>
              <table className="table">
                <thead>
                  <tr>
                    <th style={{ width: "auto" }}>Name</th>
                    <th style={{ width: 110 }}>Size</th>
                    <th style={{ width: 130 }}>Permissions</th>
                    <th style={{ width: 100 }}>Owner</th>
                    <th style={{ width: 150 }}>Modified</th>
                    <th className="actions"></th>
                  </tr>
                </thead>
                <tbody>
                  {MOCK.FILES.map((f, i) => {
                    if (f.type === "up") {
                      return (
                        <tr key={i} onDoubleClick={() => setPath(path.split("/").slice(0, -1).join("/") || "/")} style={{ cursor: "pointer" }}>
                          <td className="muted"><Icons.ChevronRight size={11} style={{ transform: "rotate(180deg)" }} /> ..</td>
                          <td colSpan={5}></td>
                        </tr>
                      );
                    }
                    const isDir = f.type === "dir";
                    const isSel = selected === f.name;
                    const ext = f.name.split(".").pop();
                    let iconColor = "var(--text-muted)";
                    let Icon = Icons.File;
                    if (isDir) { Icon = Icons.Folder; iconColor = "var(--accent)"; }
                    else if (/frida/.test(f.name)) { Icon = Icons.Bug; iconColor = "var(--accent)"; }
                    else if (ext === "png") { Icon = Icons.Image; iconColor = "var(--info)"; }
                    return (
                      <tr key={i}
                          onClick={() => setSelected(f.name)}
                          style={{ background: isSel ? "var(--accent-soft)" : undefined, cursor: "pointer" }}>
                        <td>
                          <span className="row" style={{ gap: 8 }}>
                            <span style={{ color: iconColor }}><Icon size={14} /></span>
                            <span style={{ fontWeight: isSel ? 600 : 500 }}>{f.name}</span>
                          </span>
                        </td>
                        <td className="mono muted">{f.size != null ? formatSize(f.size) : "—"}</td>
                        <td className="mono muted" style={{ fontSize: 11 }}>{f.perms}</td>
                        <td className="mono muted">{f.owner}:{f.group}</td>
                        <td className="mono muted">{f.mtime}</td>
                        <td className="actions">
                          <div className="row" style={{ gap: 2, justifyContent: "flex-end" }}>
                            <button className="iconbtn" title="Pull"><Icons.Download size={12} /></button>
                            <button className="iconbtn" title="Delete"><Icons.Trash size={12} /></button>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </div>

          {sel && (
            <div className="file-detail">
              <div className="row" style={{ gap: 12 }}>
                <div style={{ width: 44, height: 44, borderRadius: 9, background: "var(--accent-soft)", color: "var(--accent-strong)", display: "flex", alignItems: "center", justifyContent: "center" }}>
                  {sel.type === "dir" ? <Icons.Folder size={20} /> : /frida/.test(sel.name) ? <Icons.Bug size={20} /> : <Icons.File size={20} />}
                </div>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontWeight: 600, fontSize: 14 }}>{sel.name}</div>
                  <div className="subtle mono" style={{ fontSize: 11 }}>{path}/{sel.name}</div>
                </div>
              </div>

              <div className="divider" style={{ margin: "14px 0" }} />
              <DetailGrid rows={[
                ["Size",        sel.size != null ? `${formatSize(sel.size)} (${sel.size.toLocaleString()} bytes)` : "—"],
                ["Permissions", sel.perms, true],
                ["Owner",       `${sel.owner}:${sel.group}`, true],
                ["Modified",    sel.mtime, true],
                ["SELinux",     "u:object_r:shell_data_file:s0", true],
              ]} />

              <div className="divider" style={{ margin: "14px 0" }} />
              <div className="col" style={{ gap: 6 }}>
                <button className="btn"><Icons.Download className="icon" /> Pull to host</button>
                <button className="btn"><Icons.Eye className="icon" /> Preview / hex view</button>
                <button className="btn">chmod &amp; chown</button>
                {/frida/.test(sel.name) && <button className="btn primary"><Icons.Play className="icon" /> Run as frida-server</button>}
                <button className="btn danger"><Icons.Trash className="icon" /> Delete</button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function FileTree({ nodes, depth, expand, setExpand, path, setPath, prefix }) {
  return (
    <>
      {nodes.map((n) => {
        const p = (prefix + "/" + n.name).replace("//", "/");
        const isOpen = !!expand[p];
        const hasChildren = !!n.children?.length;
        const isActive = path === p || n.active;
        return (
          <React.Fragment key={p}>
            <div className={`tree-row ${isActive ? "active" : ""}`}
                 style={{ paddingLeft: 8 + depth * 12 }}
                 onClick={() => { if (hasChildren) setExpand({ ...expand, [p]: !isOpen }); setPath(p); }}>
              <span style={{ width: 11, display: "inline-flex" }}>
                {hasChildren ? <Icons.ChevronRight size={10} style={{ transform: isOpen ? "rotate(90deg)" : "" }} /> : null}
              </span>
              <Icons.Folder size={12} style={{ color: isActive ? "var(--accent)" : "var(--text-subtle)" }} />
              <span>{n.name}</span>
            </div>
            {hasChildren && isOpen && (
              <FileTree nodes={n.children} depth={depth + 1} expand={expand} setExpand={setExpand} path={path} setPath={setPath} prefix={p} />
            )}
          </React.Fragment>
        );
      })}
    </>
  );
}

function formatSize(n) {
  if (n < 1024) return `${n} B`;
  if (n < 1024 ** 2) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 ** 3) return `${(n / 1024 ** 2).toFixed(1)} MB`;
  return `${(n / 1024 ** 3).toFixed(2)} GB`;
}

Object.assign(window, { FilesScreen });
