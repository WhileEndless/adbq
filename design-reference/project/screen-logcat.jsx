// Logcat — single-line rows + virtualized, click to expand. Searchable app picker.
const LOGCAT_ROW_H = 22;

function LogcatScreen({ device }) {
  const [level, setLevel] = useState("V");
  const [appFilter, setAppFilter] = useState("com.example.scanner");
  const [search, setSearch] = useState("");
  const [paused, setPaused] = useState(false);
  const [autoscroll, setAutoscroll] = useState(true);
  const [cleared, setCleared] = useState(false);
  const [expandedKey, setExpandedKey] = useState(null);
  const [liveCount, setLiveCount] = useState(150); // simulates a growing stream from initial buffer
  const userScrolledRef = useRef(false);

  useEffect(() => {
    if (paused || cleared) return;
    const id = setInterval(() => {
      setLiveCount(c => Math.min(MOCK.LOG_LINES.length, c + 6 + Math.floor(Math.random() * 8)));
    }, 1000);
    return () => clearInterval(id);
  }, [paused, cleared]);

  const lines = useMemo(() => MOCK.LOG_LINES.slice(0, liveCount), [liveCount]);

  const levelIdx = { V: 0, D: 1, I: 2, W: 3, E: 4, F: 5 };
  const filtered = useMemo(() => {
    if (cleared) return [];
    return lines.filter((l) => {
      if (levelIdx[l.lvl] < levelIdx[level]) return false;
      if (appFilter && appFilter !== "__all" && !affineAppMatch(l, appFilter)) return false;
      if (search && !`${l.tag} ${l.msg}`.toLowerCase().includes(search.toLowerCase())) return false;
      return true;
    });
  }, [level, appFilter, search, lines, cleared]);

  const appOptions = useMemo(() => [
    { value: "__all", label: "All apps", sub: "no filter", icon: { bg: "var(--accent)", l: "*" } },
    { value: "system_server", label: "system_server", sub: "pid 1582", icon: { bg: "#5f6368", l: "S" } },
    ...MOCK.APPS.map(a => ({
      value: a.pkg,
      label: a.name,
      sub: a.pkg,
      icon: a.icon,
      right: a.system ? "system" : "",
    })),
  ], []);

  const onClear = () => {
    setCleared(true);
    setExpandedKey(null);
    showToast({ title: "Buffer cleared", body: `adb logcat -c on ${device.label}`, kind: "info", mono: true });
  };
  const onExport = () => {
    showToast({ title: "Exported", body: `~/adbq-logs/logcat-${Date.now()}.txt — ${filtered.length} lines`, kind: "ok", mono: true });
  };

  return (
    <div className="screen">
      <div className="screen-header">
        <h1>
          <Icons.Logcat size={17} /> Logcat
          {paused
            ? <span className="badge warn"><span className="dot" /> paused</span>
            : cleared
              ? <span className="badge"><span className="dot" /> cleared</span>
              : <span className="badge ok"><span className="pulse" /> live</span>}
        </h1>
        <span className="subtitle muted">
          {filtered.length.toLocaleString()} / {lines.length.toLocaleString()} lines · main,system,crash
        </span>
        <div className="spacer" />
        <button className="btn ghost" onClick={() => setPaused(!paused)}>
          {paused ? <><Icons.Play className="icon" /> Resume</> : <><Icons.Pause className="icon" /> Pause</>}
        </button>
        <button className="btn ghost" onClick={onClear}><Icons.Trash className="icon" /> Clear</button>
        <button className="btn" onClick={onExport}><Icons.Download className="icon" /> Export</button>
      </div>

      <div className="logcat">
        <div className="logcat-toolbar">
          <div className="search-wrap" style={{ flex: 1, maxWidth: 300 }}>
            <Icons.Search size={13} />
            <input className="input" style={{ width: "100%" }} placeholder="search tag or message…"
                   value={search} onChange={(e) => setSearch(e.target.value)} />
          </div>

          <div className="row" style={{ background: "var(--bg-elev)", border: "1px solid var(--border)", borderRadius: 6, padding: 2, gap: 0 }}>
            {["V", "D", "I", "W", "E"].map(l => (
              <button key={l}
                      className={`iconbtn ${level === l ? "active" : ""}`}
                      style={{
                        width: 24, height: 22, borderRadius: 4, fontFamily: "var(--font-mono)",
                        fontSize: 11, fontWeight: 700,
                        color: level === l ? `var(--log-${l.toLowerCase()})` : undefined,
                      }}
                      onClick={() => setLevel(l)}
                      title={`Show ${l} and above`}>
                {l}
              </button>
            ))}
          </div>

          <Combobox value={appFilter} onChange={setAppFilter} options={appOptions}
                    placeholder="Filter by app…" triggerIcon="Filter" popWidth={340} />

          <div className="spacer" style={{ flex: 1 }} />

          <button className="btn ghost" onClick={() => setAutoscroll(!autoscroll)}
                  style={{ color: autoscroll ? "var(--accent)" : undefined }}>
            <Icons.ChevronDown className="icon" /> tail
          </button>
        </div>

        <LogcatList
          rows={filtered}
          expandedKey={expandedKey}
          setExpandedKey={setExpandedKey}
          search={search}
          followBottom={autoscroll && !paused && !cleared}
          onUserScroll={() => { userScrolledRef.current = true; }}
        />

        <div className="logcat-status">
          <span>{filtered.length.toLocaleString()} of {lines.length.toLocaleString()} lines</span>
          <span>·</span>
          <span>filter: <span style={{ color: "var(--accent)" }}>{appFilter === "__all" ? "all apps" : appFilter}</span></span>
          <span>·</span>
          <span>level ≥ <span className="mono" style={{ color: `var(--log-${level.toLowerCase()})` }}>{level}</span></span>
          <div className="spacer" style={{ flex: 1 }} />
          <span>virtual · only visible rows rendered</span>
          <span>·</span>
          <span>-v threadtime{appFilter !== "__all" && appFilter !== "system_server" ? ` --pid=$(pidof ${appFilter})` : ""}</span>
        </div>
      </div>
    </div>
  );
}

function LogcatList({ rows, expandedKey, setExpandedKey, search, followBottom, onUserScroll }) {
  if (rows.length === 0) {
    return (
      <div className="logcat-rows" style={{ display: "flex", alignItems: "center", justifyContent: "center", color: "var(--text-subtle)", fontFamily: "var(--font-sans)" }}>
        <div style={{ textAlign: "center" }}>
          <Icons.Logcat size={32} />
          <div style={{ marginTop: 10 }}>No matching lines</div>
        </div>
      </div>
    );
  }
  return (
    <VirtualList
      className="logcat-rows"
      items={rows}
      itemHeight={LOGCAT_ROW_H}
      followBottom={followBottom}
      onScroll={onUserScroll}
      renderItem={(l, i) => {
        const key = `${l.time}-${l.pid}-${i}`;
        const exp = expandedKey === key;
        return (
          <div className={`logrow ${l.lvl} ${exp ? "expanded" : ""}`}
               onClick={() => setExpandedKey(exp ? null : key)}>
            <span className="time">{l.time}</span>
            <span className="pid">{l.pid}-{l.tid}</span>
            <span className="lvl">{l.lvl}</span>
            <span className="tag-msg">
              <span className="tag" style={{ color: MOCK.TAG_COLORS[l.tag] || "var(--text-muted)" }}>{l.tag}:</span>
              <span className="msg">{highlight(l.msg, search)}</span>
            </span>
          </div>
        );
      }}
    />
  );
}

function affineAppMatch(line, app) {
  if (app === "com.example.scanner") return line.pid === 19842 || /scanner|example/i.test(line.msg);
  if (app === "com.spotify.music") return /spotify/i.test(line.msg) || line.pid === 18221;
  if (app === "system_server") return line.pid === 1582;
  if (app === "com.android.chrome") return /chrome/i.test(line.msg);
  const short = app.split(".").pop();
  return new RegExp(short, "i").test(line.tag + " " + line.msg);
}

function highlight(text, q) {
  if (!q) return text;
  const i = text.toLowerCase().indexOf(q.toLowerCase());
  if (i < 0) return text;
  return (
    <>
      {text.slice(0, i)}
      <mark>{text.slice(i, i + q.length)}</mark>
      {text.slice(i + q.length)}
    </>
  );
}

Object.assign(window, { LogcatScreen });
