// Per-device persisted settings — keyed by device serial. Survives reloads.
const STORAGE_KEY = "adbq.state.v1";

function loadAll() {
  try {
    return JSON.parse(localStorage.getItem(STORAGE_KEY) || "{}");
  } catch { return {}; }
}
function saveAll(obj) {
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(obj)); } catch {}
}

function deviceDefaults(device) {
  return {
    label: device.label,
    proxy: {
      enabled: false,
      host: "",
      port: "8080",
      exclude: "localhost,127.0.0.1",
    },
    hosts: [
      // Custom DNS overrides — pushed to /etc/hosts on device
    ],
    capture: {
      iface: "wlan0",
      filter: "",
      sslDump: true,
    },
    frida: { iface: "0.0.0.0", port: 27042 },
    notes: "",
    pinnedApps: ["com.example.scanner"],
  };
}

function useDeviceState(deviceId, device) {
  const [state, setState] = React.useState(() => {
    const all = loadAll();
    return { ...deviceDefaults(device), ...(all[deviceId] || {}) };
  });
  // Reload when device changes
  React.useEffect(() => {
    const all = loadAll();
    setState({ ...deviceDefaults(device), ...(all[deviceId] || {}) });
  }, [deviceId]);

  const update = React.useCallback((patch) => {
    setState(prev => {
      const next = typeof patch === "function" ? patch(prev) : { ...prev, ...patch };
      const all = loadAll();
      all[deviceId] = next;
      saveAll(all);
      return next;
    });
  }, [deviceId]);

  return [state, update];
}

function listSavedDevices() {
  const all = loadAll();
  return Object.entries(all).map(([id, s]) => ({ id, label: s.label, hostsCount: (s.hosts || []).length, hasProxy: s.proxy?.enabled }));
}

function forgetDevice(id) {
  const all = loadAll();
  delete all[id];
  saveAll(all);
}

Object.assign(window, { useDeviceState, listSavedDevices, forgetDevice, deviceDefaults });
