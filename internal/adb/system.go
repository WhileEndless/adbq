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

// ListConnections reads /proc/net/tcp(6) and /proc/net/udp(6) from the device.
func (c *Client) ListConnections(ctx context.Context, serial string) ([]Connection, error) {
	var conns []Connection
	for _, src := range []struct{ proto, path string }{
		{"tcp", "/proc/net/tcp"}, {"tcp6", "/proc/net/tcp6"},
		{"udp", "/proc/net/udp"}, {"udp6", "/proc/net/udp6"},
	} {
		out, err := c.Shell(ctx, serial, "cat "+src.path+" 2>/dev/null")
		if err != nil {
			continue
		}
		conns = append(conns, parseProcNet(out, src.proto)...)
	}
	return conns, nil
}

func parseProcNet(out, proto string) []Connection {
	res := []Connection{}
	for i, ln := range strings.Split(out, "\n") {
		if i == 0 {
			continue
		}
		fs := strings.Fields(ln)
		if len(fs) < 10 {
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
	_, _ = c.Shell(ctx, sess.Serial, "killall -INT screenrecord || pkill -INT screenrecord")
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
	// pkill is missing on stripped ROMs, so we scan procfs for the PIDs.
	for _, pid := range c.tcpdumpPIDs(ctx, serial) {
		_, _, _ = c.ShellSU(ctx, serial, "kill -9 "+itoa(int64(pid)))
	}
	_, _, _ = c.ShellSU(ctx, serial, "rm -f "+capturePath)
	args := bin + " -i " + shQuote(iface) + " -U -w " + capturePath
	if bpf != "" {
		// Strip outer single quotes if user wrapped it
		bpf = strings.Trim(bpf, "'\"")
		args += " " + shQuote(bpf)
	}
	cmd := "nohup " + args + " >/dev/null 2>/data/local/tmp/adbq-tcpdump.err </dev/null &"
	if _, _, err := c.ShellSU(ctx, serial, cmd); err != nil {
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
	for _, pid := range c.tcpdumpPIDs(ctx, serial) {
		_, _, _ = c.ShellSU(ctx, serial, "kill -INT "+itoa(int64(pid)))
	}
	time.Sleep(500 * time.Millisecond)
	// If SIGINT didn't take, fall back to SIGKILL.
	for _, pid := range c.tcpdumpPIDs(ctx, serial) {
		_, _, _ = c.ShellSU(ctx, serial, "kill -9 "+itoa(int64(pid)))
	}
	return c.CaptureStatus(ctx, serial, "", "")
}

// tcpdumpPIDs scans /proc for processes whose comm is exactly "tcpdump" and
// returns their PIDs. Runs via root because /proc/<pid>/comm of a root-owned
// tcpdump isn't readable otherwise. Used in place of `pgrep -x`/`pkill`, both
// missing on stripped ROMs.
func (c *Client) tcpdumpPIDs(ctx context.Context, serial string) []int {
	const scan = `for p in /proc/[0-9]*; do read c < "$p/comm" 2>/dev/null || continue; case "$c" in tcpdump) echo "${p##*/}";; esac; done`
	out, _, err := c.ShellSU(ctx, serial, scan)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil
	}
	var pids []int
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(strings.TrimRight(ln, "\r"))
		if ln == "" {
			continue
		}
		if n, err := strconv.Atoi(ln); err == nil && n > 0 {
			pids = append(pids, n)
		}
	}
	return pids
}

// CaptureStatus probes for the running tcpdump and the file size of the pcap.
// Uses an exact comm match (procfs scan, since pgrep/pkill are missing on
// stripped ROMs) so the shell wrapper running the probe itself isn't falsely
// identified as the capture process.
func (c *Client) CaptureStatus(ctx context.Context, serial, iface, bpf string) (*CaptureState, error) {
	st := &CaptureState{Iface: iface, BPF: bpf, RemoteFile: capturePath}

	// 1. Find candidate processes whose comm is exactly "tcpdump", then confirm
	//    the one writing OUR pcap by inspecting its (null-separated) cmdline.
	for _, n := range c.tcpdumpPIDs(ctx, serial) {
		cmdline, _, err := c.ShellSU(ctx, serial, "cat /proc/"+itoa(int64(n))+"/cmdline 2>/dev/null")
		if err != nil && cmdline == "" {
			continue
		}
		cmdline = strings.ReplaceAll(cmdline, "\x00", " ")
		if strings.Contains(cmdline, capturePath) {
			st.PID = n
			st.Active = true
			break
		}
	}
	// StartedAt: deriving an epoch from /proc/<pid>/stat field 22 requires
	// btime + jiffies math that's fragile on this ROM (no getconf, no stat).
	// We deliberately leave it at 0 rather than add unreliable dependencies.

	// File size: stat is missing, so parse `ls -l` with the package's existing
	// parser (it already handles this ROM's layout).
	if out, err := c.Shell(ctx, serial, "ls -l "+capturePath+" 2>/dev/null"); err == nil {
		for _, ln := range strings.Split(out, "\n") {
			ln = strings.TrimRight(ln, "\r")
			if ln == "" || strings.HasPrefix(ln, "total ") {
				continue
			}
			if e, ok := parseLsLine(ln); ok {
				st.SizeBytes = e.Size
				break
			}
		}
	}
	if st.Active && st.SizeBytes >= 24 {
		st.PacketHint = "≈" + itoa((st.SizeBytes-24)/80) + " packets"
	}
	return st, nil
}

func shQuote(s string) string {
	// wrap in single quotes; embedded single quotes broken with the standard `'\''`
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ExportAppData tars and gzips /data/data/<pkg> into a host file (root required).
func (c *Client) ExportAppData(ctx context.Context, serial, pkg, localPath string) (string, error) {
	remote := "/sdcard/adbq-appdata-" + pkg + ".tar.gz"
	// Run as root, write to /sdcard (world-readable), then pull.
	cmd := fmt.Sprintf("tar -czf %s -C /data/data %s 2>&1 || tar -czf %s /data/data/%s 2>&1", remote, pkg, remote, pkg)
	_, _, err := c.ShellSU(ctx, serial, cmd)
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
	deviceTmp := "/sdcard/adbq-screenrecord.mp4"
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
	// escape single quotes
	esc := strings.ReplaceAll(text, "'", `'\''`)
	return c.Shell(ctx, serial, "cmd clipboard set-text '"+esc+"' || input text '"+esc+"'")
}
