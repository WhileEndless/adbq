package adb

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// LivePacket is the per-packet summary handed to the UI list. The full per-
// layer breakdown lives in LivePacketDetail and is rebuilt on demand by
// DescribeLivePacket so the live stream stays tiny.
type LivePacket struct {
	No      uint64    `json:"no"`
	Ts      time.Time `json:"ts"`
	Length  int       `json:"length"`
	SrcIP   string    `json:"srcIP"`
	DstIP   string    `json:"dstIP"`
	SrcPort uint16    `json:"srcPort"`
	DstPort uint16    `json:"dstPort"`
	Proto   string    `json:"proto"`
	Info    string    `json:"info"`
	Layers  []string  `json:"layers"` // short label list, e.g. ["SLL2","IPv4","TCP","TLS"]
}

// LivePacketField is one key/value inside a layer (e.g. "TTL"=64).
type LivePacketField struct {
	K string `json:"k"`
	V string `json:"v"`
}

// LivePacketLayer is the expanded form for the detail pane.
type LivePacketLayer struct {
	Name   string            `json:"name"`
	Bytes  int               `json:"bytes"`  // length in payload bytes
	Offset int               `json:"offset"` // start offset within the raw frame, for hex highlight
	Fields []LivePacketField `json:"fields"`
}

// LivePacketDetail is the full description for the detail panel.
type LivePacketDetail struct {
	LivePacket
	LayersFull []LivePacketLayer `json:"layersFull"`
	RawHex     string            `json:"rawHex"`
}

// LiveCaptureState mirrors the long-lived stream to the UI.
type LiveCaptureState struct {
	Active        bool   `json:"active"`
	Iface         string `json:"iface"`
	BPF           string `json:"bpf"`
	StartedAt     int64  `json:"startedAt"`
	Packets       uint64 `json:"packets"`       // total received since start
	Bytes         uint64 `json:"bytes"`         // total bytes captured since start
	PcapPath      string `json:"pcapPath"`      // host-side mirror, used by SaveLivePcap
	PcapBytes     int64  `json:"pcapBytes"`     // current size of the mirror on disk
	PcapRotations int    `json:"pcapRotations"` // how many times the mirror has wrapped
	LinkType      uint32 `json:"linkType"`      // real DLT value (1=Ethernet, 113=SLL, 276=SLL2)
	MaxPackets    int    `json:"maxPackets"`    // in-memory ring cap
	MaxPcapBytes  int64  `json:"maxPcapBytes"`  // mirror file rotation cap
	Error         string `json:"error"`         // set when the stream died before producing a pcap (e.g. su/SELinux denial)
}

// Defaults — also enforced as floors/ceilings on user-supplied values.
const (
	liveRingDefault   = 10000
	liveRingMin       = 1000
	liveRingMax       = 200000
	liveMirrorDefault = 100 * 1024 * 1024      // 100 MB
	liveMirrorMin     = 4 * 1024 * 1024        // 4 MB — pcap header + a couple of records minimum
	liveMirrorMax     = 4 * 1024 * 1024 * 1024 // 4 GB ceiling, sanity only
)

type liveSession struct {
	mu       sync.Mutex
	state    LiveCaptureState
	cmd      *exec.Cmd
	ring     []*LivePacket
	pcapFile *os.File
	// pcapHeader is the first 24 bytes of the live stream — kept verbatim so
	// we can prepend it after a rotation and the mirror file stays a valid
	// pcap. Captured before we even hand bytes to the decoder.
	pcapHeader []byte
	// stderr collects the adb client's stderr (bounded). On exec-out the device
	// stderr is folded into stdout, so this mostly catches adb/su-level errors.
	stderr *capBuffer
	// readErr reads the on-device file tcpdump's stderr was redirected to. That
	// redirect is mandatory: `adb exec-out` merges device stderr into the binary
	// stdout, so tcpdump's "listening on …" banner would corrupt the pcap stream
	// if left on the shell. We surface this file's contents when a capture dies
	// before producing data.
	readErr   func() string
	pcapBytes int64
	counter   uint64
	rawIdx    map[uint64][]byte
	linkType  uint32
	emit      func(batch []*LivePacket)
	stopOnce  sync.Once
	done      chan struct{}
}

// LiveCapture is the per-Client registry of running streams. Keyed by serial.
type LiveCapture struct {
	mu       sync.Mutex
	sessions map[string]*liveSession
}

func newLiveCapture() *LiveCapture { return &LiveCapture{sessions: map[string]*liveSession{}} }

func (l *LiveCapture) Status(serial string) *LiveCaptureState {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.sessions[serial]
	if !ok {
		return &LiveCaptureState{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state
	return &st
}

// Stop ends the tcpdump child but keeps the session record around so the UI
// can still query Status (and SaveLivePcap can still pull the mirror file)
// after a stop. A subsequent Start for the same serial replaces the entry.
func (l *LiveCapture) Stop(serial string) error {
	l.mu.Lock()
	s, ok := l.sessions[serial]
	l.mu.Unlock()
	if !ok {
		return nil
	}
	s.stopOnce.Do(func() {
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
	})
	<-s.done
	// Don't close pcapFile — SaveLivePcap re-opens it. We only ensure the
	// pending writes are flushed by syncing.
	if s.pcapFile != nil {
		_ = s.pcapFile.Sync()
	}
	s.mu.Lock()
	s.state.Active = false
	s.mu.Unlock()
	return nil
}

// LiveCaptureOptions tunes the per-session resource limits. Zero values fall
// back to the package-level defaults so callers can omit fields they don't
// care about.
type LiveCaptureOptions struct {
	MaxPackets   int   `json:"maxPackets"`   // in-memory ring cap
	MaxPcapBytes int64 `json:"maxPcapBytes"` // mirror file size cap (rotates on overflow)
}

func (o LiveCaptureOptions) normalize() (int, int64) {
	mp := o.MaxPackets
	if mp <= 0 {
		mp = liveRingDefault
	}
	if mp < liveRingMin {
		mp = liveRingMin
	}
	if mp > liveRingMax {
		mp = liveRingMax
	}
	mb := o.MaxPcapBytes
	if mb <= 0 {
		mb = liveMirrorDefault
	}
	if mb < liveMirrorMin {
		mb = liveMirrorMin
	}
	if mb > liveMirrorMax {
		mb = liveMirrorMax
	}
	return mp, mb
}

// StartLiveCapture spawns a tcpdump on the device, mirrors the raw pcap stream
// to a host-side file (so SaveLivePcap is byte-perfect), and decodes packets
// for the UI. The link type is whatever the device picked (Ethernet for
// physical ifaces, SLL/SLL2 for `-i any`); we read it from the file header
// ourselves because gopacket truncates the 32-bit DLT to uint8.
//
// opts caps resource use so a forgotten browser tab doesn't grow without
// bound — in-memory packet ring is reused, the on-disk mirror file rotates
// (truncates and replays the header) when it crosses MaxPcapBytes.
func (c *Client) StartLiveCapture(ctx context.Context, serial, iface, bpf string, opts LiveCaptureOptions, emit func(batch []*LivePacket)) (*LiveCaptureState, error) {
	if c.Live == nil {
		c.Live = newLiveCapture()
	}
	c.Live.mu.Lock()
	if prev, exists := c.Live.sessions[serial]; exists {
		if prev.state.Active {
			c.Live.mu.Unlock()
			return nil, fmt.Errorf("a live capture is already running on %s", serial)
		}
		// Inactive leftover from a prior Stop — release its file handle so the
		// new run can recreate the mirror at the same path.
		if prev.pcapFile != nil {
			_ = prev.pcapFile.Close()
		}
		delete(c.Live.sessions, serial)
	}
	c.Live.mu.Unlock()

	td, err := c.FindTcpdump(ctx, serial)
	if err != nil {
		return nil, err
	}
	if iface == "" {
		iface = "any"
	}
	// Some stripped OEM ROMs (LineageOS variants, certain x86 emulator system
	// images) don't expose `any`. Probe the device's iface list once and fall
	// back to a real iface so the user doesn't see a silent "0 packets" run.
	resolved, err := c.resolveCaptureIface(ctx, serial, td, iface)
	if err != nil {
		return nil, err
	}
	iface = resolved
	// Redirect tcpdump's stderr to a device file: exec-out folds device stderr
	// into the binary stdout, so the "listening on …" banner would corrupt the
	// pcap. We read this file back only if the capture fails to start.
	errFile := "/data/local/tmp/adbq-pcap-" + sanitizeSerial(serial) + ".err"
	inner := td + " -i " + shellQuote(iface) + " -U -s 0 -w -"
	if bpf = strings.TrimSpace(bpf); bpf != "" {
		inner += " " + shellQuote(bpf)
	}
	inner += " 2>" + errFile
	// Wrap as root using whichever `su -c` form this device accepts, and pass
	// the whole thing as ONE adb-shell arg so it isn't re-split (see rootWrap).
	remote, err := c.rootWrap(ctx, serial, inner)
	if err != nil {
		return nil, err
	}
	bin, err := c.Binary()
	if err != nil {
		return nil, err
	}
	// Use `exec-out`, NOT `shell`: the pcap stream is binary and on older
	// devices (pre-shell_v2, e.g. API 21) `adb shell` runs through a PTY that
	// translates LF→CRLF, corrupting the stream so the decoder reads garbage
	// record lengths and stalls after the first packet. exec-out is raw.
	cmd := exec.CommandContext(ctx, bin, "-s", serial, "exec-out", remote)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &capBuffer{max: 8192}
	cmd.Stderr = stderr

	pcapPath, err := liveSessionPcapPath(serial)
	if err != nil {
		return nil, err
	}
	pf, err := os.Create(pcapPath)
	if err != nil {
		return nil, fmt.Errorf("open pcap mirror: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = pf.Close()
		return nil, fmt.Errorf("tcpdump spawn: %w", err)
	}

	maxPkts, maxPcap := opts.normalize()
	s := &liveSession{
		state: LiveCaptureState{
			Active: true, Iface: iface, BPF: bpf,
			StartedAt: time.Now().Unix(), PcapPath: pcapPath,
			MaxPackets: maxPkts, MaxPcapBytes: maxPcap,
		},
		cmd:      cmd,
		pcapFile: pf,
		stderr:   stderr,
		readErr: func() string {
			out, _, _ := c.ShellSU(ctx, serial, "cat "+errFile+" 2>/dev/null")
			return strings.TrimSpace(out)
		},
		rawIdx: map[uint64][]byte{},
		emit:   emit,
		done:   make(chan struct{}),
	}

	c.Live.mu.Lock()
	c.Live.sessions[serial] = s
	c.Live.mu.Unlock()

	go s.pump(stdout)

	st := s.state
	return &st, nil
}

// resolveCaptureIface picks an interface that actually exists on the device.
// Calls `tcpdump -D` once and matches the requested name; if `any` was asked
// for but the device doesn't list it (some stripped ROMs), we fall back to
// the first wireless/cellular iface that's available.
func (c *Client) resolveCaptureIface(ctx context.Context, serial, tcpdumpPath, requested string) (string, error) {
	remote, werr := c.rootWrap(ctx, serial, tcpdumpPath+" -D 2>&1")
	if werr != nil {
		// Root not available for the probe — let the real tcpdump start surface
		// the precise failure rather than second-guessing the iface here.
		return requested, nil
	}
	out, err := c.Shell(ctx, serial, remote)
	if err != nil || out == "" {
		// Probe failed — caller will see the real failure when tcpdump
		// actually starts. Don't second-guess here.
		return requested, nil
	}
	avail := make([]string, 0, 8)
	for _, line := range strings.Split(out, "\n") {
		// "1.wlan0 [Up, Running, Wireless]"
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, ".") {
			continue
		}
		dot := strings.Index(line, ".")
		rest := line[dot+1:]
		name := rest
		if sp := strings.IndexAny(rest, " \t["); sp > 0 {
			name = rest[:sp]
		}
		if name != "" {
			avail = append(avail, name)
		}
	}
	if len(avail) == 0 {
		return requested, nil
	}
	if contains(avail, requested) {
		return requested, nil
	}
	if requested == "any" {
		// Prefer common Android upstreams in order; otherwise first available.
		for _, pref := range []string{"wlan0", "rmnet_data0", "rmnet0", "eth0", "lo"} {
			if contains(avail, pref) {
				return pref, nil
			}
		}
		return avail[0], nil
	}
	return "", fmt.Errorf("interface %q not available on device — try one of: %s", requested, strings.Join(avail, ", "))
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func liveSessionPcapPath(serial string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "adbq", "captures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, sanitizeSerial(serial)+".pcap"), nil
}

// sanitizeSerial makes a serial safe for use in a filename (TCP serials contain
// ':', some contain '/').
func sanitizeSerial(serial string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == ' ' {
			return '_'
		}
		return r
	}, serial)
}

// pcap file header: 24 bytes
//
//	magic(4) version_major(2) version_minor(2) thiszone(4) sigfigs(4) snaplen(4) network(4)
//
// We need the network field as a real uint32 (gopacket's layers.LinkType is
// uint8 and silently truncates 276 → 20). Returns the byte order needed to
// parse subsequent per-packet records.
func readPcapHeader(r io.Reader) (linkType uint32, order binary.ByteOrder, err error) {
	var hdr [24]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	magic := binary.LittleEndian.Uint32(hdr[0:4])
	switch magic {
	case 0xa1b2c3d4, 0xa1b23c4d:
		order = binary.LittleEndian
	case 0xd4c3b2a1, 0x4d3cb2a1:
		order = binary.BigEndian
	default:
		return 0, nil, fmt.Errorf("not a pcap stream (magic %08x)", magic)
	}
	linkType = order.Uint32(hdr[20:24])
	return
}

// readPcapRecord pulls one packet record from a libpcap stream.
func readPcapRecord(r io.Reader, order binary.ByteOrder) (ts time.Time, data []byte, err error) {
	var rh [16]byte
	if _, err = io.ReadFull(r, rh[:]); err != nil {
		return time.Time{}, nil, err
	}
	sec := order.Uint32(rh[0:4])
	usec := order.Uint32(rh[4:8])
	caplen := order.Uint32(rh[8:12])
	// origlen at 12:16 — informational, ignore.
	if caplen > 1<<24 {
		return time.Time{}, nil, fmt.Errorf("oversized record %d", caplen)
	}
	data = make([]byte, caplen)
	if _, err = io.ReadFull(r, data); err != nil {
		return time.Time{}, nil, err
	}
	ts = time.Unix(int64(sec), int64(usec)*1000)
	return
}

func (s *liveSession) pump(stdout io.ReadCloser) {
	defer close(s.done)
	defer stdout.Close()

	// Capture the 24-byte pcap header up-front. We keep it verbatim so a
	// rotation can rewrite it at the start of the freshly-truncated mirror
	// file — without it the on-disk pcap would be invalid after the first
	// wrap.
	hdr := make([]byte, 24)
	if _, err := io.ReadFull(stdout, hdr); err != nil {
		// No pcap stream arrived. Drain the child so its stderr flushes, then
		// surface the real cause (su denial, missing/incompatible tcpdump,
		// SELinux, bad iface) instead of a silent inactive session.
		_ = s.cmd.Wait()
		s.failWith(err)
		return
	}
	if _, err := s.pcapFile.Write(hdr); err != nil {
		s.markInactive()
		return
	}
	linkType, order, err := readPcapHeader(bytes.NewReader(hdr))
	if err != nil {
		s.markInactive()
		return
	}
	s.mu.Lock()
	s.linkType = linkType
	s.state.LinkType = linkType
	s.pcapHeader = hdr
	s.pcapBytes = int64(len(hdr))
	s.state.PcapBytes = s.pcapBytes
	s.mu.Unlock()

	tee := io.TeeReader(stdout, &mirrorWriter{s: s})

	batch := make([]*LivePacket, 0, 64)
	flushTimer := time.NewTicker(200 * time.Millisecond)
	defer flushTimer.Stop()
	flush := func() {
		if len(batch) == 0 {
			return
		}
		out := make([]*LivePacket, len(batch))
		copy(out, batch)
		batch = batch[:0]
		if s.emit != nil {
			s.emit(out)
		}
	}
	go func() {
		for range flushTimer.C {
			s.mu.Lock()
			flush()
			s.mu.Unlock()
		}
	}()

	for {
		ts, data, err := readPcapRecord(tee, order)
		if err != nil {
			break
		}
		s.counter++
		lp := decodeLivePacket(data, ts, s.counter, linkType)
		s.mu.Lock()
		s.rawIdx[s.counter] = data
		ringCap := s.state.MaxPackets
		if ringCap <= 0 {
			ringCap = liveRingDefault
		}
		s.ring = append(s.ring, lp)
		if len(s.ring) > ringCap {
			s.ring = s.ring[len(s.ring)-ringCap:]
		}
		// Mirror the ring policy for rawIdx so detail lookups for evicted
		// rows return nil instead of leaking memory.
		if uint64(len(s.rawIdx)) > uint64(ringCap) {
			cutoff := s.counter - uint64(ringCap)
			for k := range s.rawIdx {
				if k < cutoff {
					delete(s.rawIdx, k)
				}
			}
		}
		s.state.Packets = s.counter
		s.state.Bytes += uint64(len(data))
		batch = append(batch, lp)
		if len(batch) >= 64 {
			flush()
		}
		s.mu.Unlock()
	}

	s.mu.Lock()
	flush()
	s.mu.Unlock()
	s.markInactive()
}

// mirrorWriter teeing wraps the host-side pcap file with a soft size cap.
// When MaxPcapBytes is crossed the file is truncated and the original 24-byte
// pcap header is replayed at the top, so it stays a valid pcap (just losing
// the older records — the in-memory ring is what serves live UI anyway).
// SaveLivePcap dumps this file as-is, so what you save is whatever currently
// fits within the cap.
type mirrorWriter struct{ s *liveSession }

func (m *mirrorWriter) Write(p []byte) (int, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	cap := m.s.state.MaxPcapBytes
	if cap > 0 && m.s.pcapBytes+int64(len(p)) > cap {
		if err := m.s.rotateMirrorLocked(); err != nil {
			return 0, err
		}
	}
	n, err := m.s.pcapFile.Write(p)
	m.s.pcapBytes += int64(n)
	m.s.state.PcapBytes = m.s.pcapBytes
	return n, err
}

// rotateMirrorLocked is called with s.mu held. Truncates the file, replays the
// pcap header so downstream tools (Wireshark, our SaveLivePcap copy) still
// see a valid stream, and bumps the rotation counter for the UI.
func (s *liveSession) rotateMirrorLocked() error {
	if s.pcapFile == nil {
		return nil
	}
	if err := s.pcapFile.Truncate(0); err != nil {
		return err
	}
	if _, err := s.pcapFile.Seek(0, 0); err != nil {
		return err
	}
	if len(s.pcapHeader) > 0 {
		if _, err := s.pcapFile.Write(s.pcapHeader); err != nil {
			return err
		}
		s.pcapBytes = int64(len(s.pcapHeader))
	} else {
		s.pcapBytes = 0
	}
	s.state.PcapBytes = s.pcapBytes
	s.state.PcapRotations++
	return nil
}

func (s *liveSession) markInactive() {
	s.mu.Lock()
	s.state.Active = false
	s.mu.Unlock()
}

// failWith records why the stream never produced a pcap so the UI can show the
// real error. readErr is the (usually opaque) EOF from the empty stdout; the
// child's stderr, when present, carries the actionable message.
func (s *liveSession) failWith(readErr error) {
	msg := ""
	if s.readErr != nil {
		msg = s.readErr() // tcpdump's own stderr, redirected to a device file
	}
	if msg == "" && s.stderr != nil {
		msg = strings.TrimSpace(s.stderr.String())
	}
	s.mu.Lock()
	if msg != "" {
		s.state.Error = "capture failed: " + msg
	} else {
		s.state.Error = "capture produced no data (root denied or tcpdump unavailable): " + readErr.Error()
	}
	s.state.Active = false
	s.mu.Unlock()
}

// capBuffer is a bounded, concurrency-safe io.Writer. tcpdump's diagnostics are
// only a few lines; we cap to keep a runaway stderr from growing without bound.
type capBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (b *capBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.max <= 0 {
		b.max = 8192
	}
	if room := b.max - len(b.buf); room > 0 {
		if room > len(p) {
			room = len(p)
		}
		b.buf = append(b.buf, p[:room]...)
	}
	return len(p), nil
}

func (b *capBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// ─── Decode helpers ──────────────────────────────────────────────────────

// stripLinkLayer peels the link-layer header off and returns the network-
// layer payload plus a label and the consumed length. Handles Ethernet,
// SLL (Linux cooked v1) and SLL2 (v2). For SLL/SLL2 we can't rely on
// gopacket (it doesn't know SLL2 and truncates the link-type enum).
//
// Returns: (l3type, payload, l2name, l2offset, l2bytes, ok)
//
//	l3type     - gopacket layer to decode the payload with (IPv4/IPv6/ARP)
//	payload    - bytes after the L2 header
//	l2name     - human label for the L2 layer
//	l2bytes    - L2 header length (for hex highlight)
func stripLinkLayer(data []byte, linkType uint32) (gopacket.LayerType, []byte, string, int) {
	switch linkType {
	case 1: // Ethernet
		if len(data) < 14 {
			return gopacket.LayerTypeZero, nil, "Ethernet", 0
		}
		et := binary.BigEndian.Uint16(data[12:14])
		return ethertypeToLayer(et), data[14:], "Ethernet", 14
	case 113: // SLL (Linux cooked v1)
		if len(data) < 16 {
			return gopacket.LayerTypeZero, nil, "Linux SLL", 0
		}
		et := binary.BigEndian.Uint16(data[14:16])
		return ethertypeToLayer(et), data[16:], "Linux SLL", 16
	case 276: // SLL2 (Linux cooked v2)
		if len(data) < 20 {
			return gopacket.LayerTypeZero, nil, "Linux SLL2", 0
		}
		// SLL2 header: protocol_type(2) reserved(2) iface_idx(4) arphrd(2) pkt_type(1) ll_len(1) ll_addr(8)
		et := binary.BigEndian.Uint16(data[0:2])
		return ethertypeToLayer(et), data[20:], "Linux SLL2", 20
	case 101: // raw IP
		if len(data) == 0 {
			return gopacket.LayerTypeZero, nil, "Raw", 0
		}
		ver := data[0] >> 4
		if ver == 4 {
			return layers.LayerTypeIPv4, data, "Raw IPv4", 0
		}
		if ver == 6 {
			return layers.LayerTypeIPv6, data, "Raw IPv6", 0
		}
	}
	return gopacket.LayerTypeZero, nil, fmt.Sprintf("DLT %d", linkType), 0
}

func ethertypeToLayer(et uint16) gopacket.LayerType {
	switch et {
	case 0x0800:
		return layers.LayerTypeIPv4
	case 0x86dd:
		return layers.LayerTypeIPv6
	case 0x0806:
		return layers.LayerTypeARP
	case 0x8100:
		return layers.LayerTypeDot1Q
	default:
		return gopacket.LayerTypeZero
	}
}

// decodeLivePacket produces the row-level summary handed to the UI list.
func decodeLivePacket(data []byte, ts time.Time, no uint64, linkType uint32) *LivePacket {
	lp := &LivePacket{No: no, Ts: ts, Length: len(data), Proto: "?"}
	l3type, payload, l2name, l2bytes := stripLinkLayer(data, linkType)
	lp.Layers = append(lp.Layers, l2name)

	if l3type == gopacket.LayerTypeZero || len(payload) == 0 {
		lp.Info = fmt.Sprintf("Unparsed L3 (linktype=%d, %dB after %dB L2)", linkType, len(payload), l2bytes)
		return lp
	}
	pkt := gopacket.NewPacket(payload, l3type, gopacket.Lazy)
	for _, l := range pkt.Layers() {
		lp.Layers = append(lp.Layers, layerShortName(l))
	}
	if nl := pkt.NetworkLayer(); nl != nil {
		src, dst := nl.NetworkFlow().Endpoints()
		lp.SrcIP = src.String()
		lp.DstIP = dst.String()
	}
	if tl := pkt.TransportLayer(); tl != nil {
		switch t := tl.(type) {
		case *layers.TCP:
			lp.SrcPort, lp.DstPort = uint16(t.SrcPort), uint16(t.DstPort)
			lp.Proto = "TCP"
			lp.Info = tcpInfo(t)
		case *layers.UDP:
			lp.SrcPort, lp.DstPort = uint16(t.SrcPort), uint16(t.DstPort)
			lp.Proto = "UDP"
			lp.Info = "UDP " + strconv.Itoa(int(t.SrcPort)) + "→" + strconv.Itoa(int(t.DstPort)) + " len=" + strconv.Itoa(int(t.Length))
		}
	} else if il := pkt.Layer(layers.LayerTypeICMPv4); il != nil {
		lp.Proto = "ICMP"
		if ic, ok := il.(*layers.ICMPv4); ok {
			lp.Info = ic.TypeCode.String()
		}
	} else if il := pkt.Layer(layers.LayerTypeICMPv6); il != nil {
		lp.Proto = "ICMPv6"
		if ic, ok := il.(*layers.ICMPv6); ok {
			lp.Info = ic.TypeCode.String()
		}
	} else if al := pkt.Layer(layers.LayerTypeARP); al != nil {
		lp.Proto = "ARP"
		if a, ok := al.(*layers.ARP); ok {
			lp.Info = arpInfo(a)
			// ARP isn't a NetworkLayer in gopacket; expose the IPs anyway so
			// the list view doesn't show empty columns for ARP rows.
			if lp.SrcIP == "" {
				lp.SrcIP = ipString(a.SourceProtAddress)
			}
			if lp.DstIP == "" {
				lp.DstIP = ipString(a.DstProtAddress)
			}
		}
	}

	// Application enrichment
	if dnsLayer := pkt.Layer(layers.LayerTypeDNS); dnsLayer != nil {
		if d, ok := dnsLayer.(*layers.DNS); ok {
			lp.Proto = "DNS"
			lp.Info = dnsInfo(d)
		}
	} else if lp.Proto == "TCP" && (lp.DstPort == 443 || lp.SrcPort == 443) {
		if al := pkt.ApplicationLayer(); al != nil {
			if sni := extractSNI(al.Payload()); sni != "" {
				lp.Proto = "TLS"
				lp.Info = "ClientHello sni=" + sni
			} else if isTLSRecord(al.Payload()) {
				lp.Proto = "TLS"
				lp.Info = tlsRecordSummary(al.Payload())
			}
		}
	} else if lp.Proto == "TCP" && (lp.DstPort == 80 || lp.SrcPort == 80) {
		if al := pkt.ApplicationLayer(); al != nil {
			if line := firstHTTPLine(al.Payload()); line != "" {
				lp.Proto = "HTTP"
				lp.Info = line
			}
		}
	} else if lp.Proto == "UDP" && (lp.DstPort == 443 || lp.SrcPort == 443) {
		// QUIC has no easy-to-decode public header without a full parser, but
		// recognising it is useful info on its own.
		lp.Proto = "QUIC"
		lp.Info = "QUIC " + strconv.Itoa(int(lp.SrcPort)) + "→" + strconv.Itoa(int(lp.DstPort))
	}

	if lp.Info == "" {
		lp.Info = lp.Proto + " " + strconv.Itoa(lp.Length) + "B"
	}
	return lp
}

func layerShortName(l gopacket.Layer) string {
	n := l.LayerType().String()
	// strip gopacket's "Linux" prefix to match the L2 label we already added
	if n == "LinuxSLL" {
		return "Linux SLL"
	}
	return n
}

func tcpInfo(t *layers.TCP) string {
	flags := tcpFlagList(t)
	parts := []string{strings.Join(flags, ",")}
	parts = append(parts, fmt.Sprintf("seq=%d", t.Seq))
	if t.ACK {
		parts = append(parts, fmt.Sprintf("ack=%d", t.Ack))
	}
	parts = append(parts, fmt.Sprintf("win=%d", t.Window))
	return strings.Join(parts, " ")
}

func tcpFlagList(t *layers.TCP) []string {
	flags := make([]string, 0, 4)
	if t.SYN {
		flags = append(flags, "SYN")
	}
	if t.ACK {
		flags = append(flags, "ACK")
	}
	if t.FIN {
		flags = append(flags, "FIN")
	}
	if t.RST {
		flags = append(flags, "RST")
	}
	if t.PSH {
		flags = append(flags, "PSH")
	}
	if t.URG {
		flags = append(flags, "URG")
	}
	if len(flags) == 0 {
		flags = append(flags, "-")
	}
	return flags
}

func dnsInfo(d *layers.DNS) string {
	if len(d.Questions) > 0 {
		q := d.Questions[0]
		if d.QR {
			ans := ""
			if len(d.Answers) > 0 {
				ans = " → "
				answers := []string{}
				for i, a := range d.Answers {
					if i >= 3 {
						answers = append(answers, fmt.Sprintf("+%d more", len(d.Answers)-i))
						break
					}
					answers = append(answers, dnsAnswerString(a))
				}
				ans += strings.Join(answers, ", ")
			}
			return "Reply " + string(q.Name) + " " + q.Type.String() + ans
		}
		return "Query " + string(q.Name) + " " + q.Type.String()
	}
	if d.QR {
		return "DNS Reply"
	}
	return "DNS Query"
}

func dnsAnswerString(a layers.DNSResourceRecord) string {
	switch a.Type {
	case layers.DNSTypeA, layers.DNSTypeAAAA:
		if a.IP != nil {
			return a.IP.String()
		}
	case layers.DNSTypeCNAME, layers.DNSTypePTR:
		if len(a.CNAME) > 0 {
			return string(a.CNAME)
		}
		if len(a.PTR) > 0 {
			return string(a.PTR)
		}
	case layers.DNSTypeMX:
		return string(a.MX.Name)
	case layers.DNSTypeTXT:
		if len(a.TXTs) > 0 {
			return string(a.TXTs[0])
		}
	}
	return a.Type.String()
}

func arpInfo(a *layers.ARP) string {
	op := "REQ"
	if a.Operation == layers.ARPReply {
		op = "REPLY"
	}
	return op + " " + ipString(a.SourceProtAddress) + " → " + ipString(a.DstProtAddress)
}

func ipString(b []byte) string {
	if len(b) == 4 {
		return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
	}
	if len(b) == 16 {
		return net.IP(b).String()
	}
	return ""
}

func isTLSRecord(p []byte) bool {
	if len(p) < 5 {
		return false
	}
	return p[0] >= 0x14 && p[0] <= 0x18 // change_cipher / alert / handshake / app_data / heartbeat
}

func tlsRecordSummary(p []byte) string {
	if len(p) < 5 {
		return ""
	}
	rt := map[byte]string{0x14: "ChangeCipherSpec", 0x15: "Alert", 0x16: "Handshake", 0x17: "ApplicationData", 0x18: "Heartbeat"}
	name := rt[p[0]]
	if name == "" {
		name = fmt.Sprintf("type=0x%x", p[0])
	}
	return "TLS " + name + " v" + tlsVersionString(p[1], p[2])
}

func tlsVersionString(maj, min byte) string {
	switch {
	case maj == 3 && min == 0:
		return "SSL3.0"
	case maj == 3 && min == 1:
		return "1.0"
	case maj == 3 && min == 2:
		return "1.1"
	case maj == 3 && min == 3:
		return "1.2"
	case maj == 3 && min == 4:
		return "1.3"
	default:
		return fmt.Sprintf("%d.%d", maj, min)
	}
}

func extractSNI(payload []byte) string {
	if len(payload) < 11 || payload[0] != 0x16 || payload[5] != 0x01 {
		return ""
	}
	p := payload
	off := 43
	if len(p) < off+1 {
		return ""
	}
	off += 1 + int(p[off])
	if len(p) < off+2 {
		return ""
	}
	off += 2 + int(binary.BigEndian.Uint16(p[off:]))
	if len(p) < off+1 {
		return ""
	}
	off += 1 + int(p[off])
	if len(p) < off+2 {
		return ""
	}
	extTotal := int(binary.BigEndian.Uint16(p[off:]))
	off += 2
	end := off + extTotal
	if end > len(p) {
		end = len(p)
	}
	for off+4 <= end {
		extType := binary.BigEndian.Uint16(p[off:])
		extLen := int(binary.BigEndian.Uint16(p[off+2:]))
		off += 4
		if off+extLen > end {
			break
		}
		if extType == 0x00 && extLen >= 5 {
			nameLen := int(binary.BigEndian.Uint16(p[off+3:]))
			if off+5+nameLen <= end {
				return string(p[off+5 : off+5+nameLen])
			}
		}
		off += extLen
	}
	return ""
}

func firstHTTPLine(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	end := len(payload)
	if end > 256 {
		end = 256
	}
	line := payload[:end]
	if idx := bytes.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	s := strings.TrimRight(string(line), "\r")
	for _, r := range s {
		if r < 0x20 && r != '\t' {
			return ""
		}
	}
	if !(strings.HasPrefix(s, "GET ") || strings.HasPrefix(s, "POST ") ||
		strings.HasPrefix(s, "PUT ") || strings.HasPrefix(s, "DELETE ") ||
		strings.HasPrefix(s, "HEAD ") || strings.HasPrefix(s, "OPTIONS ") ||
		strings.HasPrefix(s, "PATCH ") || strings.HasPrefix(s, "HTTP/")) {
		return ""
	}
	return s
}

// ─── Detail (per-layer field tree) ───────────────────────────────────────

func (l *LiveCapture) DescribeLivePacket(serial string, no uint64) *LivePacketDetail {
	l.mu.Lock()
	s, ok := l.sessions[serial]
	l.mu.Unlock()
	if !ok {
		return nil
	}
	s.mu.Lock()
	data, has := s.rawIdx[no]
	linkType := s.linkType
	s.mu.Unlock()
	if !has {
		return nil
	}
	return describePacket(data, no, linkType)
}

func describePacket(data []byte, no uint64, linkType uint32) *LivePacketDetail {
	d := &LivePacketDetail{LivePacket: *decodeLivePacket(data, time.Time{}, no, linkType)}
	d.RawHex = hexDump(data)

	// Re-walk with the same logic, but accumulate per-layer fields.
	l3type, payload, l2name, l2bytes := stripLinkLayer(data, linkType)
	d.LayersFull = append(d.LayersFull, linkLayerDetail(data, linkType, l2name, l2bytes))
	if l3type == gopacket.LayerTypeZero || len(payload) == 0 {
		return d
	}
	pkt := gopacket.NewPacket(payload, l3type, gopacket.Default)
	off := l2bytes
	for _, layer := range pkt.Layers() {
		var det LivePacketLayer
		switch v := layer.(type) {
		case *layers.IPv4:
			det = describeIPv4(v)
		case *layers.IPv6:
			det = describeIPv6(v)
		case *layers.TCP:
			det = describeTCP(v)
		case *layers.UDP:
			det = describeUDP(v)
		case *layers.ICMPv4:
			det = describeICMPv4(v)
		case *layers.ICMPv6:
			det = describeICMPv6(v)
		case *layers.ARP:
			det = describeARP(v)
		case *layers.DNS:
			det = describeDNS(v)
		case *layers.TLS:
			det = describeGeneric("TLS", v.LayerContents())
		case *gopacket.Payload:
			det = describePayload(v.Payload(), d.Proto)
		default:
			det = describeGeneric(layer.LayerType().String(), layer.LayerContents())
		}
		det.Offset = off
		det.Bytes = len(layer.LayerContents())
		off += det.Bytes
		d.LayersFull = append(d.LayersFull, det)
	}
	return d
}

func linkLayerDetail(data []byte, linkType uint32, name string, l2bytes int) LivePacketLayer {
	l := LivePacketLayer{Name: name, Offset: 0, Bytes: l2bytes}
	l.Fields = append(l.Fields, kv("Link type", fmt.Sprintf("%d (%s)", linkType, name)))
	if linkType == 1 && len(data) >= 14 {
		l.Fields = append(l.Fields,
			kv("Dst MAC", macString(data[0:6])),
			kv("Src MAC", macString(data[6:12])),
			kv("EtherType", fmt.Sprintf("0x%04x", binary.BigEndian.Uint16(data[12:14]))),
		)
	} else if linkType == 113 && len(data) >= 16 {
		l.Fields = append(l.Fields,
			kv("Packet type", sllPacketType(binary.BigEndian.Uint16(data[0:2]))),
			kv("ARPHRD", fmt.Sprintf("%d", binary.BigEndian.Uint16(data[2:4]))),
			kv("Addr len", fmt.Sprintf("%d", binary.BigEndian.Uint16(data[4:6]))),
			kv("Src addr", macString(data[6:12])),
			kv("Protocol", fmt.Sprintf("0x%04x", binary.BigEndian.Uint16(data[14:16]))),
		)
	} else if linkType == 276 && len(data) >= 20 {
		l.Fields = append(l.Fields,
			kv("Protocol", fmt.Sprintf("0x%04x", binary.BigEndian.Uint16(data[0:2]))),
			kv("Iface idx", fmt.Sprintf("%d", binary.BigEndian.Uint32(data[4:8]))),
			kv("ARPHRD", fmt.Sprintf("%d", binary.BigEndian.Uint16(data[8:10]))),
			kv("Packet type", sllPacketType(uint16(data[10]))),
			kv("Addr len", fmt.Sprintf("%d", data[11])),
			kv("Src addr", macString(data[12:20])),
		)
	}
	return l
}

func sllPacketType(t uint16) string {
	switch t {
	case 0:
		return "0 (host)"
	case 1:
		return "1 (broadcast)"
	case 2:
		return "2 (multicast)"
	case 3:
		return "3 (other-host)"
	case 4:
		return "4 (outgoing)"
	}
	return fmt.Sprintf("%d", t)
}

func macString(b []byte) string {
	if len(b) < 6 {
		return ""
	}
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4], b[5])
}

func describeIPv4(ip *layers.IPv4) LivePacketLayer {
	l := LivePacketLayer{Name: "IPv4"}
	l.Fields = []LivePacketField{
		kv("Version", "4"),
		kv("IHL", fmt.Sprintf("%d (%d bytes)", ip.IHL, int(ip.IHL)*4)),
		kv("TOS", fmt.Sprintf("0x%02x", ip.TOS)),
		kv("Total length", fmt.Sprintf("%d", ip.Length)),
		kv("ID", fmt.Sprintf("0x%04x (%d)", ip.Id, ip.Id)),
		kv("Flags", ipv4Flags(ip)),
		kv("Fragment offset", fmt.Sprintf("%d", ip.FragOffset)),
		kv("TTL", fmt.Sprintf("%d", ip.TTL)),
		kv("Protocol", fmt.Sprintf("%d (%s)", ip.Protocol, ip.Protocol.String())),
		kv("Checksum", fmt.Sprintf("0x%04x", ip.Checksum)),
		kv("Source", ip.SrcIP.String()),
		kv("Destination", ip.DstIP.String()),
	}
	return l
}

func ipv4Flags(ip *layers.IPv4) string {
	parts := []string{}
	if ip.Flags&layers.IPv4DontFragment != 0 {
		parts = append(parts, "DF")
	}
	if ip.Flags&layers.IPv4MoreFragments != 0 {
		parts = append(parts, "MF")
	}
	if ip.Flags&layers.IPv4EvilBit != 0 {
		parts = append(parts, "Evil")
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ",")
}

func describeIPv6(ip *layers.IPv6) LivePacketLayer {
	return LivePacketLayer{Name: "IPv6", Fields: []LivePacketField{
		kv("Version", "6"),
		kv("Traffic class", fmt.Sprintf("0x%02x", ip.TrafficClass)),
		kv("Flow label", fmt.Sprintf("0x%05x", ip.FlowLabel)),
		kv("Payload length", fmt.Sprintf("%d", ip.Length)),
		kv("Next header", fmt.Sprintf("%d (%s)", ip.NextHeader, ip.NextHeader.String())),
		kv("Hop limit", fmt.Sprintf("%d", ip.HopLimit)),
		kv("Source", ip.SrcIP.String()),
		kv("Destination", ip.DstIP.String()),
	}}
}

func describeTCP(t *layers.TCP) LivePacketLayer {
	flags := tcpFlagList(t)
	fields := []LivePacketField{
		kv("Source port", fmt.Sprintf("%d", t.SrcPort)),
		kv("Destination port", fmt.Sprintf("%d", t.DstPort)),
		kv("Sequence", fmt.Sprintf("%d", t.Seq)),
		kv("Acknowledgment", fmt.Sprintf("%d", t.Ack)),
		kv("Data offset", fmt.Sprintf("%d (%d bytes)", t.DataOffset, int(t.DataOffset)*4)),
		kv("Flags", strings.Join(flags, ",")),
		kv("Window", fmt.Sprintf("%d", t.Window)),
		kv("Checksum", fmt.Sprintf("0x%04x", t.Checksum)),
		kv("Urgent", fmt.Sprintf("%d", t.Urgent)),
	}
	for _, opt := range t.Options {
		fields = append(fields, kv("Option "+opt.OptionType.String(), tcpOptionValue(opt)))
	}
	return LivePacketLayer{Name: "TCP", Fields: fields}
}

func tcpOptionValue(opt layers.TCPOption) string {
	if len(opt.OptionData) == 0 {
		return "-"
	}
	switch opt.OptionType {
	case layers.TCPOptionKindMSS:
		if len(opt.OptionData) == 2 {
			return fmt.Sprintf("%d", binary.BigEndian.Uint16(opt.OptionData))
		}
	case layers.TCPOptionKindWindowScale:
		return fmt.Sprintf("%d", opt.OptionData[0])
	}
	return hex.EncodeToString(opt.OptionData)
}

func describeUDP(u *layers.UDP) LivePacketLayer {
	return LivePacketLayer{Name: "UDP", Fields: []LivePacketField{
		kv("Source port", fmt.Sprintf("%d", u.SrcPort)),
		kv("Destination port", fmt.Sprintf("%d", u.DstPort)),
		kv("Length", fmt.Sprintf("%d", u.Length)),
		kv("Checksum", fmt.Sprintf("0x%04x", u.Checksum)),
	}}
}

func describeICMPv4(i *layers.ICMPv4) LivePacketLayer {
	return LivePacketLayer{Name: "ICMPv4", Fields: []LivePacketField{
		kv("Type/Code", i.TypeCode.String()),
		kv("Checksum", fmt.Sprintf("0x%04x", i.Checksum)),
		kv("ID", fmt.Sprintf("%d", i.Id)),
		kv("Sequence", fmt.Sprintf("%d", i.Seq)),
	}}
}

func describeICMPv6(i *layers.ICMPv6) LivePacketLayer {
	return LivePacketLayer{Name: "ICMPv6", Fields: []LivePacketField{
		kv("Type/Code", i.TypeCode.String()),
		kv("Checksum", fmt.Sprintf("0x%04x", i.Checksum)),
	}}
}

func describeARP(a *layers.ARP) LivePacketLayer {
	op := "request"
	if a.Operation == layers.ARPReply {
		op = "reply"
	}
	return LivePacketLayer{Name: "ARP", Fields: []LivePacketField{
		kv("Operation", op),
		kv("Hardware type", fmt.Sprintf("%d", a.AddrType)),
		kv("Protocol type", fmt.Sprintf("0x%04x", a.Protocol)),
		kv("Sender MAC", macString(a.SourceHwAddress)),
		kv("Sender IP", ipString(a.SourceProtAddress)),
		kv("Target MAC", macString(a.DstHwAddress)),
		kv("Target IP", ipString(a.DstProtAddress)),
	}}
}

func describeDNS(d *layers.DNS) LivePacketLayer {
	fields := []LivePacketField{
		kv("Transaction ID", fmt.Sprintf("0x%04x", d.ID)),
		kv("Response", boolStr(d.QR)),
		kv("Opcode", d.OpCode.String()),
		kv("Authoritative", boolStr(d.AA)),
		kv("Truncated", boolStr(d.TC)),
		kv("Recursion desired", boolStr(d.RD)),
		kv("Recursion available", boolStr(d.RA)),
		kv("Response code", d.ResponseCode.String()),
		kv("Questions", fmt.Sprintf("%d", len(d.Questions))),
		kv("Answers", fmt.Sprintf("%d", len(d.Answers))),
		kv("Authorities", fmt.Sprintf("%d", len(d.Authorities))),
		kv("Additionals", fmt.Sprintf("%d", len(d.Additionals))),
	}
	for i, q := range d.Questions {
		fields = append(fields, kv(fmt.Sprintf("Question %d", i+1), fmt.Sprintf("%s %s %s", string(q.Name), q.Type.String(), q.Class.String())))
	}
	for i, a := range d.Answers {
		fields = append(fields, kv(fmt.Sprintf("Answer %d", i+1), fmt.Sprintf("%s %s ttl=%d → %s", string(a.Name), a.Type.String(), a.TTL, dnsAnswerString(a))))
	}
	return LivePacketLayer{Name: "DNS", Fields: fields}
}

// describePayload interprets the application payload when one of the well-
// known protocols is in play.
func describePayload(payload []byte, proto string) LivePacketLayer {
	name := "Payload"
	fields := []LivePacketField{
		kv("Length", fmt.Sprintf("%d", len(payload))),
	}
	if sni := extractSNI(payload); sni != "" {
		name = "TLS ClientHello"
		fields = append(fields,
			kv("Server name", sni),
			kv("TLS version", tlsVersionString(payload[1], payload[2])),
		)
	} else if isTLSRecord(payload) {
		name = "TLS Record"
		fields = append(fields,
			kv("Content type", fmt.Sprintf("0x%02x", payload[0])),
			kv("TLS version", tlsVersionString(payload[1], payload[2])),
			kv("Length", fmt.Sprintf("%d", binary.BigEndian.Uint16(payload[3:5]))),
		)
	} else if line := firstHTTPLine(payload); line != "" {
		name = "HTTP"
		fields = append(fields, kv("Request/Status line", line))
		// Pull a few headers for context.
		hdrs := bytes.Split(payload, []byte("\r\n"))
		shown := 0
		for _, h := range hdrs[1:] {
			if len(h) == 0 {
				break
			}
			if shown >= 6 {
				fields = append(fields, kv("…", fmt.Sprintf("+%d more headers", len(hdrs)-1-shown)))
				break
			}
			fields = append(fields, kv("Header", string(h)))
			shown++
		}
	}
	return LivePacketLayer{Name: name, Fields: fields}
}

func describeGeneric(name string, raw []byte) LivePacketLayer {
	return LivePacketLayer{Name: name, Fields: []LivePacketField{
		kv("Length", fmt.Sprintf("%d", len(raw))),
	}}
}

func kv(k, v string) LivePacketField { return LivePacketField{K: k, V: v} }

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func hexDump(b []byte) string {
	var sb strings.Builder
	const cols = 16
	for i := 0; i < len(b); i += cols {
		end := i + cols
		if end > len(b) {
			end = len(b)
		}
		fmt.Fprintf(&sb, "%04x  ", i)
		for j := i; j < end; j++ {
			fmt.Fprintf(&sb, "%02x ", b[j])
		}
		for j := end; j < i+cols; j++ {
			sb.WriteString("   ")
		}
		sb.WriteByte(' ')
		for j := i; j < end; j++ {
			c := b[j]
			if c < 0x20 || c > 0x7e {
				c = '.'
			}
			sb.WriteByte(c)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
