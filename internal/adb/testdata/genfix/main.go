// Command genfix regenerates the live-capture decoder test fixtures with
// synthetic traffic only. No real host/network data is embedded: addresses come
// from RFC 5737 documentation ranges (192.0.2.0/24, 198.51.100.0/24) and MACs
// are locally-administered placeholders. Run from the repo root:
//
//	go run ./internal/adb/testdata/genfix
//
// It rewrites eth_wlan.pcap, sll2_icmp.pcap and sll2_mixed.pcap in the parent
// directory. The packet mix is chosen so the existing assertions in
// pcap_live_test.go still hold (SLL2 link type 276 with an ICMP packet,
// Ethernet link type 1 with IP packets, and a loadable mixed SLL2 capture).
package main

import (
	"encoding/binary"
	"log"
	"net"
	"os"
	"path/filepath"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

const (
	dltEthernet = 1
	dltSLL2     = 276
)

// Documentation-only addresses (RFC 5737 / locally-administered MACs).
var (
	clientIP = net.IP{192, 0, 2, 10}
	peerIP   = net.IP{198, 51, 100, 20}
	dnsIP    = net.IP{192, 0, 2, 53}
	srvIP    = net.IP{198, 51, 100, 80}

	macClient = net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	macPeer   = net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x02}
)

// fixed timestamps keep regeneration deterministic.
const baseSec = 1700000000

func serializeL3(ipProto layers.IPProtocol, l4, payload gopacket.SerializableLayer) []byte {
	ip := &layers.IPv4{
		Version:  4,
		IHL:      5,
		TTL:      64,
		Protocol: ipProto,
		SrcIP:    clientIP,
		DstIP:    peerIP,
	}
	return serializeWithIP(ip, l4, payload)
}

func serializeWithIP(ip *layers.IPv4, l4, payload gopacket.SerializableLayer) []byte {
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	stack := []gopacket.SerializableLayer{ip}
	if l4 != nil {
		if csum, ok := l4.(interface {
			SetNetworkLayerForChecksum(gopacket.NetworkLayer) error
		}); ok {
			if err := csum.SetNetworkLayerForChecksum(ip); err != nil {
				log.Fatalf("checksum setup: %v", err)
			}
		}
		stack = append(stack, l4)
	}
	if payload != nil {
		stack = append(stack, payload)
	}
	if err := gopacket.SerializeLayers(buf, opts, stack...); err != nil {
		log.Fatalf("serialize: %v", err)
	}
	return buf.Bytes()
}

func icmpEcho(req bool, id, seq uint16) []byte {
	var t uint8 = layers.ICMPv4TypeEchoReply
	if req {
		t = layers.ICMPv4TypeEchoRequest
	}
	icmp := &layers.ICMPv4{
		TypeCode: layers.CreateICMPv4TypeCode(t, 0),
		Id:       id,
		Seq:      seq,
	}
	return serializeL3(layers.IPProtocolICMPv4, icmp, gopacket.Payload([]byte("synthetic-fixture-payload")))
}

func udpDNS(srcPort, dstPort layers.UDPPort, ip *layers.IPv4) []byte {
	udp := &layers.UDP{SrcPort: srcPort, DstPort: dstPort}
	// Minimal DNS-ish payload — content is irrelevant to the decoder tests.
	q := []byte{0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 0x03, 'n', 'e', 't', 0x00, 0x00, 0x01, 0x00, 0x01}
	return serializeWithIP(ip, udp, gopacket.Payload(q))
}

func tcpHTTP(srcPort, dstPort layers.TCPPort, syn bool, ip *layers.IPv4, payload []byte) []byte {
	tcp := &layers.TCP{SrcPort: srcPort, DstPort: dstPort, Seq: 1000, Window: 65535, SYN: syn, ACK: !syn}
	var pl gopacket.SerializableLayer
	if len(payload) > 0 {
		pl = gopacket.Payload(payload)
	}
	return serializeWithIP(ip, tcp, pl)
}

func ethFrame(et uint16, l3 []byte) []byte {
	hdr := make([]byte, 14)
	copy(hdr[0:6], macPeer)
	copy(hdr[6:12], macClient)
	binary.BigEndian.PutUint16(hdr[12:14], et)
	return append(hdr, l3...)
}

func sll2Frame(et uint16, l3 []byte) []byte {
	// SLL2 header: protocol_type(2) reserved(2) iface_idx(4) arphrd(2) pkt_type(1) ll_len(1) ll_addr(8)
	hdr := make([]byte, 20)
	binary.BigEndian.PutUint16(hdr[0:2], et)
	binary.BigEndian.PutUint32(hdr[4:8], 1) // iface index
	binary.BigEndian.PutUint16(hdr[8:10], 1)
	hdr[10] = 0 // pkt type
	hdr[11] = 6 // ll addr len
	copy(hdr[12:18], macClient)
	return append(hdr, l3...)
}

func writePcap(path string, linkType uint32, frames [][]byte) {
	var out []byte
	hdr := make([]byte, 24)
	binary.LittleEndian.PutUint32(hdr[0:4], 0xa1b2c3d4)
	binary.LittleEndian.PutUint16(hdr[4:6], 2)
	binary.LittleEndian.PutUint16(hdr[6:8], 4)
	binary.LittleEndian.PutUint32(hdr[16:20], 262144) // snaplen
	binary.LittleEndian.PutUint32(hdr[20:24], linkType)
	out = append(out, hdr...)
	for i, f := range frames {
		rec := make([]byte, 16)
		binary.LittleEndian.PutUint32(rec[0:4], uint32(baseSec+i))
		binary.LittleEndian.PutUint32(rec[4:8], uint32(i*1000))
		binary.LittleEndian.PutUint32(rec[8:12], uint32(len(f)))
		binary.LittleEndian.PutUint32(rec[12:16], uint32(len(f)))
		out = append(out, rec...)
		out = append(out, f...)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
	log.Printf("wrote %s (%d bytes, %d packets, linktype %d)", path, len(out), len(frames), linkType)
}

func main() {
	dir := filepath.Join("internal", "adb", "testdata")
	if _, err := os.Stat(dir); err != nil {
		log.Fatalf("run from repo root: %v", err)
	}

	const ip4 = 0x0800

	// eth_wlan.pcap — Ethernet (DLT 1), IPv4 packets only so every packet has
	// a src/dst IP (the Ethernet test asserts non-empty addresses).
	writePcap(filepath.Join(dir, "eth_wlan.pcap"), dltEthernet, [][]byte{
		ethFrame(ip4, icmpEcho(true, 1, 1)),
		ethFrame(ip4, icmpEcho(false, 1, 1)),
		ethFrame(ip4, udpDNS(50000, 53, &layers.IPv4{Version: 4, IHL: 5, TTL: 64, Protocol: layers.IPProtocolUDP, SrcIP: clientIP, DstIP: dnsIP})),
	})

	// sll2_icmp.pcap — SLL2 (DLT 276) with at least one ICMP packet.
	writePcap(filepath.Join(dir, "sll2_icmp.pcap"), dltSLL2, [][]byte{
		sll2Frame(ip4, icmpEcho(true, 2, 1)),
		sll2Frame(ip4, icmpEcho(true, 2, 2)),
		sll2Frame(ip4, udpDNS(50001, 53, &layers.IPv4{Version: 4, IHL: 5, TTL: 64, Protocol: layers.IPProtocolUDP, SrcIP: clientIP, DstIP: dnsIP})),
	})

	// sll2_mixed.pcap — SLL2 with a mix of ICMP/UDP/TCP. Eyeball fixture, no
	// hard assertions; just needs to be a loadable pcap.
	httpIP := &layers.IPv4{Version: 4, IHL: 5, TTL: 64, Protocol: layers.IPProtocolTCP, SrcIP: clientIP, DstIP: srvIP}
	writePcap(filepath.Join(dir, "sll2_mixed.pcap"), dltSLL2, [][]byte{
		sll2Frame(ip4, icmpEcho(true, 3, 1)),
		sll2Frame(ip4, udpDNS(50002, 53, &layers.IPv4{Version: 4, IHL: 5, TTL: 64, Protocol: layers.IPProtocolUDP, SrcIP: clientIP, DstIP: dnsIP})),
		sll2Frame(ip4, tcpHTTP(50003, 80, true, httpIP, nil)),
		sll2Frame(ip4, tcpHTTP(50003, 80, false, httpIP, []byte("GET / HTTP/1.1\r\nHost: example.net\r\n\r\n"))),
	})
}
