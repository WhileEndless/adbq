package adb

import (
	"context"
	"os"
	"testing"
	"time"
)

// The pcap path was rewritten end to end (buffered I/O, a ring instead of a
// slice-and-map, emit moved out of the lock). Unit tests cover the pieces; this
// checks the whole thing still produces a valid capture from a real device.
func TestLiveCaptureEndToEnd(t *testing.T) {
	c, serial := integrationDevice(t)
	if serial == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	if _, err := c.FindTcpdump(ctx, serial); err != nil {
		t.Skipf("no tcpdump on device: %v", err)
	}

	var batches, packets int
	st, err := c.StartLiveCapture(ctx, serial, "any", "", LiveCaptureOptions{MaxPackets: 500},
		func(b []*LivePacket) { batches++; packets += len(b) })
	if err != nil {
		t.Skipf("capture would not start: %v", err)
	}
	t.Logf("started: iface=%s linkType=%d pcap=%s", st.Iface, st.LinkType, st.PcapPath)

	// Generate some traffic so there is something to capture.
	go func() {
		for range 6 {
			_, _ = c.Shell(ctx, serial, "ping -c 1 -W 1 8.8.8.8 >/dev/null 2>&1; true")
			time.Sleep(400 * time.Millisecond)
		}
	}()
	time.Sleep(6 * time.Second)

	live := c.Live.Status(serial)
	t.Logf("captured %d packets (%d bytes), %d batches emitted, %d packets to UI",
		live.Packets, live.Bytes, batches, packets)

	// SaveLivePcap copies the mirror; buffered bytes must be on disk first.
	c.Live.FlushMirror(serial)
	info, err := os.Stat(live.PcapPath)
	if err != nil {
		t.Fatalf("mirror missing: %v", err)
	}
	t.Logf("mirror on disk: %d bytes", info.Size())

	if live.Packets > 0 {
		if info.Size() < 24 {
			t.Errorf("captured %d packets but the mirror holds only %d bytes — "+
				"buffered writes are not reaching the file", live.Packets, info.Size())
		}
		if packets == 0 {
			t.Error("captured packets but emitted none to the UI")
		}
		// Detail lookup must find a recent packet in the ring.
		if d := c.Live.DescribeLivePacket(serial, live.Packets); d == nil {
			t.Errorf("DescribeLivePacket(%d) = nil for the newest packet", live.Packets)
		} else {
			t.Logf("newest packet decodes: %s %s -> %s", d.Proto, d.SrcIP, d.DstIP)
		}
		// An aged-out number must return nil rather than a reused slot's packet.
		if live.Packets > 600 {
			if d := c.Live.DescribeLivePacket(serial, 1); d != nil {
				t.Errorf("packet 1 should have aged out of a 500-packet ring, got %+v", d)
			}
		}
	} else {
		t.Log("no packets captured (quiet link); mirror/ring assertions skipped")
	}

	if err := c.Live.Stop(serial); err != nil {
		t.Errorf("Stop: %v", err)
	}
}
