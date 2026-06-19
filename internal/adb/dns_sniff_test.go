package adb

import "testing"

func TestParseTcpdumpDNSQuery(t *testing.T) {
	line := "2026-05-22 12:00:01.234567 IP 10.0.0.5.54321 > 8.8.8.8.53: 12345+ A? example.com. (28)"
	ev, ok := parseTcpdumpDNSLine(line)
	if !ok {
		t.Fatal("expected parse")
	}
	if ev.IsReply {
		t.Errorf("expected query, got reply")
	}
	if ev.QType != "A" || ev.Domain != "example.com" || ev.ID != 12345 {
		t.Errorf("bad parse: %+v", ev)
	}
}

func TestParseTcpdumpDNSReplyA(t *testing.T) {
	line := "2026-05-22 12:00:01.300000 IP 8.8.8.8.53 > 10.0.0.5.54321: 12345 1/0/0 A 93.184.216.34 (44)"
	ev, ok := parseTcpdumpDNSLine(line)
	if !ok {
		t.Fatal("expected parse")
	}
	if !ev.IsReply {
		t.Errorf("expected reply")
	}
	if ev.QType != "A" || len(ev.Answers) != 1 || ev.Answers[0] != "93.184.216.34" {
		t.Errorf("bad parse: %+v", ev)
	}
}

func TestParseTcpdumpDNSReplyNXDomain(t *testing.T) {
	line := "2026-05-22 12:00:01.300000 IP 8.8.8.8.53 > 10.0.0.5.54321: 12345 NXDomain 0/1/0 (90)"
	ev, ok := parseTcpdumpDNSLine(line)
	if !ok {
		t.Fatal("expected parse")
	}
	if !ev.IsReply || len(ev.Answers) != 1 || ev.Answers[0] != "NXDOMAIN" {
		t.Errorf("bad parse: %+v", ev)
	}
}

func TestParseTcpdumpDNSAAAA(t *testing.T) {
	line := "2026-05-22 12:00:01.234567 IP6 fe80::1.54321 > fe80::2.53: 99+ AAAA? ipv6.google.com. (33)"
	ev, ok := parseTcpdumpDNSLine(line)
	if !ok {
		t.Fatal("expected parse")
	}
	if ev.QType != "AAAA" || ev.Domain != "ipv6.google.com" {
		t.Errorf("bad parse: %+v", ev)
	}
}

func TestParseTcpdumpDNSNoMatch(t *testing.T) {
	if _, ok := parseTcpdumpDNSLine("garbage line"); ok {
		t.Error("expected no match")
	}
}
