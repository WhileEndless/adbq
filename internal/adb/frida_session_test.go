package adb

import (
	"sync"
	"testing"
)

func TestParseFridaMsg(t *testing.T) {
	cases := []struct {
		line       string
		wantKind   string
		wantPayl   string
		wantLevel  string
		wantDetail string
		wantOK     bool
	}{
		{`{"type":"log","script":"s0","level":"info","payload":"hello"}`, "log", "hello", "info", "", true},
		{`{"type":"log","level":"warning","payload":"careful"}`, "log", "careful", "warning", "", true},
		// send with an object payload → compact JSON string.
		{`{"type":"send","script":"s0","payload":{"a":1}}`, "send", `{"a":1}`, "", "", true},
		// send with a string payload → unquoted.
		{`{"type":"send","payload":"plain"}`, "send", "plain", "", "", true},
		{`{"type":"error","payload":"ReferenceError: x","stack":"at <anonymous>"}`, "error", "ReferenceError: x", "", "", true},
		{`{"type":"loaded","script":"s0"}`, "loaded", "", "", "", true},
		{`{"type":"resumed","pid":1234}`, "resumed", "", "", "", true},
		{`{"type":"detached","reason":"process-terminated"}`, "detached", "", "", "process-terminated", true},
		{`{"type":"fatal","error":"version-mismatch","detail":"major version"}`, "fatal", "version-mismatch", "", "major version", true},
		{`{"type":"ready","driverProto":1,"fridaVersion":"16.4.7"}`, "ready", "", "", "", true},
		{`{"type":"status","stage":"running"}`, "status", "", "", "running", true},
		// malformed / non-JSON / missing type → not ok.
		{`not json at all`, "", "", "", "", false},
		{`{"no_type":true}`, "", "", "", "", false},
		{``, "", "", "", "", false},
	}
	for _, c := range cases {
		m, ok := parseFridaMsg(c.line)
		if ok != c.wantOK {
			t.Errorf("%s: ok=%v want %v", c.line, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if m.Kind != c.wantKind || m.Payload != c.wantPayl || m.Level != c.wantLevel || m.Detail != c.wantDetail {
			t.Errorf("%s: got kind=%q payload=%q level=%q detail=%q",
				c.line, m.Kind, m.Payload, m.Level, m.Detail)
		}
	}
}

// A chatty script used to outrun the delivery channel and have its lines
// dropped on the floor. Draining the ring by seq must hand every message to a
// slow consumer exactly once instead.
func TestFridaLogSinceLosesNothingUnderLoad(t *testing.T) {
	s := &FridaSession{done: make(chan struct{})}
	const total = fridaSessionRing / 2

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < total; i++ {
			s.ingest(FridaMsg{Kind: "log", Payload: "x"})
		}
		close(s.done)
	}()

	seen, last := 0, 0
	drain := func() {
		for _, m := range s.LogSince(last) {
			if m.Seq != last+1 {
				t.Errorf("seq gap: got %d after %d", m.Seq, last)
			}
			last = m.Seq
			seen++
		}
	}
	for done := false; !done; {
		select {
		case <-s.done:
			done = true
		default:
		}
		drain()
	}
	wg.Wait()
	drain()

	if seen != total {
		t.Fatalf("delivered %d of %d messages", seen, total)
	}
}

func TestFridaMsgRingAndSeq(t *testing.T) {
	s := &FridaSession{done: make(chan struct{})}
	for i := 0; i < fridaSessionRing+50; i++ {
		s.ingest(FridaMsg{Kind: "log", Payload: "x"})
	}
	// Ring is bounded.
	all := s.LogSince(0)
	if len(all) != fridaSessionRing {
		t.Fatalf("ring size: got %d want %d", len(all), fridaSessionRing)
	}
	// Seq is monotonic and the oldest retained reflects the trim.
	if all[0].Seq != 51 {
		t.Fatalf("oldest retained seq: got %d want 51", all[0].Seq)
	}
	if all[len(all)-1].Seq != fridaSessionRing+50 {
		t.Fatalf("newest seq: got %d", all[len(all)-1].Seq)
	}
	// Backfill since a seq returns only newer entries.
	since := s.LogSince(fridaSessionRing + 45)
	if len(since) != 5 {
		t.Fatalf("LogSince tail: got %d want 5", len(since))
	}
}

func TestFatalNote(t *testing.T) {
	if note := fatalNote(FridaMsg{Kind: "fatal", Payload: "version-mismatch", Detail: "major version mismatch"}); note == "" {
		t.Fatal("expected a non-empty version-mismatch note")
	}
	if note := fatalNote(FridaMsg{Kind: "fatal", Detail: "could not reach device"}); note != "could not reach device" {
		t.Fatalf("detail note: %q", note)
	}
}
