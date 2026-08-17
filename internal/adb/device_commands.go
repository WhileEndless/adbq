package adb

import (
	"context"
	"strconv"
	"strings"
)

// Command previews for the whole-device actions and the streaming screens.
//
// These are the screens where "what is adbq doing right now" is hardest to
// answer from the outside: a log pane fills up, a process table refreshes, a
// capture counts packets. §4.1 K3 asks for the command to stay visible while
// they run, and K2 for it to appear before a reboot.

// DeviceCommands are the Overview screen's actions.
type DeviceCommands struct {
	Reboot           []string `json:"reboot"`
	RebootRecovery   []string `json:"rebootRecovery"`
	RebootBootloader []string `json:"rebootBootloader"`
	Tcpip            []string `json:"tcpip"`
	Screenshot       []string `json:"screenshot"`
	ScreenRecord     []string `json:"screenRecord"`
	PowerOff         []string `json:"powerOff"`
	RestartAdbd      []string `json:"restartAdbd"`
	RootProbe        []string `json:"rootProbe"`
	Scrcpy           []string `json:"scrcpy"`
}

// DeviceCommandsFor renders them. scrcpy is a host program adbq launches, so its
// line is passed in by the caller that knows where the binary is; an empty
// string means it is not installed and there is no command to show.
func DeviceCommandsFor(serial string, tcpipPort, recordSeconds int, scrcpy string, render CommandRenderer) DeviceCommands {
	if tcpipPort <= 0 {
		tcpipPort = 5555
	}
	if recordSeconds <= 0 || recordSeconds > 180 {
		recordSeconds = 180
	}
	remote := screenRecordRemote()
	dc := DeviceCommands{
		Reboot:           []string{DeviceCommandText(serial, "reboot")},
		RebootRecovery:   []string{DeviceCommandText(serial, "reboot", "recovery")},
		RebootBootloader: []string{DeviceCommandText(serial, "reboot", "bootloader")},
		Tcpip:            []string{DeviceCommandText(serial, "tcpip", strconv.Itoa(tcpipPort))},
		// screencap runs through exec-out because the PNG is binary; `shell`
		// mangles it on older devices, which is why adbq only falls back to it.
		Screenshot: []string{DeviceCommandText(serial, "exec-out", "screencap -p") + " > screenshot.png"},
		ScreenRecord: []string{
			DeviceCommandText(serial, "shell", "screenrecord", "--time-limit", strconv.Itoa(recordSeconds), remote),
			"# stopping early asks the device to finalise the container first:",
			render(screenRecordStopRemote(), false),
			DeviceCommandText(serial, "pull", remote, "screenrecord.mp4"),
			render("rm "+remote, false),
		},
		PowerOff:    []string{render(powerOffRemote(), true)},
		RestartAdbd: []string{render(restartAdbdRemote(), true)},
		RootProbe:   []string{render(rootProbeRemote(), false)},
	}
	if scrcpy != "" {
		dc.Scrcpy = []string{scrcpy}
	}
	return dc
}

// DeviceCommandsFor is the device-aware entry point.
func (c *Client) DeviceCommandsFor(ctx context.Context, serial string, tcpipPort, recordSeconds int, scrcpy string) DeviceCommands {
	return DeviceCommandsFor(serial, tcpipPort, recordSeconds, scrcpy, c.Renderer(ctx, serial))
}

// ConnectCommands is the pair behind the Wi-Fi connect dialog. Both are
// untargeted: they name the address rather than a serial, because the serial is
// what they produce.
type ConnectCommands struct {
	Connect    []string `json:"connect"`
	Disconnect []string `json:"disconnect"`
}

// ConnectCommandsFor renders them for one address.
func ConnectCommandsFor(addr string) ConnectCommands {
	if addr == "" {
		return ConnectCommands{}
	}
	return ConnectCommands{
		Connect:    []string{DeviceCommandText("", "connect", addr)},
		Disconnect: []string{DeviceCommandText("", "disconnect", addr)},
	}
}

// StreamCommands is a running stream's command plus the one that clears what it
// has collected — the shape the log pane needs.
type StreamCommands struct {
	Stream []string `json:"stream"`
	Clear  []string `json:"clear"`
}

// LogcatCommandsFor renders the logcat subscription and the clear that empties
// it. pid > 0 adds the --pid filter (API 24+), tail > 0 the -T backfill.
func LogcatCommandsFor(serial string, pid, tail int, render CommandRenderer) (stream, clear []string) {
	args := []string{"logcat", "-v", "threadtime"}
	if pid > 0 {
		args = append(args, "--pid="+strconv.Itoa(pid))
	}
	if tail > 0 {
		args = append(args, "-T", strconv.Itoa(tail))
	}
	// The explicit filterspec is not decoration: without it the device's
	// ANDROID_LOG_TAGS decides the level and V/D lines can vanish.
	args = append(args, "*:V")
	return []string{DeviceCommandText(serial, args...)},
		[]string{DeviceCommandText(serial, "logcat", "-c")}
}

// ProcessCommands renders the procfs sweep the process table polls. asRoot
// mirrors what the stream is doing: it starts as root and latches to the shell
// user when su is unavailable, and the preview should say which one is in use.
func ProcessCommands(serial string, asRoot bool, render CommandRenderer) []string {
	return []string{render(procfsCmd, asRoot)}
}

// CaptureCommandFor renders the live capture. It goes through exec-out rather
// than shell because the pcap stream is binary and a PTY would translate line
// endings inside it.
func CaptureCommandFor(serial, tcpdumpPath, iface, bpf string, rootWrap func(string) string) []string {
	if tcpdumpPath == "" {
		return nil
	}
	if iface == "" {
		iface = "any"
	}
	inner := liveTcpdumpInner(tcpdumpPath, iface, bpf, liveCaptureErrFile(serial))
	return []string{DeviceCommandText(serial, "exec-out", rootWrap(inner)) + " > capture.pcap"}
}

// CaptureCommandFor is the device-aware entry point. It resolves tcpdump and the
// device's `su` form, but deliberately does not probe the interface list the way
// a real start does — that is a second root round trip, and the rendered `-i`
// still names what was asked for.
func (c *Client) CaptureCommandFor(ctx context.Context, serial, iface, bpf string) []string {
	td, err := c.FindTcpdump(ctx, serial)
	if err != nil || td == "" {
		return nil
	}
	wrap := func(inner string) string {
		if remote, err := c.rootWrap(ctx, serial, inner); err == nil {
			return remote
		}
		return "su -c " + shQuote(inner)
	}
	return CaptureCommandFor(serial, td, iface, bpf, wrap)
}

// screenRecordRemote is where the fixed-duration recording (the one the Overview
// button runs) is written before being pulled. The interactive start/stop path
// timestamps its file instead, because that one can be left running while
// another is started.
func screenRecordRemote() string { return "/sdcard/adbq-screenrecord.mp4" }

// screenRecordStopRemote asks the device-side recorder to stop cleanly so the
// MP4 gets its header. Killing the adb child instead leaves a broken file.
func screenRecordStopRemote() string {
	return "killall -INT screenrecord || pkill -INT screenrecord"
}

// powerOffRemote shuts the device down. `reboot -p` is the portable spelling;
// `svc power shutdown` is missing on plenty of ROMs.
func powerOffRemote() string { return "reboot -p" }

// restartAdbdRemote bounces the device-side adb daemon. nohup + background is
// what keeps the command from being killed by the very connection it drops.
func restartAdbdRemote() string {
	return `nohup sh -c "stop adbd; sleep 1; start adbd" >/dev/null 2>&1 &`
}

// rootProbeRemote collects the signals adbq reads to decide a device is rooted,
// so "Re-test" can show what it looked at.
func rootProbeRemote() string {
	return "which su; ls -d /sbin/.magisk /data/adb/magisk 2>/dev/null; magisk -V 2>/dev/null"
}

// clipboardSetRemote sets the device clipboard, falling back to typing the text
// on Android versions without the clipboard service command.
func clipboardSetRemote(text string) string {
	esc := strings.ReplaceAll(text, "'", `'\''`)
	return "cmd clipboard set-text '" + esc + "' || input text '" + esc + "'"
}
