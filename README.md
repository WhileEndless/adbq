<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.png">
    <img src="docs/assets/logo-light.png" alt="adbq" width="315">
  </picture>
</p>

# adbq — ADB Manager

[![release](https://img.shields.io/github/v/release/WhileEndless/adbq?include_prereleases&sort=semver)](https://github.com/WhileEndless/adbq/releases)
[![CI](https://github.com/WhileEndless/adbq/actions/workflows/ci.yml/badge.svg)](https://github.com/WhileEndless/adbq/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Platforms](https://img.shields.io/badge/platforms-macOS%20%7C%20Windows%20%7C%20Linux-informational)](#download)

**adbq** is a cross-platform desktop ADB manager that wraps the common
day-to-day Android-debugging workflows — logcat, shell, app management, file
transfer, port forwards, frida-server, network/proxy control, packet capture,
iptables, and screenshots — in a fast, keyboard-friendly, Linear/Raycast-style
UI. Built with [Wails v2](https://wails.io) (Go backend + React/TypeScript
frontend), it ships as a small native binary on macOS, Windows, and Linux.

> 🇹🇷 **Türkçe:** Bu belgenin Türkçesi için [README.tr.md](README.tr.md).

---

## Table of contents

- [Features](#features)
- [Download](#download)
- [Requirements](#requirements)
- [Build from source](#build-from-source)
- [Cross-platform builds](#cross-platform-builds)
- [Usage notes & caveats](#usage-notes--caveats)
- [Versioning & releases](#versioning--releases)
- [Architecture](#architecture)
- [Development](#development)
- [License](#license)

---

## Features

- **Multi-device tabs** — every connected device gets its own tab; switch with one click.
- **Overview** — manufacturer, model, Android version, SDK, build, kernel, ABI,
  IP, MAC, Wi-Fi SSID, root method (Magisk / userdebug / su), live battery/RAM/CPU.
- **Logcat** — `adb logcat -v threadtime` streamed live, per-app PID filter (with
  a searchable picker for 100+ packages), level filter, text search with
  highlighting, export to `.txt`. OS-owned lines are hidden by default (one
  toggle brings them back) and the view is windowed, so a chatty device stays
  readable instead of drowning the screen in kernel audit spam. With an app
  selected, lines it repeats within 10 seconds collapse to the first one —
  toggleable, and honoured by Export. Scrolling up stops the auto-scroll from
  dragging you back; a pill offers the way to the newest line.
- **Shell** — multiple concurrent interactive sessions per device, including root
  sessions that auto-`su`.
- **Apps** — install (file picker), uninstall, force-stop, clear data, launch,
  kill, restart with live PID, export APK, dumpsys-driven details + permissions
  list, user/system filter.
- **Files** — `ls -lAp` listings, native push/pull pickers, mkdir, delete; root
  toggle via `su -c`. Recognises frida-server binaries and can start them from
  the detail pane.
- **Forwards** — list, add, remove for both `adb forward` and `adb reverse`, with
  one-click presets (DevTools, frida, Metro, mitmproxy).
- **Frida** — full host + device workflow. Device side: scans `/data/local/tmp/`
  for `frida-server-*`, detects the running PID, starts on a configurable port,
  stops, and one-click installs verified builds from GitHub. Host side
  ("Frida Manager"): provisions a version-matched Python `frida` venv (single
  wheel, SHA256-verified, offline `pip` install) or uses a bring-your-own
  interpreter; a script library with a CodeMirror editor; Frida CodeShare search/
  import; per-app script bindings; and one-click **Start/Attach with Frida** that
  spawns or attaches an app with your scripts and streams its `console.log`/
  `send()`/errors into a live console — searchable and filterable by kind
  (logs / sends / warnings / errors / lifecycle events), with match highlighting,
  auto-scroll handover, and text export of whatever is on screen.
- **Network** — interfaces, IPv4/MAC/gateway/DNS, Wi-Fi SSID, global HTTP proxy
  set/get/clear via `settings put global http_proxy`.
- **Packet capture** — in-app live capture & analysis (gopacket), on-device
  decode, full layer detail, Wireshark-syntax display filter, memory/disk caps,
  cross-screen persistence, save-after-stop, on-device `tcpdump` auto-install.
- **iptables** — on-device rule management with a safety undo.
- **Screenshot** — `adb exec-out screencap -p` captured to disk with a save dialog.
- **Theme** — light, dark, and system (follows `prefers-color-scheme`); accent
  palette persisted to `localStorage`.

## Download

Grab a prebuilt binary for your OS from the
**[Releases page](https://github.com/WhileEndless/adbq/releases)**:

| OS | Asset | Run |
|----|-------|-----|
| **macOS** (Apple Silicon + Intel) | `adbq-macos-universal.zip` | Unzip → drag `adbq.app` to Applications |
| **Windows** (x64) | `adbq-windows-amd64.zip` | Unzip → run `adbq.exe` |
| **Linux** (x64) | `adbq-linux-amd64.tar.gz` | `tar -xzf … && ./adbq` |

Verify what you have at any time:

```bash
adbq --version      # also: -v, or `version`
```

> **macOS Gatekeeper:** the builds are not code-signed/notarized yet, so the
> first launch may be blocked. Either right-click the app → **Open**, or clear
> the quarantine flag: `xattr -cr /Applications/adbq.app`.
>
> **Linux runtime deps:** install the WebKit GTK runtime if it is missing —
> e.g. `sudo apt install libgtk-3-0 libwebkit2gtk-4.0-37`.

## Requirements

To **run** adbq you only need the Android Platform Tools:

- The `adb` binary on your `PATH` (or under `$ANDROID_HOME/platform-tools`, the
  Homebrew `platform-tools` cask, or the default Android SDK location).

To **build from source** you additionally need:

- **Go 1.23+**
- **Node.js 18+** (20+ recommended)
- The **Wails v2 CLI**: `go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0`
- Platform toolchain that `wails doctor` reports as healthy:
  - **macOS:** Xcode command-line tools
  - **Windows:** WebView2 runtime (preinstalled on Windows 10/11) + a C compiler (MinGW or MSVC)
  - **Linux:** `gcc`, `libgtk-3-dev`, `libwebkit2gtk-4.0-dev`

## Build from source

```bash
git clone https://github.com/WhileEndless/adbq.git
cd adbq

wails doctor          # verify your toolchain first
make build            # → build/bin/  (or: wails build)
make run              # build, then launch
```

Common Makefile targets (`make` with no args prints them all):

| Target | What it does |
|--------|--------------|
| `make dev` | Hot-reload dev mode (Vite + Go) |
| `make build` | Default build for **your own system** into `build/bin/` |
| `make build-prod` | Stripped, `-trimpath`, version-stamped release build |
| `make build-mac` / `build-mac-intel` / `build-mac-arm` | macOS universal / Intel (x86_64) / Apple Silicon — any of these works on any Mac |
| `make build-universal` | macOS universal2 (arm64 + amd64), macOS only |
| `make build-linux` / `make build-windows` | Native build on that OS |
| `make build-target PLATFORM=os/arch` | Build any target directly, e.g. `PLATFORM=darwin/amd64` |
| `make test` | Go tests + frontend type-check |
| `make lint` | gofmt, go vet, staticcheck (if installed), tsc |
| `make version` | Print the version (single source of truth) |
| `make doctor` | `wails doctor` |

## Cross-platform builds

Wails apps embed a native webview per OS (WebKit on macOS/Linux, WebView2 on
Windows) and use CGO, so **a binary must be built on the OS it targets** — you
cannot reliably cross-compile a macOS `.app`, a Windows `.exe`, and a Linux ELF
from a single machine.

Different **architectures of the same OS**, however, *do* cross-build locally —
e.g. on an Apple-Silicon Mac you can produce an Intel/x86_64 build with
`make build-mac-intel` (or `make build-target PLATFORM=darwin/amd64`), and a
universal `.app` with `make build-mac`. `make build-target PLATFORM=os/arch` is
the generic escape hatch for any target you want to force.

adbq handles this with a **GitHub Actions matrix** that builds each target on
its own native runner and publishes the artifacts:

- **[`.github/workflows/ci.yml`](.github/workflows/ci.yml)** — on every push/PR,
  builds on macOS + Windows + Linux and runs vet/gofmt/tests. This is the proof
  that the app compiles everywhere.
- **[`.github/workflows/release.yml`](.github/workflows/release.yml)** — on a
  pushed `v*` tag, builds all three, then creates a GitHub Release and uploads
  `adbq-macos-universal.zip`, `adbq-windows-amd64.zip`, and
  `adbq-linux-amd64.tar.gz`.

So to ship binaries for all platforms you don't need three machines — just push
a tag (see below) and let CI produce the downloads.

## Usage notes & caveats

Several features depend on device state and are surfaced in the UI:

- **`frida-server` start** requires root; on unrooted devices the screen explains
  the limitation (use `frida-gadget` via repackaging instead).
- **Root toggles** (Files / capture / iptables) rely on `su` being on the shell
  user's `PATH` (Magisk standard). Failures are reported via toast.
- **`http_proxy`** affects the system HTTP stack only; apps with their own client
  (TLS pinning, custom DNS) ignore it.
- **Packet capture / tcpdump** install pulls a pinned `magisk-tcpdump` build onto
  the device; rooted devices only.
- **Clipboard** is intentionally not exposed: Android 10+ blocks background
  clipboard reads.

## Versioning & releases

The version lives in **exactly one place**:
[`internal/version/version.go`](internal/version/version.go). The UI
(`App.Version` binding), the `adbq --version` CLI flag, and the published git tag
all read from it, and CI **fails a release if the pushed tag does not match that
file** — so it can never drift.

adbq follows [Semantic Versioning](https://semver.org) with a `v` prefix; the
current line is a **beta** (`v0.1.0-beta`). Pre-1.0 means the UI and bindings may
still change between minor versions.

Cutting a release:

```bash
# 1. bump the literal in internal/version/version.go (e.g. v0.2.0)
# 2. commit it
git commit -am "release: v0.2.0"
# 3. tag it with the SAME value and push
git tag -a v0.2.0 -m "v0.2.0"
git push origin main --tags
# → release.yml builds all three OSes and publishes the GitHub Release
```

## Architecture

```
adbq/
├── app.go                  # Wails bindings: thin layer over internal/adb
├── main.go                 # Wails bootstrap + `--version` flag
├── internal/
│   ├── version/            # single source of truth for the version
│   └── adb/
│       ├── adb.go          # Client (binary lookup, exec, su wrapping)
│       ├── devices.go      # adb devices, getprop, root detection
│       ├── logcat.go       # streaming logcat (events)
│       ├── shell.go        # interactive shell sessions
│       ├── apps.go         # pm list / dumpsys / install / uninstall / pull
│       ├── files.go        # ls -lAp parser, push/pull/mkdir/rm
│       ├── forwards.go     # forward/reverse CRUD
│       ├── frida.go        # /data/local/tmp scanning + setsid+su launch
│       ├── network.go      # ip addr, dumpsys wifi, http_proxy
│       ├── iptables.go     # on-device rule management + undo
│       ├── tcpdump.go      # pinned magisk-tcpdump auto-install
│       ├── screenshot.go   # exec-out screencap -p
│       └── stats.go        # /proc/meminfo, /proc/loadavg, dumpsys battery
├── frontend/src/
│   ├── App.tsx             # shell (titlebar, device tabs, sidebar)
│   ├── ui.tsx              # theme hook, toasts, modals, search
│   ├── icons.tsx          # inline SVG set
│   └── screens/            # one file per screen, live-bound via Wails events
├── .github/workflows/      # ci.yml (build everywhere) + release.yml (publish)
└── design-reference/       # original design handoff (not built)
```

All exported `App` methods are auto-bound and type-safe via `wails generate
module`.

## Development

```bash
make dev               # hot-reload (Vite + Go)
make test              # Go tests + frontend type-check
make lint              # gofmt, go vet, staticcheck, tsc
```

The Go backend ships both unit tests (pure parsers, no device) and integration
tests that hit the first online ADB device:

```bash
go test ./...                          # unit + integration
ADBQ_SKIP_DEVICE=1 go test ./...       # skip device-touching tests
```

Contributions should keep `make lint`, `make test`, and `wails doctor` green —
see [`CLAUDE.md`](CLAUDE.md) and [`docs/`](docs/) for the full project rules.

## License

Released under the [MIT License](LICENSE). © 2026 WhileEndless.
