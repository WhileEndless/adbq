package adb

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestParseHexLen(t *testing.T) {
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"0000", 0, false},
		{"0012", 18, false},
		{"00ff", 255, false},
		{"FFFF", 65535, false},
		{"ffff", 65535, false},
		{"zzzz", 0, true},
		{"01", 0, true},
	}
	for _, tc := range tests {
		got, err := parseHexLen([]byte(tc.in))
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseHexLen(%q) = %d, want error", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("parseHexLen(%q) = (%d, %v), want %d", tc.in, got, err, tc.want)
		}
	}
}

// One parser serves both the CLI listing and the push stream. If they drifted,
// the UI would show two different versions of the same device depending on
// which path last delivered.
func TestParseDeviceListHandlesBothSources(t *testing.T) {
	// What `adb devices -l` prints: a banner, then rows.
	cli := "List of devices attached\n" +
		"ABC123             device usb:1-1 product:foo model:Some_Phone device:bar transport_id:1\n" +
		"emulator-5554      device product:sdk_gphone model:Emu transport_id:2\n" +
		"192.168.1.20:5555  offline transport_id:3\n"
	// What host:track-devices-l streams: the same rows, no banner.
	stream := strings.TrimPrefix(cli, "List of devices attached\n")

	fromCLI := ParseDeviceList(cli)
	fromStream := ParseDeviceList(stream)

	if len(fromCLI) != 3 {
		t.Fatalf("CLI parse gave %d devices, want 3: %+v", len(fromCLI), fromCLI)
	}
	if len(fromStream) != len(fromCLI) {
		t.Fatalf("stream parse gave %d devices, CLI gave %d", len(fromStream), len(fromCLI))
	}
	for i := range fromCLI {
		if fromCLI[i] != fromStream[i] {
			t.Errorf("device %d differs between sources:\n cli:    %+v\n stream: %+v",
				i, fromCLI[i], fromStream[i])
		}
	}

	usb := fromCLI[0]
	if usb.Via != "USB" || !usb.Online || usb.Model != "Some Phone" || usb.Transport != "transport_id:1" {
		t.Errorf("usb device parsed wrong: %+v", usb)
	}
	if fromCLI[1].Via != "Emulator" {
		t.Errorf("emulator not recognised: %+v", fromCLI[1])
	}
	if fromCLI[2].Via != "Wi-Fi" || fromCLI[2].Online {
		t.Errorf("offline wifi device parsed wrong: %+v", fromCLI[2])
	}
}

func TestParseDeviceListEmptyIsNotNil(t *testing.T) {
	// A zero-length frame means "nothing attached", which must serialise as an
	// empty list rather than null — the UI maps over it.
	if got := ParseDeviceList(""); got == nil || len(got) != 0 {
		t.Errorf("ParseDeviceList(\"\") = %#v, want empty non-nil slice", got)
	}
}

// fakeADBServer speaks just enough of the host protocol to drive the tracker.
// refuseLong makes it reject track-devices-l the way a pre-33 server does.
type fakeADBServer struct {
	ln         net.Listener
	refuseLong bool
	frames     []string      // sent in order once a request is accepted
	hold       chan struct{} // closed to end the connection
}

func newFakeADBServer(t *testing.T, refuseLong bool, frames ...string) *fakeADBServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeADBServer{ln: ln, refuseLong: refuseLong, frames: frames, hold: make(chan struct{})}
	go f.serve()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeADBServer) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeADBServer) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	req, err := readAdbFrame(r)
	if err != nil {
		return
	}
	if f.refuseLong && req == trackRequestLong {
		conn.Write([]byte("FAIL"))
		reason := "unknown host service"
		conn.Write([]byte(hexLen(len(reason)) + reason))
		return
	}
	conn.Write([]byte("OKAY"))
	for _, fr := range f.frames {
		conn.Write([]byte(hexLen(len(fr)) + fr))
	}
	<-f.hold
}

func hexLen(n int) string {
	const digits = "0123456789abcdef"
	return string([]byte{
		digits[(n>>12)&0xf], digits[(n>>8)&0xf], digits[(n>>4)&0xf], digits[n&0xf],
	})
}

func TestTrackerDeliversUpdates(t *testing.T) {
	srv := newFakeADBServer(t, false,
		"ABC123\tdevice\n",
		"ABC123\tdevice\nDEF456\tdevice\n",
	)
	tr := &Tracker{
		client: NewClient(),
		addr:   srv.ln.Addr().String(),
		out:    make(chan []Device, 4),
		done:   make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	tr.cancel = cancel
	go tr.loop(ctx)
	defer tr.Stop()

	first := recvDevices(t, tr)
	if len(first) != 1 || first[0].ID != "ABC123" {
		t.Fatalf("first update = %+v", first)
	}
	second := recvDevices(t, tr)
	if len(second) != 2 {
		t.Fatalf("second update = %+v", second)
	}
	if st := tr.State(); !st.Connected || !st.LongForm {
		t.Errorf("state = %+v, want connected on the long form", st)
	}
}

// A server too old for track-devices-l must not disable push altogether.
func TestTrackerFallsBackToShortForm(t *testing.T) {
	srv := newFakeADBServer(t, true, "ABC123\tdevice\n")
	tr := &Tracker{
		client: NewClient(),
		addr:   srv.ln.Addr().String(),
		out:    make(chan []Device, 4),
		done:   make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	tr.cancel = cancel
	go tr.loop(ctx)
	defer tr.Stop()

	got := recvDevices(t, tr)
	if len(got) != 1 || got[0].ID != "ABC123" {
		t.Fatalf("update = %+v", got)
	}
	if st := tr.State(); !st.Connected || st.LongForm {
		t.Errorf("state = %+v, want connected on the short form", st)
	}
}

// With no server at all the tracker must keep retrying quietly rather than
// spinning or dying — this is the state at launch, before adb has started.
func TestTrackerSurvivesNoServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // nothing is listening there now

	tr := &Tracker{
		client: NewClient(),
		addr:   addr,
		out:    make(chan []Device, 4),
		done:   make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	tr.cancel = cancel
	go tr.loop(ctx)

	time.Sleep(600 * time.Millisecond)
	if st := tr.State(); st.Connected {
		t.Error("reports connected with nothing listening")
	} else if st.LastError == "" {
		t.Error("no LastError recorded, so the UI could not explain the fallback")
	}
	tr.Stop() // must not hang
}

func recvDevices(t *testing.T, tr *Tracker) []Device {
	t.Helper()
	select {
	case d, ok := <-tr.Updates():
		if !ok {
			t.Fatal("Updates() closed early")
		}
		return d
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a device update")
		return nil
	}
}
