package adb

// packetRing holds the most recent N captured packets, together with the raw
// bytes each was decoded from, in fixed memory.
//
// The two collections it replaces both degraded as the capture ran. The decoded
// list was a slice re-sliced from the front on every append, so its backing
// array was never reused and `append` reallocated and copied the whole window
// again and again. The raw bytes lived in a map keyed by packet number, and
// eviction walked the ENTIRE map once per packet to find keys below a cutoff —
// O(n) per packet, O(n²) over the window. With the 100k-packet option that is
// a hundred thousand map iterations for every packet arriving, which is not
// slow so much as it is a stall.
//
// A ring answers both needs in O(1) and allocates once: packet number N lives
// at N mod capacity, and the number itself records whether the slot is still
// the packet the caller asked for.
type packetRing struct {
	packets []*LivePacket
	raw     [][]byte
	// first and count describe the live window: packet numbers
	// [first, first+count) are present. Numbers are 1-based, matching
	// LivePacket.No.
	first uint64
	count int
}

func newPacketRing(capacity int) *packetRing {
	if capacity <= 0 {
		capacity = liveRingDefault
	}
	return &packetRing{
		packets: make([]*LivePacket, capacity),
		raw:     make([][]byte, capacity),
		first:   1,
	}
}

// Cap returns the ring's capacity.
func (r *packetRing) Cap() int { return len(r.packets) }

// Add stores a packet and the bytes it was decoded from, evicting the oldest
// when full. raw may be nil.
func (r *packetRing) Add(p *LivePacket, raw []byte) {
	n := len(r.packets)
	if n == 0 {
		return
	}
	idx := int((p.No - 1) % uint64(n))
	// Dropping the reference matters: without it the ring pins the evicted
	// packet's bytes until the slot is overwritten again, which for a large
	// capacity and a slow capture is the whole window's worth of memory held
	// past its usefulness.
	r.packets[idx] = p
	r.raw[idx] = raw

	if r.count < n {
		r.count++
	} else {
		r.first++
	}
	// A capture that starts mid-stream (or a ring resized underneath us) can
	// leave first behind the oldest number the window actually holds.
	if p.No >= uint64(n) && r.first < p.No-uint64(n)+1 {
		r.first = p.No - uint64(n) + 1
	}
}

// Raw returns the bytes packet `no` was decoded from, or nil once it has aged
// out. The stored number is re-checked because the slot is reused: without that
// check a lookup for an evicted packet would confidently return whichever
// packet later landed on the same index.
func (r *packetRing) Raw(no uint64) []byte {
	p, raw := r.at(no)
	if p == nil {
		return nil
	}
	return raw
}

// Packet returns the decoded packet `no`, or nil once it has aged out.
func (r *packetRing) Packet(no uint64) *LivePacket {
	p, _ := r.at(no)
	return p
}

func (r *packetRing) at(no uint64) (*LivePacket, []byte) {
	n := len(r.packets)
	if n == 0 || no == 0 || no < r.first || no >= r.first+uint64(r.count) {
		return nil, nil
	}
	idx := int((no - 1) % uint64(n))
	p := r.packets[idx]
	if p == nil || p.No != no {
		return nil, nil
	}
	return p, r.raw[idx]
}

// Snapshot returns the live packets in arrival order, oldest first.
//
// This allocates, deliberately: callers hand the result to a JSON encoder or
// keep it, and returning the internal arrays would let them observe the ring
// mutating underneath. It is called on demand, not per packet.
func (r *packetRing) Snapshot() []*LivePacket {
	out := make([]*LivePacket, 0, r.count)
	n := len(r.packets)
	for i := 0; i < r.count; i++ {
		no := r.first + uint64(i)
		idx := int((no - 1) % uint64(n))
		if p := r.packets[idx]; p != nil && p.No == no {
			out = append(out, p)
		}
	}
	return out
}

// Len returns how many packets the ring currently holds.
func (r *packetRing) Len() int { return r.count }

// Reset empties the ring without releasing its arrays, so a restarted capture
// reuses the memory rather than allocating a second window.
func (r *packetRing) Reset() {
	for i := range r.packets {
		r.packets[i] = nil
		r.raw[i] = nil
	}
	r.first, r.count = 1, 0
}
