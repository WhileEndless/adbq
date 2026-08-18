package adb

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// The adb server already knows the instant a device appears or goes away, and
// it will say so if asked: `host:track-devices` holds the connection open and
// writes a fresh device list on every change. Polling `adb devices -l` on a
// timer asks the same question over and over, forks a process each time, and
// still reports the answer up to one interval late.
//
// This is the push side of that. It speaks the adb host protocol directly
// because there is no CLI surface for it — stdlib only, no new dependency.
//
// Polling remains as a fallback and is not optional: the tracker is a long-lived
// socket to a server the user can restart or kill from inside this very app, and
// a device list that silently stops updating is worse than one that updates
// slowly. See Tracker.

// adbServerAddr is where the adb server listens. adb itself honours ANDROID_ADB_SERVER_PORT;
// callers that need that can pass an address to NewTracker.
const adbServerAddr = "127.0.0.1:5037"

// trackRequest asks for the long form, which carries the same `product:`,
// `model:` and `transport_id:` fields as `adb devices -l`. It landed in adb 33;
// older servers reject it and we fall back to the short form, which gives only
// serial and state — enough to detect a change and trigger one enrichment.
const (
	trackRequestLong  = "host:track-devices-l"
	trackRequestShort = "host:track-devices"
)

// TrackerState describes what the tracker is currently doing, for the UI to
// report. A fallback that nobody can see is a fallback nobody debugs.
type TrackerState struct {
	// Connected is true while a tracking socket is live.
	Connected bool `json:"connected"`
	// LongForm is true when the server accepted track-devices-l, so the list
	// carries model/product without a follow-up call.
	LongForm bool `json:"longForm"`
	// LastError is the most recent connection failure, or "".
	LastError string `json:"lastError"`
}

// Tracker maintains a push subscription to the adb server's device list.
//
// Updates() emits a parsed list on every change. The channel is closed only
// when the tracker is stopped; a dropped connection is retried rather than
// reported as the end, because the adb server going away (`adb kill-server`,
// an upgrade, a crash) is a normal thing that resolves itself.
type Tracker struct {
	client *Client
	addr   string

	out    chan []Device
	cancel context.CancelFunc
	done   chan struct{}

	mu    sync.Mutex
	state TrackerState
}

// Updates yields the device list each time the adb server reports a change.
func (t *Tracker) Updates() <-chan []Device { return t.out }

// State reports whether the push connection is currently live.
func (t *Tracker) State() TrackerState {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.state
}

func (t *Tracker) setState(f func(*TrackerState)) {
	t.mu.Lock()
	f(&t.state)
	connected := t.state.Connected
	t.mu.Unlock()
	SetTrackingDevices(connected)
}

// Stop ends the subscription and closes Updates().
func (t *Tracker) Stop() {
	if t.cancel != nil {
		t.cancel()
	}
	<-t.done
}

// StartTracker begins following the adb server's device list.
//
// It never returns an error: a server that is not up yet is the normal state at
// launch, and the retry loop handles it. Callers watch State() to know whether
// push is working and whether they still need to poll.
func (c *Client) StartTracker(ctx context.Context) *Tracker {
	cctx, cancel := context.WithCancel(ctx)
	t := &Tracker{
		client: c,
		addr:   adbServerAddr,
		out:    make(chan []Device, 4),
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go t.loop(cctx)
	return t
}

// trackBackoff is the reconnect schedule. It starts fast — `adb kill-server`
// followed by any command restarts the server within a second — and settles at
// a few seconds so a machine with no adb server at all is not spun against.
var trackBackoff = []time.Duration{
	250 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
}

func (t *Tracker) loop(ctx context.Context) {
	defer close(t.done)
	defer close(t.out)
	defer t.setState(func(s *TrackerState) { s.Connected = false })

	attempt := 0
	for ctx.Err() == nil {
		err := t.session(ctx)
		if ctx.Err() != nil {
			return
		}
		// A session that carried at least one update was healthy; reset the
		// backoff so a long-running connection that drops once reconnects
		// immediately rather than at the tail of the schedule.
		if err == nil {
			attempt = 0
		}
		t.setState(func(s *TrackerState) {
			s.Connected = false
			if err != nil {
				s.LastError = err.Error()
			}
		})
		delay := trackBackoff[min(attempt, len(trackBackoff)-1)]
		attempt++
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// session holds one tracking connection until it drops. It returns nil when the
// connection was established and delivered at least one list, so the caller can
// tell "the server went away mid-stream" from "the server was never there".
func (t *Tracker) session(ctx context.Context) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", t.addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	// Unblock the blocking read below when the caller stops us.
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	long := true
	if err := adbHandshake(conn, trackRequestLong); err != nil {
		// Older servers do not know the long form. Reconnect and ask for the
		// short one rather than giving up on push entirely.
		_ = conn.Close()
		conn2, err2 := d.DialContext(ctx, "tcp", t.addr)
		if err2 != nil {
			return err
		}
		defer conn2.Close()
		go func() {
			<-ctx.Done()
			_ = conn2.Close()
		}()
		if err := adbHandshake(conn2, trackRequestShort); err != nil {
			return err
		}
		conn, long = conn2, false
	}

	t.setState(func(s *TrackerState) {
		s.Connected = true
		s.LongForm = long
		s.LastError = ""
	})

	r := bufio.NewReader(conn)
	delivered := false
	for {
		payload, err := readAdbFrame(r)
		if err != nil {
			if delivered {
				return nil // healthy session that ended
			}
			return err
		}
		delivered = true
		select {
		case t.out <- ParseDeviceList(payload):
		case <-ctx.Done():
			return nil
		}
	}
}

// adbHandshake sends one host request and reads the server's OKAY/FAIL.
//
// The protocol frames everything as four hex digits of length followed by the
// payload; a FAIL is followed by a length-prefixed reason.
func adbHandshake(conn net.Conn, request string) error {
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(conn, "%04x%s", len(request), request); err != nil {
		return err
	}
	var status [4]byte
	if _, err := io.ReadFull(conn, status[:]); err != nil {
		return err
	}
	switch string(status[:]) {
	case "OKAY":
		// Tracking is open-ended, so the deadline has to go — otherwise the
		// first quiet period would look like a dropped connection.
		return conn.SetDeadline(time.Time{})
	case "FAIL":
		reason, err := readAdbFrameFrom(conn)
		if err != nil {
			return fmt.Errorf("adb refused %q", request)
		}
		return fmt.Errorf("adb refused %q: %s", request, strings.TrimSpace(reason))
	default:
		return fmt.Errorf("adb spoke something other than the host protocol (%q)", status[:])
	}
}

// readAdbFrame reads one length-prefixed payload. A zero length is legal and
// means "no devices attached" — an empty list, not an error.
func readAdbFrame(r *bufio.Reader) (string, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return "", err
	}
	n, err := parseHexLen(hdr[:])
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func readAdbFrameFrom(conn net.Conn) (string, error) {
	return readAdbFrame(bufio.NewReader(conn))
}

// parseHexLen decodes the protocol's four-hex-digit length prefix.
func parseHexLen(b []byte) (int, error) {
	if len(b) != 4 {
		return 0, fmt.Errorf("short length prefix")
	}
	dst := make([]byte, 2)
	if _, err := hex.Decode(dst, b); err != nil {
		return 0, fmt.Errorf("bad length prefix %q: %w", b, err)
	}
	return int(dst[0])<<8 | int(dst[1]), nil
}
