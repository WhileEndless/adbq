// Main app
const { useState: useState_, useEffect: useEffect_ } = React;

function App() {
  const [tw, setTw] = useTweaks(window.TWEAK_DEFAULTS);

  // Theme: follow system unless overridden
  const [systemDark, setSystemDark] = useState_(window.matchMedia("(prefers-color-scheme: dark)").matches);
  useEffect_(() => {
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const h = (e) => setSystemDark(e.matches);
    mq.addEventListener("change", h);
    return () => mq.removeEventListener("change", h);
  }, []);
  const theme = tw.theme === "system" ? (systemDark ? "dark" : "light") : tw.theme;

  useEffect_(() => {
    document.documentElement.setAttribute("data-theme", theme);
    document.documentElement.style.setProperty("--accent", tw.accent);
    document.documentElement.style.setProperty("--accent-soft", hexToRGBA(tw.accent, 0.14));
    document.documentElement.style.setProperty("--accent-soft-strong", hexToRGBA(tw.accent, 0.22));
    document.documentElement.style.setProperty("--accent-strong", darken(tw.accent, 0.08));
  }, [theme, tw.accent]);

  const devices = MOCK.DEVICES;
  const [activeId, setActiveId] = useState_(devices[0].id);
  const [screen, setScreen] = useState_("logcat");
  const [settingsOpen, setSettingsOpen] = useState_(false);

  const device = devices.find(d => d.id === activeId) || devices[0];
  const [devState, setDevState] = useDeviceState(device.id, device);

  // counts for sidebar
  const counts = {
    apps: MOCK.APPS.length,
    forwards: (devState.forwards || MOCK.FORWARDS).length + (devState.reverses || MOCK.REVERSES).length,
  };

  const Screen = {
    overview: OverviewScreen,
    logcat:   LogcatScreen,
    shell:    ShellScreen,
    apps:     AppsScreen,
    files:    FilesScreen,
    forwards: ForwardsScreen,
    frida:    FridaScreen,
    network:  NetworkScreen,
  }[screen] || OverviewScreen;

  return (
    <div className="app">
      <Titlebar theme={theme} setTheme={(t) => setTw("theme", t)} onOpenSettings={() => setSettingsOpen(true)} />
      <DeviceTabs devices={devices} activeId={activeId} onSelect={setActiveId}
                  onClose={(id) => showToast({ title: "Disconnect device", body: `adb -s ${id} disconnect`, kind: "info", mono: true })}
                  onAdd={() => showToast({ title: "Connect device", body: "scan USB · adb pair · adb connect IP:5555", kind: "info", mono: true })} />
      <Sidebar device={device} screen={screen} setScreen={setScreen} counts={counts} />
      <main className="main">
        <Screen device={device} devState={devState} setDevState={setDevState} />
      </main>

      <SettingsModal open={settingsOpen} onClose={() => setSettingsOpen(false)}
                     theme={tw.theme} setTheme={(t) => setTw("theme", t)}
                     accent={tw.accent} setAccent={(c) => setTw("accent", c)} />
      <ToastHost />

      <TweaksPanel title="Tweaks">
        <TweakSection label="Appearance">
          <TweakRadio label="Theme" value={tw.theme} options={["light", "dark", "system"]}
            onChange={(v) => setTw("theme", v)} />
          <TweakColor label="Accent" value={tw.accent}
            options={["#a07cf7", "#7aa2ff", "#5ed29a", "#e9b454", "#ec6a73", "#c5a3ff"]}
            onChange={(v) => setTw("accent", v)} />
        </TweakSection>
      </TweaksPanel>
    </div>
  );
}

function hexToRGBA(hex, a) {
  const m = hex.replace("#", "");
  const n = parseInt(m.length === 3 ? m.split("").map(c => c + c).join("") : m, 16);
  const r = (n >> 16) & 255, g = (n >> 8) & 255, b = n & 255;
  return `rgba(${r}, ${g}, ${b}, ${a})`;
}
function darken(hex, amt) {
  const m = hex.replace("#", "");
  const n = parseInt(m.length === 3 ? m.split("").map(c => c + c).join("") : m, 16);
  let r = (n >> 16) & 255, g = (n >> 8) & 255, b = n & 255;
  r = Math.max(0, Math.round(r * (1 - amt)));
  g = Math.max(0, Math.round(g * (1 - amt)));
  b = Math.max(0, Math.round(b * (1 - amt)));
  return "#" + [r, g, b].map(x => x.toString(16).padStart(2, "0")).join("");
}

ReactDOM.createRoot(document.getElementById("root")).render(<App />);
