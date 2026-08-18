package adb

import (
	"testing"
	"time"
)

func pkt(no uint64) *LivePacket {
	return &LivePacket{No: no, Ts: time.Unix(int64(no), 0), Length: int(no)}
}

func TestPacketRingKeepsTheMostRecent(t *testing.T) {
	r := newPacketRing(4)
	for i := uint64(1); i <= 4; i++ {
		r.Add(pkt(i), []byte{byte(i)})
	}
	if r.Len() != 4 {
		t.Fatalf("Len = %d, want 4", r.Len())
	}
	for i := uint64(1); i <= 4; i++ {
		if p := r.Packet(i); p == nil || p.No != i {
			t.Errorf("Packet(%d) = %v", i, p)
		}
	}

	// Overflow by three: 1..3 age out, 5..7 arrive.
	for i := uint64(5); i <= 7; i++ {
		r.Add(pkt(i), []byte{byte(i)})
	}
	if r.Len() != 4 {
		t.Fatalf("Len = %d after overflow, want 4", r.Len())
	}
	for _, gone := range []uint64{1, 2, 3} {
		if p := r.Packet(gone); p != nil {
			t.Errorf("Packet(%d) = %+v, want nil (aged out)", gone, p)
		}
		if raw := r.Raw(gone); raw != nil {
			t.Errorf("Raw(%d) = %v, want nil (aged out)", gone, raw)
		}
	}
	for _, live := range []uint64{4, 5, 6, 7} {
		p := r.Packet(live)
		if p == nil || p.No != live {
			t.Fatalf("Packet(%d) = %v, want the packet", live, p)
		}
		raw := r.Raw(live)
		if len(raw) != 1 || raw[0] != byte(live) {
			t.Errorf("Raw(%d) = %v, want [%d]", live, raw, live)
		}
	}
}

// The failure this guards against is the quiet one: slots are reused, so a
// lookup for an aged-out packet must not return whichever packet later landed
// on the same index. The detail pane would show the wrong bytes and look right.
func TestPacketRingDoesNotConfuseReusedSlots(t *testing.T) {
	r := newPacketRing(3)
	for i := uint64(1); i <= 9; i++ {
		r.Add(pkt(i), []byte{byte(i)})
	}
	// 1..6 have aged out; each shares a slot with one of 7,8,9.
	for _, gone := range []uint64{1, 2, 3, 4, 5, 6} {
		if p := r.Packet(gone); p != nil {
			t.Errorf("Packet(%d) returned packet %d from a reused slot", gone, p.No)
		}
	}
	for _, live := range []uint64{7, 8, 9} {
		if p := r.Packet(live); p == nil || p.No != live {
			t.Errorf("Packet(%d) = %v", live, p)
		}
	}
}

func TestPacketRingSnapshotIsOldestFirst(t *testing.T) {
	r := newPacketRing(3)
	for i := uint64(1); i <= 5; i++ {
		r.Add(pkt(i), nil)
	}
	snap := r.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("Snapshot len = %d, want 3", len(snap))
	}
	for i, want := range []uint64{3, 4, 5} {
		if snap[i].No != want {
			t.Errorf("Snapshot[%d].No = %d, want %d", i, snap[i].No, want)
		}
	}
}

func TestPacketRingEdgeCases(t *testing.T) {
	r := newPacketRing(2)
	if p := r.Packet(0); p != nil {
		t.Error("packet 0 does not exist; numbering is 1-based")
	}
	if p := r.Packet(1); p != nil {
		t.Error("empty ring returned a packet")
	}
	if len(r.Snapshot()) != 0 {
		t.Error("empty ring produced a non-empty snapshot")
	}

	r.Add(pkt(1), nil)
	if p := r.Packet(2); p != nil {
		t.Error("returned a packet that has not arrived yet")
	}

	r.Reset()
	if r.Len() != 0 || len(r.Snapshot()) != 0 || r.Packet(1) != nil {
		t.Error("Reset left state behind")
	}

	// A zero/negative capacity must not panic; it falls back to the default.
	if got := newPacketRing(0).Cap(); got != liveRingDefault {
		t.Errorf("newPacketRing(0).Cap() = %d, want %d", got, liveRingDefault)
	}
	if got := newPacketRing(-5).Cap(); got != liveRingDefault {
		t.Errorf("newPacketRing(-5).Cap() = %d, want %d", got, liveRingDefault)
	}
}

// Eviction used to walk the whole collection per packet — O(n) each, O(n²) over
// a window — which with the 100k-packet option was a stall rather than a
// slowdown. This pins the cost as constant.
func BenchmarkPacketRingAddWhenFull(b *testing.B) {
	r := newPacketRing(100000)
	raw := make([]byte, 128)
	for i := 0; i < 100000; i++ {
		r.Add(pkt(uint64(i+1)), raw)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Add(pkt(uint64(100001+i)), raw)
	}
}
