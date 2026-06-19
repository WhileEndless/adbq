// Shell / terminal screen
function ShellScreen({ device }) {
  const [tabs, setTabs] = useState([
    { id: 1, name: "shell", root: false, cwd: "/sdcard" },
    { id: 2, name: "su",    root: true,  cwd: "/data/local/tmp" },
  ]);
  const [activeId, setActiveId] = useState(1);
  const active = tabs.find(t => t.id === activeId);
  const [input, setInput] = useState("");

  return (
    <div className="screen">
      <div className="screen-header">
        <h1><Icons.Terminal size={17} /> Shell</h1>
        <span className="subtitle muted">adb -s {device.id} shell · {tabs.length} session{tabs.length === 1 ? "" : "s"}</span>
        <div className="spacer" />
        {device.root && !active.root && (
          <button className="btn"><Icons.Shield className="icon" style={{ color: "var(--accent)" }} /> Become root</button>
        )}
        <button className="btn"><Icons.Plus className="icon" /> New session</button>
      </div>

      <div className="shell">
        <div className="shell-tabs">
          {tabs.map(t => (
            <div key={t.id}
                 className={`shell-tab ${t.root ? "root" : ""} ${activeId === t.id ? "active" : ""}`}
                 onClick={() => setActiveId(t.id)}>
              <span className="dot" />
              <span className="mono">{t.root ? "#" : "$"} {t.name}</span>
              <Icons.Close size={10} className="muted" />
            </div>
          ))}
          <div className="shell-tab" onClick={() => {
            const id = Math.max(...tabs.map(t => t.id)) + 1;
            setTabs([...tabs, { id, name: `shell-${id}`, root: false, cwd: "/sdcard" }]);
            setActiveId(id);
          }}>
            <Icons.Plus size={11} />
          </div>
          <div className="spacer" style={{ flex: 1 }} />
          <span className="muted mono" style={{ fontSize: 10.5, padding: "0 8px" }}>
            {active.root ? "root@" : "shell@"}{device.product}:{active.cwd}
          </span>
        </div>

        <div className="term">
          {active.root ? <RootTerm device={device} cwd={active.cwd} /> : <ShellTerm device={device} cwd={active.cwd} />}

          <div className="line">
            <Prompt root={active.root} device={device} cwd={active.cwd} />
            <span style={{ marginLeft: 6 }}>{input}</span>
            <span className="cursor" />
          </div>

          <div style={{ marginTop: 12, padding: "10px 12px", background: "var(--panel-2)",
                        border: "1px solid var(--border)", borderRadius: 8, display: "flex", gap: 8, flexWrap: "wrap" }}>
            <span className="muted" style={{ fontSize: 11 }}>Snippets:</span>
            {[
              "getprop ro.build.version.release",
              "pm list packages -3",
              "dumpsys battery",
              "settings put global http_proxy 10.0.0.5:8080",
              "ip route",
              "screencap -p /sdcard/s.png",
            ].map(s => (
              <button key={s} className="tag-pill" style={{ cursor: "pointer" }} onClick={() => setInput(s)}>{s}</button>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function Prompt({ root, device, cwd }) {
  return (
    <span>
      <span className={root ? "prompt-root" : "prompt-user"}>{root ? "root" : "shell"}</span>
      <span className="muted">@</span>
      <span className="prompt-host">{device.product}</span>
      <span className="muted">:</span>
      <span className="prompt-path">{cwd}</span>
      <span className={root ? "prompt-root" : ""}>{root ? " #" : " $"}</span>
    </span>
  );
}

function ShellTerm({ device, cwd }) {
  return (
    <>
      <Line><span className="cmt"># adbq shell — Wed May 22 12:18:04 2026</span></Line>
      <Line><Prompt root={false} device={device} cwd="/" /><Cmd>id</Cmd></Line>
      <Line><Out>uid=2000(shell) gid=2000(shell) groups=2000(shell),1004(input),1007(log),1011(adb),1015(sdcard_rw),1028(sdcard_r),3001(net_bt_admin),3002(net_bt),3003(inet),3006(net_bw_stats)</Out></Line>
      <Line><Prompt root={false} device={device} cwd="/" /><Cmd>cd /sdcard && ls -la</Cmd></Line>
      <Line><Out className="muted">total 124</Out></Line>
      <Line><Out>drwxrwx--- 16 root sdcard_rw  4096 May 22 09:11 <span className="prompt-path">.</span></Out></Line>
      <Line><Out>drwxr-xr-x  3 root root       4096 May 22 09:11 <span className="prompt-path">..</span></Out></Line>
      <Line><Out>drwxrwx---  7 root sdcard_rw  4096 May 22 10:02 <span className="prompt-path">Android</span></Out></Line>
      <Line><Out>drwxrwx---  3 root sdcard_rw  4096 May 22 12:08 <span className="prompt-path">DCIM</span></Out></Line>
      <Line><Out>drwxrwx---  2 root sdcard_rw  4096 May 22 09:42 <span className="prompt-path">Download</span></Out></Line>
      <Line><Prompt root={false} device={device} cwd="/sdcard" /><Cmd>su -c 'cat /data/local/tmp/burp-cert.der | head -c 8 | xxd'</Cmd></Line>
      <Line><Out className="err">/system/bin/sh: su: inaccessible or not found</Out></Line>
      <Line><Out className="muted">                      <span className="warn"># tip</span>: this shell is not root. Click <span style={{ color: "var(--accent)" }}>“Become root”</span> or open the <span className="prompt-path">su</span> tab.</Out></Line>
      <Line><Prompt root={false} device={device} cwd="/sdcard" /><Cmd>getprop ro.build.version.release</Cmd></Line>
      <Line><Out>{device.androidVersion}</Out></Line>
    </>
  );
}

function RootTerm({ device, cwd }) {
  return (
    <>
      <Line><span className="cmt"># Switched to root via `su` — {device.rootMethod || "su"}</span></Line>
      <Line><Prompt root={false} device={device} cwd="/sdcard" /><Cmd>su</Cmd></Line>
      <Line><Out className="muted">                      <span className="ok"># magisk</span>: granted (uid=2000 → 0)</Out></Line>
      <Line><Prompt root={true} device={device} cwd="/data/local/tmp" /><Cmd>id</Cmd></Line>
      <Line><Out>uid=0(root) gid=0(root) groups=0(root) context=u:r:su:s0</Out></Line>
      <Line><Prompt root={true} device={device} cwd="/data/local/tmp" /><Cmd>./frida-server-16.4.7-android-arm64 -l 0.0.0.0:27042 &amp;</Cmd></Line>
      <Line><Out className="ok">[1] 4421</Out></Line>
      <Line><Out className="muted">Frida server listening on 0.0.0.0:27042 (version 16.4.7)</Out></Line>
      <Line><Prompt root={true} device={device} cwd="/data/local/tmp" /><Cmd>ps -ef | grep frida</Cmd></Line>
      <Line><Out>root      4421     1   2 12:14 ?        00:00:01 ./frida-server-16.4.7-android-arm64 -l 0.0.0.0:27042</Out></Line>
      <Line><Prompt root={true} device={device} cwd="/data/local/tmp" /><Cmd>iptables -t nat -L OUTPUT -n --line-numbers</Cmd></Line>
      <Line><Out className="muted">Chain OUTPUT (policy ACCEPT)</Out></Line>
      <Line><Out>num  target     prot opt source               destination</Out></Line>
      <Line><Out>1    REDIRECT   tcp  --  0.0.0.0/0            0.0.0.0/0   tcp dpt:443 redir ports 8080</Out></Line>
    </>
  );
}

function Line({ children }) { return <div className="line">{children}</div>; }
function Cmd({ children }) { return <span style={{ marginLeft: 6 }}>{children}</span>; }
function Out({ children, className = "" }) { return <span className={className}>{children}</span>; }

Object.assign(window, { ShellScreen });
