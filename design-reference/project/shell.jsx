// App shell: titlebar + device tabs + sidebar + main content
const { useState, useEffect, useMemo, useRef, useCallback } = React;

const NAV_SECTIONS = [
  {
    label: "Device",
    items: [
      { id: "overview",  name: "Overview",        icon: "Dashboard" },
      { id: "logcat",    name: "Logcat",          icon: "Logcat", featured: true, badge: "live" },
      { id: "shell",     name: "Shell",           icon: "Terminal", featured: true },
    ],
  },
  {
    label: "Manage",
    items: [
      { id: "apps",      name: "Apps",            icon: "Apps", count: 12 },
      { id: "files",     name: "Files",           icon: "Folder" },
      { id: "forwards",  name: "Port forwards",   icon: "Forward", count: 5 },
      { id: "frida",     name: "Frida",           icon: "Bug" },
      { id: "network",   name: "Network & proxy", icon: "Network" },
    ],
  },
];

function Titlebar({ theme, setTheme, onOpenSettings }) {
  const isDark = theme === "dark";
  return (
    <div className="titlebar">
      <div className="traffic"><span /><span /><span /></div>
      <div className="brand"><span className="dot" />adbq</div>
      <div className="titlebar-spacer" />
      <span className="meta-line">
        <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
          <span className="pulse" style={{ width: 6, height: 6 }} />
          <span>server up</span>
        </span>
        <span className="subtle">·</span>
        <span>127.0.0.1:5037</span>
        <span className="subtle">·</span>
        <span>3 devices</span>
      </span>
      <div style={{ width: 8 }} />
      <button className="iconbtn" title="Toggle theme" onClick={() => setTheme(isDark ? "light" : "dark")}
        style={{ WebkitAppRegion: "no-drag" }}>
        {isDark ? <Icons.Sun size={14} /> : <Icons.Moon size={14} />}
      </button>
      <button className="iconbtn" title="Settings" onClick={onOpenSettings} style={{ WebkitAppRegion: "no-drag" }}>
        <Icons.Settings size={14} />
      </button>
    </div>
  );
}

function DeviceTabs({ devices, activeId, onSelect, onClose, onAdd }) {
  return (
    <div className="devicetabs">
      {devices.map((d) => {
        const led = d.online ? (d.unauth ? "unauth" : "online") : "offline";
        return (
          <div key={d.id}
               className={`devicetab ${activeId === d.id ? "active" : ""}`}
               onClick={() => onSelect(d.id)}>
            <span className={`led ${led}`} />
            <div className="col" style={{ gap: 0 }}>
              <span className="name">{d.label}</span>
              <span className="sub">{d.id}</span>
            </div>
            <span className="x" onClick={(e) => { e.stopPropagation(); onClose(d.id); }}>
              <Icons.Close size={11} />
            </span>
          </div>
        );
      })}
      <div className="devicetab add" title="Connect new device" onClick={onAdd}>
        <Icons.Plus size={13} /> Connect
      </div>
    </div>
  );
}

function Sidebar({ device, screen, setScreen, counts }) {
  const Icon = (name) => Icons[name];
  const sections = [
    {
      label: "Device",
      items: [
        { id: "overview", name: "Overview", icon: "Dashboard" },
        { id: "logcat",   name: "Logcat",   icon: "Logcat", badge: "live" },
        { id: "shell",    name: "Shell",    icon: "Terminal" },
      ],
    },
    {
      label: "Manage",
      items: [
        { id: "apps",     name: "Apps",     icon: "Apps",    count: counts.apps },
        { id: "files",    name: "Files",    icon: "Folder" },
        { id: "forwards", name: "Forwards", icon: "Forward", count: counts.forwards },
        { id: "frida",    name: "Frida",    icon: "Bug" },
        { id: "network",  name: "Network",  icon: "Network" },
      ],
    },
  ];
  return (
    <aside className="sidebar">
      <div className="devicecard">
        <div className="model">
          <span>{device.label}</span>
          <span className={`badge ${device.online ? "ok" : ""}`}><span className="dot" />{device.online ? "online" : "offline"}</span>
        </div>
        <div className="serial">{device.id}</div>
        <div className="badges">
          <span className={`badge ${device.root ? "accent" : ""}`}>
            {device.root ? <><Icons.Shield size={10} /> root</> : <><Icons.Lock size={10} /> no root</>}
          </span>
          <span className="badge">Android {device.androidVersion}</span>
          <span className="badge">{device.via}</span>
        </div>
      </div>

      {sections.map((sec) => (
        <div className="group" key={sec.label}>
          <div className="label">{sec.label}</div>
          {sec.items.map((item) => {
            const Cmp = Icon(item.icon);
            return (
              <div key={item.id}
                   className={`nav ${screen === item.id ? "active" : ""}`}
                   onClick={() => setScreen(item.id)}>
                <Cmp className="icon" size={15} />
                <span>{item.name}</span>
                {item.badge === "live" ? (
                  <span className="count" style={{ display: "flex", alignItems: "center", gap: 4 }}>
                    <span className="pulse" style={{ width: 5, height: 5 }} />live
                  </span>
                ) : item.count != null ? (
                  <span className="count">{item.count}</span>
                ) : null}
              </div>
            );
          })}
        </div>
      ))}

      <div style={{ flex: 1 }} />
      <div className="footer">
        <span className="pulse" /> adb-server  127.0.0.1:5037
      </div>
    </aside>
  );
}

Object.assign(window, { Titlebar, DeviceTabs, Sidebar });
