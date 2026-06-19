// Mock data shared across screens — globals so all babel scripts can read them.
(function () {
  const DEVICES = [
    {
      id: "R5CR40XYZAB",
      label: "Pixel 8",
      manufacturer: "Google",
      model: "Pixel 8",
      product: "shiba",
      androidVersion: "14",
      sdk: 34,
      buildId: "UQ1A.240205.004",
      kernel: "5.15.110-android14-11",
      cpu: "Tensor G3",
      arch: "arm64-v8a",
      ramTotal: 8192,
      storageTotal: 128,
      storageFree: 47.3,
      battery: { level: 87, charging: true, temp: 31.2, voltage: 4321, health: "Good" },
      ip: "192.168.1.42",
      wifi: "Home-5G",
      mac: "9C:6B:00:A1:42:7E",
      root: true,
      rootMethod: "Magisk",
      online: true,
      via: "USB",
      transport: "usb-2A1A100000",
    },
    {
      id: "emulator-5554",
      label: "Pixel_6_API_33",
      manufacturer: "Google",
      model: "sdk_gphone64_arm64",
      product: "sdk_gphone64_arm64",
      androidVersion: "13",
      sdk: 33,
      buildId: "TPB4.220624.004",
      kernel: "6.1.21-android14",
      cpu: "Host CPU",
      arch: "arm64-v8a",
      ramTotal: 4096,
      storageTotal: 32,
      storageFree: 11.2,
      battery: { level: 100, charging: true, temp: 25.0, voltage: 4200, health: "—" },
      ip: "10.0.2.15",
      wifi: "—",
      mac: "02:00:00:44:55:66",
      root: true,
      rootMethod: "AOSP userdebug",
      online: true,
      via: "Emulator",
      transport: "tcp-127.0.0.1:5554",
    },
    {
      id: "192.168.1.51:5555",
      label: "Galaxy S22",
      manufacturer: "Samsung",
      model: "SM-S901B",
      product: "r0qxx",
      androidVersion: "14",
      sdk: 34,
      buildId: "UP1A.231005.007",
      kernel: "5.10.157",
      cpu: "Exynos 2200",
      arch: "arm64-v8a",
      ramTotal: 8192,
      storageTotal: 256,
      storageFree: 83.5,
      battery: { level: 64, charging: false, temp: 33.4, voltage: 4011, health: "Good" },
      ip: "192.168.1.51",
      wifi: "Home-5G",
      mac: "A8:8C:3E:12:8B:1F",
      root: false,
      rootMethod: null,
      online: true,
      via: "Wi-Fi",
      transport: "tcp-192.168.1.51:5555",
    },
  ];

  const TAG_COLORS = {
    ActivityManager: "#7aa2ff",
    PackageManager: "#7aa2ff",
    WindowManager: "#8bb6ff",
    InputDispatcher: "#a07cf7",
    audio_hw_primary: "#e9b454",
    NetworkScheduler: "#5ed29a",
    System: "#5ed29a",
    GraalVM: "#d472f0",
    OkHttp: "#5ed29a",
    Choreographer: "#e9b454",
    BluetoothAdapter: "#6fb3ff",
    SQLiteOpenHelper: "#a07cf7",
    Frida: "#c5a3ff",
    ART: "#ec6a73",
    chatty: "#8a8a96",
    libc: "#ec6a73",
    Zygote: "#7aa2ff",
    CameraService: "#ec6a73",
  };

  // Apps installed (with mocked icon colors) — extended list so the picker needs search
  const APPS = [
    { pkg: "com.android.chrome", name: "Chrome", v: "121.0.6167.143", size: 184.2, system: true, icon: { bg: "#4285F4", l: "C" } },
    { pkg: "com.example.scanner", name: "Example Scanner", v: "2.4.1", size: 42.7, system: false, icon: { bg: "#a07cf7", l: "EX" } },
    { pkg: "com.spotify.music", name: "Spotify", v: "8.9.62.522", size: 117.4, system: false, icon: { bg: "#1ed760", l: "S" } },
    { pkg: "com.whatsapp", name: "WhatsApp", v: "2.24.6.78", size: 162.1, system: false, icon: { bg: "#25D366", l: "W" } },
    { pkg: "com.slack", name: "Slack", v: "24.03.10.0", size: 96.3, system: false, icon: { bg: "#611f69", l: "#" } },
    { pkg: "com.google.android.gms", name: "Google Play services", v: "24.07.13", size: 312.8, system: true, icon: { bg: "#34a853", l: "G" } },
    { pkg: "com.android.settings", name: "Settings", v: "14", size: 18.4, system: true, icon: { bg: "#5f6368", l: "S" } },
    { pkg: "com.termux", name: "Termux", v: "0.118.1", size: 26.5, system: false, icon: { bg: "#222", l: ">_" } },
    { pkg: "re.frida.gadget", name: "Frida Gadget", v: "16.4.7", size: 22.9, system: false, icon: { bg: "#1f1f1f", l: "ƒ", color: "#c5a3ff" } },
    { pkg: "com.discord", name: "Discord", v: "229.18", size: 142.1, system: false, icon: { bg: "#5865F2", l: "D" } },
    { pkg: "com.figma.android", name: "Figma", v: "115.5", size: 53.0, system: false, icon: { bg: "#0d0d0d", l: "F" } },
    { pkg: "com.example.devtools", name: "Example DevTools", v: "0.9.2-beta", size: 18.3, system: false, icon: { bg: "#8a62f0", l: "EX" } },
    { pkg: "com.example.proxy", name: "Example Proxy", v: "1.2.0", size: 8.1, system: false, icon: { bg: "#7aa2ff", l: "EP" } },
    { pkg: "org.mozilla.firefox", name: "Firefox", v: "124.1.0", size: 102.7, system: false, icon: { bg: "#ff7139", l: "F" } },
    { pkg: "com.github.android", name: "GitHub", v: "1.156.0", size: 38.4, system: false, icon: { bg: "#0d1117", l: "GH" } },
    { pkg: "com.linear.app", name: "Linear", v: "1.16.0", size: 24.8, system: false, icon: { bg: "#5e6ad2", l: "L" } },
    { pkg: "com.notion", name: "Notion", v: "0.0.220", size: 64.1, system: false, icon: { bg: "#000000", l: "N" } },
    { pkg: "com.microsoft.teams", name: "Teams", v: "1416/1.0", size: 178.2, system: false, icon: { bg: "#5059c9", l: "T" } },
    { pkg: "com.android.systemui", name: "System UI", v: "14", size: 78.2, system: true, icon: { bg: "#5f6368", l: "UI" } },
    { pkg: "com.android.providers.media", name: "Media Storage", v: "14", size: 12.4, system: true, icon: { bg: "#5f6368", l: "M" } },
    { pkg: "com.android.bluetooth", name: "Bluetooth", v: "14", size: 9.8, system: true, icon: { bg: "#0066cc", l: "BT" } },
    { pkg: "com.android.vending", name: "Play Store", v: "39.3.31", size: 122.6, system: true, icon: { bg: "#34a853", l: "P" } },
    { pkg: "com.brave.browser", name: "Brave", v: "1.63", size: 178.0, system: false, icon: { bg: "#fb542b", l: "B" } },
    { pkg: "io.flutter.gallery", name: "Flutter Gallery", v: "3.16.0", size: 41.2, system: false, icon: { bg: "#02569B", l: "FL" } },
    { pkg: "com.facebook.katana", name: "Facebook", v: "457.0.0", size: 287.4, system: false, icon: { bg: "#1877f2", l: "f" } },
    { pkg: "com.instagram.android", name: "Instagram", v: "324.0.0", size: 121.5, system: false, icon: { bg: "#e1306c", l: "ig" } },
    { pkg: "com.twitter.android", name: "X", v: "10.42.0", size: 88.3, system: false, icon: { bg: "#000000", l: "X" } },
    { pkg: "com.reddit.frontpage", name: "Reddit", v: "2024.18.0", size: 71.2, system: false, icon: { bg: "#ff4500", l: "R" } },
  ];

  const FORWARDS = [
    { local: "tcp:8080", remote: "tcp:8080", proc: "Chrome DevTools", added: "2m ago" },
    { local: "tcp:27042", remote: "tcp:27042", proc: "frida-server", added: "12m ago" },
    { local: "tcp:9229", remote: "tcp:9229", proc: "node inspector", added: "1h ago" },
  ];

  const REVERSES = [
    { remote: "tcp:8081", local: "tcp:8081", proc: "Metro bundler", added: "3m ago" },
    { remote: "tcp:8090", local: "tcp:8090", proc: "mitmproxy", added: "5m ago" },
  ];

  const FRIDA_SERVERS = [
    { name: "frida-server-16.4.7-android-arm64", version: "16.4.7", arch: "arm64", size: 38.1, perms: "rwxr-xr-x", active: true, pid: 4421, port: 27042 },
    { name: "frida-server-16.2.1-android-arm64", version: "16.2.1", arch: "arm64", size: 36.8, perms: "rwxr-xr-x", active: false },
    { name: "frida-server-15.2.2-android-arm64", version: "15.2.2", arch: "arm64", size: 34.1, perms: "rwxr-x---", active: false },
  ];

  const FRIDA_PROCS = [
    { pid: 19842, name: "com.example.scanner", attached: true, script: "bypass_ssl_pin.js" },
    { pid: 18221, name: "com.spotify.music", attached: false, script: null },
  ];

  // Directory listing for /data/local/tmp
  const FILES = [
    { name: "..", type: "up" },
    { name: "frida-server-16.4.7-android-arm64", type: "file", size: 38_092_344, perms: "-rwxr-xr-x", owner: "shell", group: "shell", mtime: "2026-05-12 14:21" },
    { name: "frida-server-16.2.1-android-arm64", type: "file", size: 36_812_018, perms: "-rwxr-xr-x", owner: "shell", group: "shell", mtime: "2026-04-02 09:14" },
    { name: "frida-server-15.2.2-android-arm64", type: "file", size: 34_119_944, perms: "-rwxr-x---", owner: "shell", group: "shell", mtime: "2026-01-18 18:02" },
    { name: "magiskboot",       type: "file", size: 2_098_176, perms: "-rwxr-xr-x", owner: "shell", group: "shell", mtime: "2026-04-22 11:30" },
    { name: "tcpdump",          type: "file", size: 2_447_872, perms: "-rwxr-xr-x", owner: "root",  group: "shell", mtime: "2026-03-14 02:01" },
    { name: "burp-cert.der",    type: "file", size: 1_298,     perms: "-rw-r--r--", owner: "shell", group: "shell", mtime: "2026-05-18 09:42" },
    { name: "screen.png",       type: "file", size: 1_242_889, perms: "-rw-r--r--", owner: "shell", group: "shell", mtime: "2026-05-22 12:08" },
    { name: "scripts",          type: "dir",  size: null,      perms: "drwxr-xr-x", owner: "shell", group: "shell", mtime: "2026-05-01 18:12" },
    { name: "captures",         type: "dir",  size: null,      perms: "drwxr-xr-x", owner: "shell", group: "shell", mtime: "2026-05-10 11:22" },
    { name: "._cache",          type: "dir",  size: null,      perms: "drwx------", owner: "shell", group: "shell", mtime: "2026-04-30 21:55" },
  ];

  // Tree
  const TREE = [
    {
      name: "/",
      children: [
        {
          name: "data",
          children: [
            { name: "app" },
            { name: "data" },
            {
              name: "local",
              children: [{ name: "tmp", active: true }],
            },
            { name: "media" },
          ],
        },
        { name: "sdcard", children: [
          { name: "Android" },
          { name: "DCIM" },
          { name: "Download" },
          { name: "Pictures" },
        ]},
        { name: "system", children: [
          { name: "bin" },
          { name: "etc" },
          { name: "lib64" },
        ]},
        { name: "vendor" },
      ],
    },
  ];

  // Logcat: pre-generated mix of levels
  const LEVELS = ["V", "D", "I", "W", "E"];
  function pad(n, w) { return String(n).padStart(w, "0"); }
  function genTime(secOff) {
    const base = new Date("2026-05-22T12:18:04");
    const t = new Date(base.getTime() + secOff * 1000);
    return `${pad(t.getMonth() + 1, 2)}-${pad(t.getDate(), 2)} ${pad(t.getHours(), 2)}:${pad(t.getMinutes(), 2)}:${pad(t.getSeconds(), 2)}.${pad(Math.floor(Math.random() * 1000), 3)}`;
  }

  const LOG_LINES_RAW = [
    ["I", "ActivityManager", 1582, 1612, "Start proc 19842:com.example.scanner/u0a214 for activity {com.example.scanner/.ScanActivity}"],
    ["D", "OkHttp", 19842, 19870, "--> GET https://api.example.com/v2/scan/init"],
    ["V", "Choreographer", 19842, 19842, "Skipped 1 frames!  The application may be doing too much work on its main thread."],
    ["I", "Frida", 4421, 4421, "Loaded script: bypass_ssl_pin.js for pid 19842"],
    ["D", "OkHttp", 19842, 19870, "<-- 200 https://api.example.com/v2/scan/init (412ms)"],
    ["I", "ScanActivity", 19842, 19842, "Initialized scanner module v2.4.1"],
    ["D", "BluetoothAdapter", 1240, 1240, "isLeEnabled() = false"],
    ["W", "ART", 19842, 19842, "Suspending all threads took: 12.348ms"],
    ["E", "OkHttp", 19842, 19870, "Connection reset by peer at api.staging.example.com/auth"],
    ["I", "NetworkScheduler", 1582, 1602, "Schedule(): App: com.spotify.music task: prefetch-30s"],
    ["D", "InputDispatcher", 1582, 1612, "Dispatch reason=MOTION, target=com.example.scanner/.ScanActivity"],
    ["I", "ActivityManager", 1582, 1612, "Displayed com.example.scanner/.ScanActivity: +218ms"],
    ["W", "SQLiteOpenHelper", 19842, 19842, "getDatabase called recursively"],
    ["D", "OkHttp", 19842, 19870, "--> POST https://api.example.com/v2/events Content-Length: 412"],
    ["V", "audio_hw_primary", 814, 814, "out_get_presentation_position: frames=8192 ts.tv_sec=1716381489"],
    ["I", "System", 19842, 19842, "ImageReader: format=RGBA_8888 size=2048x2048"],
    ["E", "ART", 19842, 19842, "JNI ERROR (app bug): accessed deleted local reference 0x14"],
    ["I", "Frida", 4421, 4421, "Detached from process 18221"],
    ["W", "Choreographer", 19842, 19842, "Skipped 3 frames!"],
    ["D", "OkHttp", 19842, 19870, "<-- 201 https://api.example.com/v2/events (148ms)"],
    ["I", "PackageManager", 1582, 1612, "Force stopping com.figma.android appid=10218 user=0: from pid 4321"],
    ["D", "GraalVM", 19842, 19842, "Compilation queue: 4 tasks pending"],
    ["I", "WindowManager", 1582, 1612, "Relayout Window{91f2bd1 u0 com.example.scanner/.ScanActivity}"],
    ["V", "Zygote", 19842, 19842, "Forked PID 19870 for OkHttp dispatcher"],
    ["W", "libc", 19842, 19842, "Access denied finding property \"net.dns1\""],
    ["I", "CameraService", 814, 814, "openCameraDevice : Camera 0 opened by client UID=10214"],
    ["D", "InputDispatcher", 1582, 1612, "Resampled motion event from queue: x=512.4 y=1024.8"],
    ["E", "ScanActivity", 19842, 19842, "Failed to parse manifest: invalid signature block"],
    ["I", "ActivityManager", 1582, 1612, "Killing 18221:com.spotify.music/u0a212 (adj 905): empty for 1812s"],
    ["D", "OkHttp", 19842, 19870, "--> GET https://api.example.com/v2/scan/result/8c12a"],
    ["V", "chatty", 814, 814, "uid=1041(audioserver) Binder:814_3 expire 13 lines"],
    ["I", "System", 19842, 19842, "GC complete: alloc=128.3 MB free=24.1 MB pause=8ms"],
    ["W", "BluetoothAdapter", 1240, 1240, "OOB error: device disappeared during pairing"],
    ["D", "ActivityManager", 1582, 1612, "uid 10214 process com.example.scanner state=TOP"],
    ["I", "OkHttp", 19842, 19870, "<-- 200 https://api.example.com/v2/scan/result/8c12a (89ms)"],
  ];

  const LOG_LINES_BASE = LOG_LINES_RAW.map(([lvl, tag, pid, tid, msg], i) => ({
    time: genTime(i * 0.18),
    lvl, tag, pid, tid, msg,
  }));
  // Repeat into a long stream to exercise virtualization (~3.5k lines)
  const LOG_LINES = [];
  for (let k = 0; k < 100; k++) {
    for (let i = 0; i < LOG_LINES_BASE.length; i++) {
      const base = LOG_LINES_BASE[i];
      LOG_LINES.push({
        ...base,
        time: genTime((k * LOG_LINES_BASE.length + i) * 0.07),
        tid: base.tid + (k % 4),
      });
    }
  }

  // Live network connections (netstat -tunp style) — expanded
  const NET_BASE = [
    { proto: "tcp", local: "192.168.1.42:46782", remote: "151.101.0.81:443",     state: "ESTABLISHED", proc: "com.spotify.music",      pid: 18221, rx: "428 KB", tx: "12.4 KB" },
    { proto: "tcp", local: "192.168.1.42:48104", remote: "172.217.169.74:443",   state: "ESTABLISHED", proc: "com.google.android.gms", pid: 1042,  rx: "84 KB",  tx: "9.2 KB" },
    { proto: "tcp", local: "192.168.1.42:51220", remote: "44.226.122.3:443",     state: "ESTABLISHED", proc: "com.example.scanner",  pid: 19842, rx: "12.1 KB", tx: "4.8 KB" },
    { proto: "tcp", local: "192.168.1.42:51221", remote: "44.226.122.3:443",     state: "ESTABLISHED", proc: "com.example.scanner",  pid: 19842, rx: "8.4 KB",  tx: "2.1 KB" },
    { proto: "tcp", local: "192.168.1.42:52004", remote: "162.159.135.232:443",  state: "ESTABLISHED", proc: "com.discord",            pid: 17120, rx: "118 KB", tx: "3.4 KB" },
    { proto: "tcp", local: "192.168.1.42:53120", remote: "157.240.241.18:443",   state: "TIME_WAIT",   proc: "com.whatsapp",           pid: 17981, rx: "0",      tx: "0" },
    { proto: "udp", local: "192.168.1.42:5353",  remote: "224.0.0.251:5353",     state: "—",           proc: "mdnsd",                  pid: 421,   rx: "12 KB",  tx: "4 KB" },
    { proto: "tcp", local: "127.0.0.1:27042",    remote: "127.0.0.1:48922",      state: "ESTABLISHED", proc: "frida-server",           pid: 4421,  rx: "2.4 MB", tx: "1.1 MB" },
    { proto: "tcp", local: "192.168.1.42:34112", remote: "104.18.32.42:443",     state: "ESTABLISHED", proc: "com.android.chrome",     pid: 12044, rx: "1.2 MB", tx: "44 KB" },
    { proto: "tcp", local: "0.0.0.0:5555",       remote: "0.0.0.0:*",            state: "LISTEN",      proc: "adbd",                   pid: 285,   rx: "—",      tx: "—" },
  ];
  const NET_CONNECTIONS = [];
  for (let k = 0; k < 18; k++) {
    NET_BASE.forEach((c, j) => {
      NET_CONNECTIONS.push({
        ...c,
        local: c.local.replace(/(\d+)$/, (m) => String(+m + k * 7 % 50000)),
      });
    });
  }

  // DNS queries (live log) — expanded to stress virtualization
  const DNS_BASE = [
    { host: "api.example.com",          type: "A",    answer: "44.226.122.3",   proc: "com.example.scanner",  cached: false },
    { host: "api.example.com",          type: "AAAA", answer: "—",              proc: "com.example.scanner",  cached: false },
    { host: "ap-gew4.spotify.com",        type: "A",    answer: "151.101.0.81",   proc: "com.spotify.music",      cached: true  },
    { host: "spclient.wg.spotify.com",    type: "A",    answer: "35.186.224.25",  proc: "com.spotify.music",      cached: false },
    { host: "play.googleapis.com",        type: "A",    answer: "172.217.169.74", proc: "com.google.android.gms", cached: true  },
    { host: "api.staging.example.com",  type: "A",    answer: "NXDOMAIN",       proc: "com.example.scanner",  cached: false, err: true },
    { host: "discord.com",                type: "A",    answer: "162.159.135.232",proc: "com.discord",            cached: false },
    { host: "fcm.googleapis.com",         type: "A",    answer: "142.250.184.74", proc: "com.google.android.gms", cached: true  },
    { host: "cdn.example.com",          type: "A",    answer: "104.18.32.42",   proc: "com.example.scanner",  cached: false },
    { host: "www.googleapis.com",         type: "A",    answer: "142.250.184.74", proc: "com.android.chrome",     cached: true  },
    { host: "graph.facebook.com",         type: "A",    answer: "157.240.241.13", proc: "com.facebook.katana",    cached: false },
    { host: "edge-mqtt.facebook.com",     type: "A",    answer: "157.240.20.10",  proc: "com.facebook.katana",    cached: false },
    { host: "i.instagram.com",            type: "A",    answer: "31.13.71.16",    proc: "com.instagram.android",  cached: false },
    { host: "api.twitter.com",            type: "A",    answer: "104.244.42.130", proc: "com.twitter.android",    cached: true  },
    { host: "oauth.reddit.com",           type: "A",    answer: "151.101.65.140", proc: "com.reddit.frontpage",   cached: false },
    { host: "metrics.example.com",      type: "A",    answer: "44.226.122.7",   proc: "com.example.scanner",  cached: false },
    { host: "sentry.io",                  type: "A",    answer: "35.186.247.156", proc: "com.example.scanner",  cached: true  },
    { host: "captive.apple.com",          type: "A",    answer: "17.253.103.201", proc: "system_server",          cached: false },
    { host: "time.android.com",           type: "A",    answer: "216.239.35.4",   proc: "system_server",          cached: true  },
    { host: "mtalk.google.com",           type: "A",    answer: "108.177.122.188",proc: "com.google.android.gms", cached: false },
  ];
  const DNS_QUERIES = [];
  for (let k = 0; k < 30; k++) {
    DNS_BASE.forEach((d, j) => {
      DNS_QUERIES.push({
        ...d,
        time: genTime((k * DNS_BASE.length + j) * 0.32 + 100),
        ms: Math.max(0, Math.round((d.cached ? 2 : 18) + (Math.random() * 30 - 6))),
      });
    });
  }

  // Saved packet captures
  const CAPTURES = [
    { name: "scanner-init.pcap",  size: 4_812_318, packets: 1248, started: "12:08:14", duration: "1m 22s",  filter: "host api.example.com" },
    { name: "spotify-prefetch.pcap", size: 12_402_118, packets: 4422, started: "11:42:01", duration: "5m 04s", filter: "tcp port 443" },
    { name: "dns-anomaly.pcap",   size: 184_018, packets: 92, started: "09:14:33", duration: "30s", filter: "udp port 53" },
  ];

  window.MOCK = { DEVICES, TAG_COLORS, APPS, FORWARDS, REVERSES, FRIDA_SERVERS, FRIDA_PROCS, FILES, TREE, LOG_LINES, NET_CONNECTIONS, DNS_QUERIES, CAPTURES };
})();
