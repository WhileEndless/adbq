package adb

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DNSEvent is a single parsed DNS query or reply line from tcpdump.
type DNSEvent struct {
	Time    int64    `json:"time"`    // unix millis
	Source  string   `json:"source"`  // "tcpdump" or "logcat"
	Src     string   `json:"src"`     // ip:port (query: client; reply: server)
	Dst     string   `json:"dst"`     // ip:port
	QType   string   `json:"qtype"`   // A, AAAA, PTR, CNAME, MX, ...
	Domain  string   `json:"domain"`  // example.com
	Answers []string `json:"answers"` // reply IPs (or "NXDOMAIN")
	ID      int      `json:"id"`      // DNS transaction id
	IsReply bool     `json:"isReply"`
	Raw     string   `json:"raw"` // original line for debugging / unparsed cases
}

// DNSSnifferStream wraps a long-running tcpdump (or logcat fallback) process
// that emits parsed DNS events. The lifecycle mirrors LogcatStream.
type DNSSnifferStream struct {
	cmd      *exec.Cmd
	stdout   io.ReadCloser
	out      chan DNSEvent
	stopOnce sync.Once
	done     chan struct{}
	Source   string // "tcpdump" or "logcat"
}

func (s *DNSSnifferStream) Events() <-chan DNSEvent { return s.out }

func (s *DNSSnifferStream) Stop() {
	s.stopOnce.Do(func() {
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
	})
	<-s.done
}

// StartDNSSniffer starts the best available DNS event source on the device.
// Prefers tcpdump on udp/53 (full visibility, root-only). Falls back to a
// filtered logcat subscription on netd/DnsResolver tags when tcpdump is
// missing — limited but rootless.
func (c *Client) StartDNSSniffer(ctx context.Context, serial string) (*DNSSnifferStream, error) {
	// Try tcpdump path first.
	if td, err := c.ProbeTcpdump(ctx, serial); err == nil && td != nil && td.Available {
		return c.startTcpdumpDNS(ctx, serial, td.Path)
	}
	return c.startLogcatDNS(ctx, serial)
}

func (c *Client) startTcpdumpDNS(ctx context.Context, serial, tcpdumpPath string) (*DNSSnifferStream, error) {
	bin, err := c.Binary()
	if err != nil {
		return nil, err
	}
	// `-l` is critical — without it tcpdump block-buffers and the UI sees
	// nothing for minutes. Route through rootWrap so the correct su form (or
	// bare uid-0) is used, matching the live-capture path; a hardcoded `su -c`
	// silently produced zero events on AOSP-style su devices.
	inner := tcpdumpPath + " -i any -l -n -tttt -s 0 'udp port 53'"
	wrapped, err := c.rootWrap(ctx, serial, inner)
	if err != nil {
		return nil, err
	}
	args := []string{"-s", serial, "shell", wrapped}
	cmd := exec.CommandContext(ctx, bin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("tcpdump spawn failed: %w", err)
	}
	s := &DNSSnifferStream{
		cmd:    cmd,
		stdout: stdout,
		out:    make(chan DNSEvent, 256),
		done:   make(chan struct{}),
		Source: "tcpdump",
	}
	go s.pumpTcpdump()
	return s, nil
}

func (s *DNSSnifferStream) pumpTcpdump() {
	defer close(s.out)
	defer close(s.done)
	sc := LineScanner(s.stdout)
	for sc.Scan() {
		line := sc.Text()
		if ev, ok := parseTcpdumpDNSLine(line); ok {
			ev.Source = "tcpdump"
			s.out <- ev
		}
	}
	_ = s.cmd.Wait()
}

func (c *Client) startLogcatDNS(ctx context.Context, serial string) (*DNSSnifferStream, error) {
	bin, err := c.Binary()
	if err != nil {
		return nil, err
	}
	// Subscribe to known DNS tags. We use *:S to silence everything else.
	args := []string{
		"-s", serial, "logcat", "-v", "threadtime",
		"netd:V", "DnsResolver:V", "DnsProxyListener:V", "PrivateDnsConfiguration:V", "*:S",
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("logcat spawn failed: %w", err)
	}
	s := &DNSSnifferStream{
		cmd:    cmd,
		stdout: stdout,
		out:    make(chan DNSEvent, 256),
		done:   make(chan struct{}),
		Source: "logcat",
	}
	go s.pumpLogcat()
	return s, nil
}

func (s *DNSSnifferStream) pumpLogcat() {
	defer close(s.out)
	defer close(s.done)
	sc := LineScanner(s.stdout)
	for sc.Scan() {
		line := sc.Text()
		if ev, ok := parseLogcatDNSLine(line); ok {
			ev.Source = "logcat"
			s.out <- ev
		}
	}
	_ = s.cmd.Wait()
}

// ─── parsers ───────────────────────────────────────────────────────────────

// tcpdump -tttt -n output examples we recognise:
//
// Query:
//
//	2026-05-22 12:00:01.234567 IP 10.0.0.5.54321 > 8.8.8.8.53: 12345+ A? example.com. (28)
//	2026-05-22 12:00:01.234567 IP6 ::1.54321 > ::1.53: 12345+ AAAA? example.com. (28)
//
// Reply:
//
//	2026-05-22 12:00:01.300000 IP 8.8.8.8.53 > 10.0.0.5.54321: 12345 1/0/0 A 93.184.216.34 (44)
//	2026-05-22 12:00:01.300000 IP 8.8.8.8.53 > 10.0.0.5.54321: 12345 NXDomain 0/1/0 (90)
var (
	tcpdumpHead = regexp.MustCompile(`^\s*(\d{4}-\d{2}-\d{2})\s+(\d{2}:\d{2}:\d{2}\.\d+)\s+IP6?\s+(\S+)\s+>\s+(\S+):\s+(.+)$`)
	queryRe     = regexp.MustCompile(`^(\d+)\+?\s+(?:\[?[A-Z0-9]+\]?\s+)*([A-Z0-9]+)\?\s+(\S+?)\.?\s*\(?\d*\)?$`)
	replyHead   = regexp.MustCompile(`^(\d+)[\*\+\-]?\s+(.*)$`)
	answerARe   = regexp.MustCompile(`\b(A|AAAA|CNAME|PTR|MX|NS|TXT|SRV)\s+(\S+?)\.?(?:\s|,|$)`)
)

func parseTcpdumpDNSLine(line string) (DNSEvent, bool) {
	m := tcpdumpHead.FindStringSubmatch(line)
	if m == nil {
		return DNSEvent{}, false
	}
	date, tm, src, dst, body := m[1], m[2], m[3], m[4], strings.TrimSpace(m[5])
	ev := DNSEvent{
		Time: parseTcpdumpTime(date, tm),
		Src:  src,
		Dst:  dst,
		Raw:  line,
	}
	// First token of body is the txid; second decides query vs reply.
	rh := replyHead.FindStringSubmatch(body)
	if rh == nil {
		return DNSEvent{}, false
	}
	id, _ := strconv.Atoi(rh[1])
	ev.ID = id
	rest := strings.TrimSpace(rh[2])

	// Query form: "A? example.com. (28)" — qtype ends with '?'.
	if q := queryRe.FindStringSubmatch(rh[0]); q != nil {
		// q[1]=id q[2]=qtype q[3]=domain
		ev.QType = q[2]
		ev.Domain = strings.TrimSuffix(q[3], ".")
		ev.IsReply = false
		// Convention: tcpdump prints client port high (>1024), server port 53.
		// If src ends in ".53", flip — but the regex sets src/dst as-is.
		if strings.HasSuffix(src, ".53") {
			ev.IsReply = true
			ev.Src, ev.Dst = dst, src
		}
		return ev, true
	}

	// Reply form. NXDomain / ServFail are special.
	ev.IsReply = true
	low := strings.ToLower(rest)
	switch {
	case strings.Contains(low, "nxdomain"):
		ev.Answers = []string{"NXDOMAIN"}
	case strings.Contains(low, "servfail"):
		ev.Answers = []string{"SERVFAIL"}
	case strings.Contains(low, "refused"):
		ev.Answers = []string{"REFUSED"}
	}
	// Extract answer RRs: "1/0/0 A 1.2.3.4, A 1.2.3.5" or "1/0/0 AAAA ::1"
	if ms := answerARe.FindAllStringSubmatch(rest, -1); ms != nil {
		for _, am := range ms {
			rrType := am[1]
			rrData := strings.TrimSuffix(am[2], ".")
			if rrType == "A" || rrType == "AAAA" || rrType == "CNAME" || rrType == "PTR" {
				if ev.QType == "" {
					ev.QType = rrType
				}
				ev.Answers = append(ev.Answers, rrData)
			}
		}
	}
	if ev.QType == "" {
		ev.QType = "?"
	}
	return ev, true
}

func parseTcpdumpTime(date, tm string) int64 {
	t, err := time.Parse("2006-01-02 15:04:05.000000", date+" "+tm)
	if err != nil {
		// Some tcpdump builds emit fewer fractional digits; try a forgiving parse.
		if t2, err2 := time.Parse("2006-01-02 15:04:05.999999", date+" "+tm); err2 == nil {
			return t2.UnixMilli()
		}
		return time.Now().UnixMilli()
	}
	return t.UnixMilli()
}

// parseLogcatDNSLine extracts a DNS event from one of:
//   - netd     "DnsResolver: lookup id=42 example.com type=A"
//   - DnsResolver "resolv_gethostbyname(...) returned ..."
//
// The exact format varies wildly across Android versions; we keep this best
// effort and surface the raw line so the user can still see something useful.
func parseLogcatDNSLine(line string) (DNSEvent, bool) {
	low := strings.ToLower(line)
	if !strings.Contains(low, "dns") && !strings.Contains(low, "resolv") && !strings.Contains(low, "gethostby") {
		return DNSEvent{}, false
	}
	ev := DNSEvent{
		Time: time.Now().UnixMilli(),
		Raw:  line,
	}
	// Find anything that looks like a host (a.b.c) — first match.
	host := regexp.MustCompile(`([a-zA-Z0-9][a-zA-Z0-9\-]*\.)+[a-zA-Z]{2,}`).FindString(line)
	if host != "" {
		ev.Domain = host
	}
	switch {
	case strings.Contains(low, "type=aaaa"), strings.Contains(low, "aaaa "):
		ev.QType = "AAAA"
	case strings.Contains(low, "type=a "), strings.Contains(low, " a?"):
		ev.QType = "A"
	default:
		ev.QType = "?"
	}
	ev.IsReply = strings.Contains(low, "returned") || strings.Contains(low, "result")
	if ev.Domain == "" {
		return DNSEvent{}, false
	}
	return ev, true
}
