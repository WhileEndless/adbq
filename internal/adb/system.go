package adb

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Reboot triggers a reboot of the device. mode: "" (regular), "recovery",
// "bootloader", or "fastboot".
func (c *Client) Reboot(ctx context.Context, serial, mode string) (string, error) {
	args := []string{"reboot"}
	if mode != "" {
		args = append(args, mode)
	}
	cmd, err := c.DeviceCommand(ctx, serial, args...)
	if err != nil {
		return "", err
	}
	return Run(cmd)
}

// PowerOff shuts the device down. Root first, because a stock shell user is not
// allowed to halt the device; the plain shell is tried anyway since userdebug
// builds and emulators accept it.
func (c *Client) PowerOff(ctx context.Context, serial string) (string, error) {
	if out, _, err := c.ShellSU(ctx, serial, powerOffRemote()); err == nil {
		return out, nil
	}
	return c.Shell(ctx, serial, powerOffRemote())
}

// RestartAdbd bounces the device-side daemon, which drops the current
// connection — the caller is expected to have warned the user.
func (c *Client) RestartAdbd(ctx context.Context, serial string) (string, error) {
	out, _, err := c.ShellSU(ctx, serial, restartAdbdRemote())
	return out, err
}

// RootSignals re-reads the evidence behind the root badge.
func (c *Client) RootSignals(ctx context.Context, serial string) (string, error) {
	return c.Shell(ctx, serial, rootProbeRemote())
}

// collapseIPv6 returns "::" notation by collapsing the longest run of zero
// hextets, mirroring `net.IP.String()` behavior for typical addresses.
func collapseIPv6(hextets []string) string {
	bestStart, bestLen := -1, 0
	i := 0
	for i < len(hextets) {
		if hextets[i] != "0" {
			i++
			continue
		}
		j := i
		for j < len(hextets) && hextets[j] == "0" {
			j++
		}
		if j-i > bestLen && j-i >= 2 {
			bestStart = i
			bestLen = j - i
		}
		i = j
	}
	if bestStart < 0 {
		return strings.Join(hextets, ":")
	}
	left := strings.Join(hextets[:bestStart], ":")
	right := strings.Join(hextets[bestStart+bestLen:], ":")
	return left + "::" + right
}

// Connection is one socket from /proc/net/tcp{,6} or udp{,6}.
type Connection struct {
	Proto      string `json:"proto"`
	LocalAddr  string `json:"local"`
	RemoteAddr string `json:"remote"`
	State      string `json:"state"`
	UID        int    `json:"uid"`
	Inode      string `json:"inode"`
}

var tcpStates = map[string]string{
	"01": "ESTABLISHED", "02": "SYN_SENT", "03": "SYN_RECV", "04": "FIN_WAIT1",
	"05": "FIN_WAIT2", "06": "TIME_WAIT", "07": "CLOSE", "08": "CLOSE_WAIT",
	"09": "LAST_ACK", "0A": "LISTEN", "0B": "CLOSING",
}

// ListConnections reads /proc/net/tcp(6) and /proc/net/udp(6) from the device
// in a single round trip, and parses the four tables apart host-side.
//
// It used to issue one `adb shell` per table while connectionsRemote() showed
// the user all four joined into one command — so the Network panel, refreshing
// every few seconds, was displaying a command it did not run. CLAUDE.md §4.1
// requires the preview and the execution to come from the same function, which
// is now literally true: both call connectionsRemote().
func (c *Client) ListConnections(ctx context.Context, serial string) ([]Connection, error) {
	out, err := c.Shell(ctx, serial, connectionsRemote())
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}
	sections := strings.Split(out, procNetSentinel)
	conns := []Connection{}
	for i, src := range procNetSources {
		if i >= len(sections) {
			break
		}
		conns = append(conns, parseProcNet(sections[i], src.proto)...)
	}
	return conns, nil
}

// procNetSources are the procfs tables the connection list is built from. `ss`
// would be shorter but is absent on stripped ROMs.
var procNetSources = []struct{ proto, path string }{
	{"tcp", "/proc/net/tcp"}, {"tcp6", "/proc/net/tcp6"},
	{"udp", "/proc/net/udp"}, {"udp6", "/proc/net/udp6"},
}

// procNetSentinel separates the four tables in the batched read. It has to be
// echoed rather than inferred, because an unreadable table produces no output
// at all and the sections would otherwise shift — silently labelling udp rows
// as tcp6.
const procNetSentinel = "@@@"

// connectionsRemote renders the reads ListConnections performs — and is the
// command it actually runs, not a description of it.
func connectionsRemote() string {
	parts := make([]string, 0, len(procNetSources))
	for _, src := range procNetSources {
		parts = append(parts, "cat "+src.path+" 2>/dev/null")
	}
	return strings.Join(parts, "; echo '"+procNetSentinel+"'; ")
}

// isProcNetIndex reports whether a token is a /proc/net `sl` column: decimal
// digits followed by a colon.
func isProcNetIndex(tok string) bool {
	if len(tok) < 2 || tok[len(tok)-1] != ':' {
		return false
	}
	for i := 0; i < len(tok)-1; i++ {
		if tok[i] < '0' || tok[i] > '9' {
			return false
		}
	}
	return true
}

func parseProcNet(out, proto string) []Connection {
	res := []Connection{}
	for _, ln := range strings.Split(out, "\n") {
		fs := strings.Fields(ln)
		if len(fs) < 10 {
			continue
		}
		// Skip the header by recognising it, not by position. Positional
		// skipping worked only while each table arrived as its own `cat`
		// output; batching the four reads into one round trip puts a blank
		// line before each header, which shifted the header into the body and
		// produced a phantom row reading "local_address → rem_address".
		//
		// A data row always begins with the `sl` index, "0:" / "466:".
		if !isProcNetIndex(fs[0]) {
			continue
		}
		local := decodeProcAddr(fs[1])
		remote := decodeProcAddr(fs[2])
		state := tcpStates[strings.ToUpper(fs[3])]
		if state == "" && strings.HasPrefix(proto, "udp") {
			state = "—"
		}
		uid, _ := strconv.Atoi(fs[7])
		res = append(res, Connection{
			Proto:      proto,
			LocalAddr:  local,
			RemoteAddr: remote,
			State:      state,
			UID:        uid,
			Inode:      fs[9],
		})
	}
	return res
}

func decodeProcAddr(s string) string {
	// IPv4: "AABBCCDD:PORT" (little-endian hex). IPv6: 32 hex chars then ":port".
	if i := strings.Index(s, ":"); i > 0 {
		addr := s[:i]
		portHex := s[i+1:]
		port, _ := strconv.ParseInt(portHex, 16, 32)
		if len(addr) == 8 {
			b, err := hex.DecodeString(addr)
			if err == nil && len(b) == 4 {
				return fmt.Sprintf("%d.%d.%d.%d:%d", b[3], b[2], b[1], b[0], port)
			}
		}
		if len(addr) == 32 {
			// /proc/net/tcp6 stores 4 little-endian 32-bit words. Reverse each word
			// to get the natural byte order, then group as 8 colon-separated hextets.
			b, err := hex.DecodeString(addr)
			if err == nil && len(b) == 16 {
				// reverse each 4-byte word
				for wi := 0; wi < 16; wi += 4 {
					b[wi], b[wi+3] = b[wi+3], b[wi]
					b[wi+1], b[wi+2] = b[wi+2], b[wi+1]
				}
				groups := make([]string, 8)
				for j := 0; j < 8; j++ {
					groups[j] = fmt.Sprintf("%x", uint16(b[j*2])<<8|uint16(b[j*2+1]))
				}
				return "[" + collapseIPv6(groups) + "]:" + strconv.FormatInt(port, 10)
			}
		}
		return addr + ":" + strconv.FormatInt(port, 10)
	}
	return s
}

// PushFridaBinary asks for a host file via runtime and pushes it to
// /data/local/tmp/ + chmod 755. Used by the Frida screen.
func (c *Client) PushFridaBinary(ctx context.Context, serial, localPath string) (string, error) {
	// derive remote name
	base := localPath
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '/' {
			base = base[i+1:]
			break
		}
	}
	remote := "/data/local/tmp/" + base
	if _, err := c.PushFile(ctx, serial, localPath, remote); err != nil {
		return "", err
	}
	if _, err := c.Shell(ctx, serial, "chmod 755 "+remote); err != nil {
		return remote, err
	}
	return remote, nil
}

// TcpipMode puts the adbd on the device into TCP mode on the given port.
// After this you can `adb connect ip:port` over Wi-Fi.
func (c *Client) TcpipMode(ctx context.Context, serial string, port int) (string, error) {
	cmd, err := c.DeviceCommand(ctx, serial, "tcpip", strconv.Itoa(port))
	if err != nil {
		return "", err
	}
	return Run(cmd)
}

// StartScreenRecord launches `adb shell screenrecord` in the background.
// Returns the remote path so a later StopScreenRecord can pull it. The caller
// holds the *exec.Cmd via the returned ScreenRecordSession.
type ScreenRecordSession struct {
	Serial     string
	RemotePath string
	Cancel     context.CancelFunc
	Done       chan struct{}
}

// StartScreenRecord begins a recording. `maxSec` caps the duration (Android
// hard-caps at 180s anyway). Returns a session handle the caller can stop.
func (c *Client) StartScreenRecord(parent context.Context, serial string, maxSec int) (*ScreenRecordSession, error) {
	if maxSec <= 0 || maxSec > 180 {
		maxSec = 180
	}
	remote := "/sdcard/adbq-screenrecord-" + strconv.FormatInt(time.Now().Unix(), 10) + ".mp4"
	ctx, cancel := context.WithCancel(parent)
	cmd, err := c.DeviceCommand(ctx, serial, "shell", "screenrecord", "--time-limit", strconv.Itoa(maxSec), remote)
	if err != nil {
		cancel()
		return nil, err
	}
	sess := &ScreenRecordSession{Serial: serial, RemotePath: remote, Cancel: cancel, Done: make(chan struct{})}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	countStreamSpawn(cmd.Args)
	go func() {
		_ = cmd.Wait()
		close(sess.Done)
	}()
	return sess, nil
}

// StopScreenRecord sends SIGINT to the device-side screenrecord so it writes
// out the MP4 header, then pulls the file to localDir.
func (c *Client) StopScreenRecord(ctx context.Context, sess *ScreenRecordSession, localDir string) (string, error) {
	if sess == nil {
		return "", fmt.Errorf("no active recording")
	}
	// Ask the device to terminate screenrecord cleanly.
	_, _ = c.Shell(ctx, sess.Serial, screenRecordStopRemote())
	// Give it a moment to finalize the container.
	select {
	case <-sess.Done:
	case <-time.After(3 * time.Second):
		sess.Cancel()
		<-sess.Done
	}
	if localDir == "" {
		home, _ := os.UserHomeDir()
		localDir = filepath.Join(home, "Movies", "adbq")
	}
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("adbq-%s-%s.mp4", sess.Serial, time.Now().Format("20060102-150405"))
	outPath := filepath.Join(localDir, name)
	if _, err := c.PullFile(ctx, sess.Serial, sess.RemotePath, outPath); err != nil {
		return "", err
	}
	_, _ = c.Shell(ctx, sess.Serial, "rm "+sess.RemotePath)
	return outPath, nil
}

// CaptureState describes the current tcpdump session on a device.
type CaptureState struct {
	Active     bool   `json:"active"`
	OurSession bool   `json:"ourSession"` // true if we started this run
	StartedAt  int64  `json:"startedAt"`
	PID        int    `json:"pid"`
	RemoteFile string `json:"remoteFile"`
	BPF        string `json:"bpf"`
	Iface      string `json:"iface"`
	SizeBytes  int64  `json:"sizeBytes"`
	PacketHint string `json:"packetHint"`
	Warning    string `json:"warning"`
}

// capturePath is the on-device pcap target for the legacy capture path. We use
// /data/local/tmp rather than /sdcard because under root su on some ROMs
// (e.g. SDK 21 emulator) /sdcard is mounted read-only for the tcpdump process,
// whereas /data/local/tmp is reliably writable.
const capturePath = "/data/local/tmp/adbq-capture.pcap"

// captureErrPath holds tcpdump's stderr for the file-capture path: its stdout is
// the pcap, and a diagnostic mixed into that would corrupt the file.
const captureErrPath = "/data/local/tmp/adbq-tcpdump.err"

// captureStartRemote is the backgrounded tcpdump the file-capture path runs.
// `nohup … &` is what lets the adb shell return while the capture keeps going.
func captureStartRemote(bin, iface, bpf string) string {
	args := bin + " -i " + shQuote(iface) + " -U -w " + capturePath
	if bpf != "" {
		args += " " + shQuote(bpf)
	}
	return "nohup " + args + " >/dev/null 2>" + captureErrPath + " </dev/null &"
}

// captureStopRemote signals the running tcpdump so it finalises the pcap header.
// A procfs scan rather than pkill, which stripped ROMs do not ship; `kill` is a
// shell builtin, so it is always there.
func captureStopRemote(signal string) string {
	return `for p in /proc/[0-9]*; do read c < "$p/comm" 2>/dev/null || continue; ` +
		`case "$c" in tcpdump) kill -` + signal + ` "${p##*/}" 2>/dev/null;; esac; done`
}

// CaptureCommands are the file-capture panel's actions: start, stop, and pull
// the pcap this computer will read.
type CaptureCommands struct {
	Start []string `json:"start"`
	Stop  []string `json:"stop"`
	Pull  []string `json:"pull"`
}

// CaptureCommandsFor renders them. tcpdumpPath empty means the device has no
// tcpdump yet, and there is no honest command to show for a capture.
func CaptureCommandsFor(serial, tcpdumpPath, iface, bpf string, render CommandRenderer) CaptureCommands {
	if iface == "" {
		iface = "any"
	}
	cc := CaptureCommands{
		Stop: []string{render(captureStopRemote("INT"), true)},
		Pull: []string{DeviceCommandText(serial, "pull", capturePath, "capture.pcap")},
	}
	if tcpdumpPath != "" {
		cc.Start = []string{render(captureStartRemote(tcpdumpPath, iface, strings.Trim(bpf, "'\"")), true)}
	}
	return cc
}

// CaptureCommandsFor is the device-aware entry point: it resolves tcpdump the
// same way a start would.
func (c *Client) CaptureCommandsFor(ctx context.Context, serial, iface, bpf string) CaptureCommands {
	bin, err := c.FindTcpdump(ctx, serial)
	if err != nil {
		bin = ""
	}
	return CaptureCommandsFor(serial, bin, iface, bpf, c.Renderer(ctx, serial))
}

// StartCapture launches tcpdump in the background. Returns final state.
func (c *Client) StartCapture(ctx context.Context, serial, iface, bpf string) (*CaptureState, error) {
	if iface == "" {
		iface = "any"
	}
	if bpf == "" {
		bpf = ""
	}
	bin, err := c.FindTcpdump(ctx, serial)
	if err != nil {
		return nil, err
	}
	// Kill any leftover tcpdump (exact comm match) and remove our stale pcap.
	// pkill is missing on stripped ROMs, so procfs is scanned instead — by the
	// same command Stop uses, in one round trip, rather than a scan followed by
	// a kill per pid.
	_, _, _ = c.ShellSU(ctx, serial, captureStopRemote("9")+"; rm -f "+capturePath)
	// Strip outer quotes if the user wrapped the filter themselves.
	bpf = strings.Trim(bpf, "'\"")
	if _, _, err := c.ShellSU(ctx, serial, captureStartRemote(bin, iface, bpf)); err != nil {
		return nil, err
	}
	// Give tcpdump a moment then probe.
	time.Sleep(400 * time.Millisecond)
	return c.CaptureStatus(ctx, serial, iface, bpf)
}

// StopCapture sends SIGINT so tcpdump finalizes the pcap header, then returns state.
func (c *Client) StopCapture(ctx context.Context, serial string) (*CaptureState, error) {
	// pkill is missing on stripped ROMs; scan procfs and kill by PID. `kill`
	// is a shell builtin so it's always available.
	_, _, _ = c.ShellSU(ctx, serial, captureStopRemote("INT"))
	time.Sleep(500 * time.Millisecond)
	// If SIGINT didn't take, fall back to SIGKILL.
	_, _, _ = c.ShellSU(ctx, serial, captureStopRemote("9"))
	return c.CaptureStatus(ctx, serial, "", "")
}

// captureStatusRemote finds the tcpdump writing our pcap and sizes the file, in
// one command.
//
// This runs on a poll while the Network panel is open, and it is the most
// expensive thing adbq asks a device to do: the loop opens /proc/<pid>/comm for
// every process on the system — nine hundred to fifteen hundred file opens on a
// real phone. It used to run twice a second, and the pid scan, each candidate's
// cmdline and the `ls -l` were three separate round trips on top of that.
//
// Emitting the cmdline inline (rather than fetching it per candidate
// afterwards) is what collapses it to one: `tcpdump` processes are rare enough
// that printing all of their cmdlines costs nothing, and it removes a round
// trip per candidate.
func captureStatusRemote() string {
	return `for p in /proc/[0-9]*; do read c < "$p/comm" 2>/dev/null || continue; ` +
		`case "$c" in tcpdump) echo "PID ${p##*/}"; cat "$p/cmdline" 2>/dev/null; echo;; esac; done` +
		`; echo '` + procNetSentinel + `'; ls -l ` + capturePath + ` 2>/dev/null`
}

// CaptureStatus probes for the running tcpdump and the file size of the pcap.
// Uses an exact comm match (procfs scan, since pgrep/pkill are missing on
// stripped ROMs) so the shell wrapper running the probe itself isn't falsely
// identified as the capture process.
func (c *Client) CaptureStatus(ctx context.Context, serial, iface, bpf string) (*CaptureState, error) {
	st := &CaptureState{Iface: iface, BPF: bpf, RemoteFile: capturePath}
	out, _, err := c.ShellSU(ctx, serial, captureStatusRemote())
	if err != nil && strings.TrimSpace(out) == "" {
		return st, nil
	}
	applyCaptureStatus(st, out)
	// StartedAt: deriving an epoch from /proc/<pid>/stat field 22 requires
	// btime + jiffies math that's fragile on this ROM (no getconf, no stat).
	// We deliberately leave it at 0 rather than add unreliable dependencies.
	if st.Active && st.SizeBytes >= 24 {
		st.PacketHint = "≈" + itoa((st.SizeBytes-24)/80) + " packets"
	}
	return st, nil
}

// applyCaptureStatus parses one captureStatusRemote result: which tcpdump (if
// any) is writing our pcap, and how big that file is.
//
// Pure, so both halves are testable without a device. The half that matters is
// the ownership test — a false positive reports a capture the user did not
// start, and the panel then offers to stop a process belonging to something
// else.
func applyCaptureStatus(st *CaptureState, out string) {
	procs, listing, _ := strings.Cut(out, procNetSentinel)
	st.PID, st.Active = findOurTcpdump(procs)
	st.SizeBytes = parseCaptureSize(listing)
}

// findOurTcpdump walks "PID <n>" records, each followed by that process's
// NUL-separated cmdline, and returns the one whose command line names our pcap.
func findOurTcpdump(procs string) (pid int, active bool) {
	candidate := 0
	for _, raw := range strings.Split(procs, "\n") {
		ln := strings.TrimRight(raw, "\r")
		if rest, ok := strings.CutPrefix(strings.TrimSpace(ln), "PID "); ok {
			n, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				candidate = 0
				continue
			}
			candidate = n
			continue
		}
		if candidate == 0 || strings.TrimSpace(ln) == "" {
			continue
		}
		// adb turns the NULs into nothing useful, so match on the path alone.
		if strings.Contains(strings.ReplaceAll(ln, "\x00", " "), capturePath) {
			return candidate, true
		}
	}
	return 0, false
}

// parseCaptureSize reads the pcap's size out of `ls -l` output. `stat` is
// absent on the stripped ROMs this app supports, so the package's existing
// listing parser does the work.
func parseCaptureSize(listing string) int64 {
	for _, ln := range strings.Split(listing, "\n") {
		ln = strings.TrimRight(ln, "\r")
		if ln == "" || strings.HasPrefix(ln, "total ") {
			continue
		}
		if e, ok := parseLsLine(ln); ok {
			return e.Size
		}
	}
	return 0
}

func shQuote(s string) string {
	// wrap in single quotes; embedded single quotes broken with the standard `'\''`
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ExportAppData tars and gzips /data/data/<pkg> into a host file (root required).
func (c *Client) ExportAppData(ctx context.Context, serial, pkg, localPath string) (string, error) {
	remote := appDataArchive(pkg)
	// Run as root, write to /sdcard (world-readable), then pull.
	_, _, err := c.ShellSU(ctx, serial, appDataTarRemote(pkg))
	if err != nil {
		return "", err
	}
	if _, err := c.PullFile(ctx, serial, remote, localPath); err != nil {
		return "", err
	}
	_, _, _ = c.ShellSU(ctx, serial, "rm "+remote)
	return localPath, nil
}

// ScreenRecord captures `seconds` of screen video to /sdcard/<basename> on the
// device, then pulls it to outDir. Synchronous; use StartScreenRecord+
// StopScreenRecord for interactive control.
func (c *Client) ScreenRecord(ctx context.Context, serial, outDir string, seconds int) (string, error) {
	if outDir == "" {
		home, _ := os.UserHomeDir()
		outDir = filepath.Join(home, "Movies", "adbq")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	if seconds <= 0 || seconds > 180 {
		seconds = 30
	}
	deviceTmp := screenRecordRemote()
	if _, err := c.Shell(ctx, serial, fmt.Sprintf("screenrecord --time-limit %d %s", seconds, deviceTmp)); err != nil {
		return "", err
	}
	name := fmt.Sprintf("adbq-%s-%s.mp4", serial, time.Now().Format("20060102-150405"))
	outPath := filepath.Join(outDir, name)
	if _, err := c.PullFile(ctx, serial, deviceTmp, outPath); err != nil {
		return "", err
	}
	_, _ = c.Shell(ctx, serial, "rm "+deviceTmp)
	return outPath, nil
}

// ClipboardSet uses `cmd clipboard set-text` (Android 10+). Returns an error
// if not supported.
func (c *Client) ClipboardSet(ctx context.Context, serial, text string) (string, error) {
	return c.Shell(ctx, serial, clipboardSetRemote(text))
}
