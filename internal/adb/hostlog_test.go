package adb

import (
	"strconv"
	"strings"
	"testing"
)

func TestHostLogSinceReturnsOnlyNewLines(t *testing.T) {
	l := newHostLog()
	l.Append("one", false)
	l.Append("two", true)

	all := l.Since(0)
	if len(all) != 2 || all[0].Text != "one" || !all[1].Err {
		t.Fatalf("Since(0) = %+v", all)
	}
	rest := l.Since(all[0].Seq)
	if len(rest) != 1 || rest[0].Text != "two" {
		t.Errorf("Since(seq) must skip what the caller already has: %+v", rest)
	}
	if got := l.Since(all[1].Seq); len(got) != 0 {
		t.Errorf("a caught-up cursor must return nothing, got %+v", got)
	}
}

// A long-running headless emulator must not grow adbq's memory without bound.
func TestHostLogDropsOldestPastTheLineCap(t *testing.T) {
	l := newHostLog()
	for i := 0; i < hostLogMaxLines+500; i++ {
		l.Append("line "+strconv.Itoa(i), false)
	}
	held := l.Since(0)
	if len(held) != hostLogMaxLines {
		t.Fatalf("held %d lines, want the cap %d", len(held), hostLogMaxLines)
	}
	if held[0].Text != "line 500" {
		t.Errorf("oldest retained line = %q, want the 500th", held[0].Text)
	}
}

func TestHostLogRespectsTheByteCap(t *testing.T) {
	l := newHostLog()
	big := strings.Repeat("x", 8*1024)
	for i := 0; i < 100; i++ {
		l.Append(big, false)
	}
	if l.bytes > hostLogMaxBytes+len(big) {
		t.Errorf("retained %d bytes, want roughly the %d cap", l.bytes, hostLogMaxBytes)
	}
	if len(l.Since(0)) == 0 {
		t.Error("the byte cap must never empty the buffer completely")
	}
}

func TestHostLogLastMeaningfulSkipsBlankTail(t *testing.T) {
	l := newHostLog()
	l.Append("emulator: ERROR: x86 emulation requires hardware acceleration", true)
	l.Append("", false)
	l.Append("   ", false)
	if got := l.LastMeaningful(); !strings.Contains(got, "hardware acceleration") {
		t.Errorf("LastMeaningful = %q", got)
	}
	if got := newHostLog().LastMeaningful(); got != "" {
		t.Errorf("an empty log must yield an empty string, got %q", got)
	}
}

// Clear must not rewind the sequence: a frontend still holding an old cursor
// would otherwise be handed lines it has already rendered.
func TestHostLogClearKeepsSequenceMonotonic(t *testing.T) {
	l := newHostLog()
	l.Append("a", false)
	before := l.Since(0)[0].Seq
	l.Clear()
	l.Append("b", false)
	after := l.Since(0)
	if len(after) != 1 || after[0].Seq <= before {
		t.Errorf("sequence must keep climbing across Clear: before=%d after=%+v", before, after)
	}
}

func TestHostLogTrimsLineEndings(t *testing.T) {
	l := newHostLog()
	l.Append("windows line\r\n", false)
	if got := l.Since(0)[0].Text; got != "windows line" {
		t.Errorf("text = %q, want trailing CRLF stripped", got)
	}
}
