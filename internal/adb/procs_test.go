package adb

import "testing"

func TestParseProcStatLineSimple(t *testing.T) {
	in := `1 (init) S 0 0 0 0 -1 4194560 707 29676 0 51 2 7 13 10 20 0 1 0 0 2904064 232 18446744073709551615 4194304 4655752`
	p, ok := parseProcStatLine(in)
	if !ok {
		t.Fatal("expected parse")
	}
	if p.pid != 1 {
		t.Errorf("pid: got %d want 1", p.pid)
	}
	if p.comm != "init" {
		t.Errorf("comm: got %q want init", p.comm)
	}
	if p.state != "S" {
		t.Errorf("state: got %q want S", p.state)
	}
	if p.rssPages != 232 {
		t.Errorf("rssPages: got %d want 232", p.rssPages)
	}
	if p.vsize != 2904064 {
		t.Errorf("vsize: got %d want 2904064", p.vsize)
	}
	if p.utime != 2 || p.stime != 7 {
		t.Errorf("utime/stime: got %d/%d want 2/7", p.utime, p.stime)
	}
}

// comm may contain spaces AND parentheses; we must use the first '(' and the
// last ')' as delimiters, not naive whitespace splitting.
func TestParseProcStatLineCommWithParens(t *testing.T) {
	in := `42 (Binder:1_2 (x)) S 1 0 0 0 -1 4194560 0 0 0 0 11 22 0 0 20 0 1 0 0 123456 99 0 0 0`
	p, ok := parseProcStatLine(in)
	if !ok {
		t.Fatal("expected parse")
	}
	if p.pid != 42 {
		t.Errorf("pid: got %d want 42", p.pid)
	}
	if p.comm != "Binder:1_2 (x)" {
		t.Errorf("comm: got %q want %q", p.comm, "Binder:1_2 (x)")
	}
	if p.state != "S" {
		t.Errorf("state: got %q want S", p.state)
	}
	if p.utime != 11 || p.stime != 22 {
		t.Errorf("utime/stime: got %d/%d want 11/22", p.utime, p.stime)
	}
	if p.vsize != 123456 || p.rssPages != 99 {
		t.Errorf("vsize/rss: got %d/%d want 123456/99", p.vsize, p.rssPages)
	}
}

func TestParseProcStatLineBad(t *testing.T) {
	if _, ok := parseProcStatLine("garbage no parens"); ok {
		t.Error("expected no parse for malformed line")
	}
	if _, ok := parseProcStatLine("1 (init) S 0 0 0"); ok {
		t.Error("expected no parse for too-few fields")
	}
}

func TestParseProcStat(t *testing.T) {
	in := `cpu  1216 111 1200 323996 304 0 192 0 0 0
cpu0 600 50 600 161000 150 0 96 0 0 0
cpu1 616 61 600 162996 154 0 96 0 0 0
intr 12345`
	total, ncpu := parseProcStat(in)
	// 1216+111+1200+323996+304+0+192+0+0+0
	if total != 327019 {
		t.Errorf("totalJiffies: got %d want 327019", total)
	}
	if ncpu != 2 {
		t.Errorf("ncpu: got %d want 2", ncpu)
	}
}

func TestParseMemTotalKB(t *testing.T) {
	in := `MemTotal:        1024508 kB
MemFree:          580952 kB`
	if got := parseMemTotalKB(in); got != 1024508 {
		t.Errorf("MemTotal: got %d want 1024508", got)
	}
}

func TestSplitProcfsSections(t *testing.T) {
	in := "cpu 1 2 3\n@@@\n1 (init) S 0\n@@@\nMemTotal: 100 kB\n"
	a, b, c := splitProcfsSections(in)
	if a != "cpu 1 2 3" {
		t.Errorf("stat section: %q", a)
	}
	if b != "1 (init) S 0" {
		t.Errorf("proc section: %q", b)
	}
	if c != "MemTotal: 100 kB" {
		t.Errorf("mem section: %q", c)
	}
}
