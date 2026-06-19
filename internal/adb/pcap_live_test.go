package adb

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// buildEthIPv4TCP creates a minimal Ethernet/IPv4/TCP frame for decode tests.
// We can't rely on a real pcap fixture in-tree, but gopacket is happy to
// serialize whatever we hand it and decodeLivePacket should parse it back.
func buildEthIPv4TCP(t *testing.T, dstPort layers.TCPPort, payload []byte) []byte {
	t.Helper()
	eth := layers.Ethernet{
		SrcMAC:       []byte{0xaa, 0xbb, 0xcc, 0x00, 0x00, 0x01},
		DstMAC:       []byte{0xaa, 0xbb, 0xcc, 0x00, 0x00, 0x02},
		EthernetType: layers.EthernetTypeIPv4,
	}
	ip := layers.IPv4{
		Version: 4, IHL: 5, TTL: 64,
		Protocol: layers.IPProtocolTCP,
		SrcIP:    []byte{10, 0, 0, 1},
		DstIP:    []byte{1, 1, 1, 1},
	}
	tcp := layers.TCP{
		SrcPort: 49152, DstPort: dstPort,
		Seq: 1, Window: 65535,
		SYN: true,
	}
	_ = tcp.SetNetworkLayerForChecksum(&ip)
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	pl := gopacket.Payload(payload)
	if err := gopacket.SerializeLayers(buf, opts, &eth, &ip, &tcp, pl); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return buf.Bytes()
}

func TestDecodeLivePacketTCP(t *testing.T) {
	data := buildEthIPv4TCP(t, 443, nil)
	lp := decodeLivePacket(data, time.Unix(1700000000, 0), 1, uint32(layers.LinkTypeEthernet))
	if lp.Proto != "TCP" {
		t.Errorf("proto=%q want TCP", lp.Proto)
	}
	if lp.SrcIP != "10.0.0.1" || lp.DstIP != "1.1.1.1" {
		t.Errorf("ips=%s→%s", lp.SrcIP, lp.DstIP)
	}
	if lp.DstPort != 443 {
		t.Errorf("dstPort=%d", lp.DstPort)
	}
	if lp.Info == "" || lp.Info[:3] != "SYN" {
		t.Errorf("info=%q want SYN…", lp.Info)
	}
}

func TestExtractSNI(t *testing.T) {
	// Minimal TLS ClientHello with one SNI extension ("example.com")
	// Record header: type 0x16, version 0x0303, length filled below.
	host := "example.com"
	// Build inside out for clarity.
	var sni []byte
	sni = append(sni, 0x00, 0x00) // ext type server_name
	// server_name list len (2) + name type (1) + name len (2) + name
	inner := []byte{0x00, byte(len(host))}
	inner = append(inner, []byte(host)...)
	sniList := []byte{0x00} // name_type=host_name
	sniList = append(sniList, inner...)
	sniListLen := []byte{0x00, byte(len(sniList))}
	sni = append(sni, 0x00, byte(len(sniListLen)+len(sniList))) // ext_len
	sni = append(sni, sniListLen...)
	sni = append(sni, sniList...)

	var ch []byte
	ch = append(ch, 0x03, 0x03)             // client_version
	ch = append(ch, make([]byte, 32)...)    // random
	ch = append(ch, 0x00)                   // session_id len
	ch = append(ch, 0x00, 0x02, 0x00, 0x2f) // cipher_suites (1 entry)
	ch = append(ch, 0x01, 0x00)             // compression_methods
	ch = append(ch, 0x00, byte(len(sni)))   // extensions length
	ch = append(ch, sni...)

	// Handshake header: msg_type=1 + length(3) + body
	hs := []byte{0x01, 0x00, 0x00, byte(len(ch))}
	hs = append(hs, ch...)

	// Record header
	rec := []byte{0x16, 0x03, 0x03, 0x00, byte(len(hs))}
	rec = append(rec, hs...)

	got := extractSNI(rec)
	if got != host {
		t.Errorf("SNI=%q want %q", got, host)
	}

	// Non-TLS payload should yield empty.
	if extractSNI([]byte("GET / HTTP/1.1\r\n")) != "" {
		t.Errorf("non-TLS payload should not match")
	}
}

func TestFirstHTTPLine(t *testing.T) {
	if got := firstHTTPLine([]byte("GET /index.html HTTP/1.1\r\nHost: x\r\n")); got != "GET /index.html HTTP/1.1" {
		t.Errorf("http line=%q", got)
	}
	if got := firstHTTPLine([]byte{0xff, 0x00, 0x01}); got != "" {
		t.Errorf("binary should not be detected as HTTP, got %q", got)
	}
}

// loadPcapFixture reads a .pcap file and yields (linkType, packets[]). The
// fixtures are synthetic (see testdata/genfix) but byte-compatible with what an
// Android device's tcpdump emits, so they exercise the SLL2/Ethernet decoders.
func loadPcapFixture(t *testing.T, path string) (uint32, [][]byte) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	lt, order, err := readPcapHeader(f)
	if err != nil {
		t.Fatalf("header: %v", err)
	}
	var pkts [][]byte
	for {
		_, data, err := readPcapRecord(f, order)
		if err != nil {
			break
		}
		pkts = append(pkts, data)
	}
	return lt, pkts
}

func TestDecodeRealSLL2(t *testing.T) {
	lt, pkts := loadPcapFixture(t, "testdata/sll2_icmp.pcap")
	if lt != 276 {
		t.Fatalf("expected SLL2 (276), got %d", lt)
	}
	if len(pkts) == 0 {
		t.Fatal("no packets")
	}
	foundICMP := false
	for i, p := range pkts {
		lp := decodeLivePacket(p, time.Unix(0, 0), uint64(i+1), lt)
		if lp.SrcIP == "" || lp.DstIP == "" {
			t.Errorf("packet %d: empty src/dst (proto=%q layers=%v)", i+1, lp.Proto, lp.Layers)
		}
		if lp.Proto == "ICMP" {
			foundICMP = true
		}
		if len(lp.Layers) < 2 {
			t.Errorf("packet %d: too few layers %v", i+1, lp.Layers)
		}
		// Detail walk should also succeed.
		d := describePacket(p, uint64(i+1), lt)
		if len(d.LayersFull) < 2 {
			t.Errorf("packet %d: detail has %d layers", i+1, len(d.LayersFull))
		}
		if d.RawHex == "" {
			t.Errorf("packet %d: empty hex dump", i+1)
		}
	}
	if !foundICMP {
		t.Errorf("expected at least one ICMP packet (ping was generated during capture)")
	}
}

func TestDecodeRealEthernet(t *testing.T) {
	lt, pkts := loadPcapFixture(t, "testdata/eth_wlan.pcap")
	if lt != 1 {
		t.Fatalf("expected Ethernet (1), got %d", lt)
	}
	if len(pkts) == 0 {
		t.Fatal("no packets")
	}
	for i, p := range pkts {
		lp := decodeLivePacket(p, time.Unix(0, 0), uint64(i+1), lt)
		if lp.SrcIP == "" || lp.DstIP == "" {
			t.Errorf("eth packet %d: empty src/dst (proto=%q layers=%v info=%q)", i+1, lp.Proto, lp.Layers, lp.Info)
		}
	}
}

// TestDumpMixed is a developer-facing eyeball: run with `go test -run
// TestDumpMixed -v` to see the decoded view of the mixed SLL2 fixture. Tags
// itself as Skip when run without the verbose flag so CI stays quiet.
func TestDumpMixed(t *testing.T) {
	if !testing.Verbose() {
		t.Skip("verbose only")
	}
	lt, pkts := loadPcapFixture(t, "testdata/sll2_mixed.pcap")
	t.Logf("link type %d, %d packets", lt, len(pkts))
	for i, p := range pkts {
		lp := decodeLivePacket(p, time.Unix(0, 0), uint64(i+1), lt)
		t.Logf("#%d %s:%d → %s:%d  %s  %dB  %s  layers=%v",
			lp.No, lp.SrcIP, lp.SrcPort, lp.DstIP, lp.DstPort, lp.Proto, lp.Length, lp.Info, lp.Layers)
	}
}

func TestMirrorRotation(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "mirror-*.pcap")
	if err != nil {
		t.Fatal(err)
	}
	defer tmp.Close()
	hdr := bytes.Repeat([]byte{0xAA}, 24)
	const cap = 1000
	s := &liveSession{
		pcapFile:   tmp,
		pcapHeader: hdr,
		pcapBytes:  int64(len(hdr)),
		state:      LiveCaptureState{MaxPcapBytes: cap, PcapBytes: int64(len(hdr))},
	}
	if _, err := tmp.Write(hdr); err != nil {
		t.Fatal(err)
	}
	mw := &mirrorWriter{s: s}
	// 24 (hdr) + 500 = 524 < cap → no rotation.
	if _, err := mw.Write(bytes.Repeat([]byte{0x11}, 500)); err != nil {
		t.Fatal(err)
	}
	if s.state.PcapRotations != 0 {
		t.Errorf("rotated too early: %d", s.state.PcapRotations)
	}
	// 524 + 600 = 1124 > cap → rotate, then write 600. pcapBytes becomes 24+600.
	if _, err := mw.Write(bytes.Repeat([]byte{0x22}, 600)); err != nil {
		t.Fatal(err)
	}
	if s.state.PcapRotations != 1 {
		t.Errorf("expected 1 rotation, got %d", s.state.PcapRotations)
	}
	if s.pcapBytes != int64(len(hdr))+600 {
		t.Errorf("pcapBytes=%d", s.pcapBytes)
	}
	// File on disk should start with the replayed header.
	if _, err := tmp.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(tmp.Name())
	if !bytes.Equal(got[:24], hdr) {
		t.Errorf("header not replayed: %x", got[:24])
	}
	if len(got) != int(len(hdr))+600 {
		t.Errorf("on-disk size=%d", len(got))
	}
}

func TestLiveCaptureOptionsClamp(t *testing.T) {
	if mp, mb := (LiveCaptureOptions{}).normalize(); mp != liveRingDefault || mb != liveMirrorDefault {
		t.Errorf("defaults wrong: %d / %d", mp, mb)
	}
	if mp, _ := (LiveCaptureOptions{MaxPackets: 10}).normalize(); mp != liveRingMin {
		t.Errorf("low clamp wrong: %d", mp)
	}
	if mp, _ := (LiveCaptureOptions{MaxPackets: 999_999_999}).normalize(); mp != liveRingMax {
		t.Errorf("high clamp wrong: %d", mp)
	}
}
