package adb

import (
	"strconv"
	"strings"
)

// This file collects small, device-agnostic string helpers shared across the
// device-output parsers (logcat, ls, network, iptables, …). They live here
// rather than next to their first caller so the next parser can find them
// instead of re-deriving "first line of output".

// firstLine returns the first non-empty line of s, trimmed.
func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" {
			return ln
		}
	}
	return ""
}

// firstNLines returns at most the first n lines of s joined by newlines.
func firstNLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// firstField returns the first whitespace-separated field of s, or "".
func firstField(s string) string {
	if fs := strings.Fields(s); len(fs) > 0 {
		return fs[0]
	}
	return ""
}

// isAllDigits reports whether s is non-empty and all ASCII digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// isDottedIPv4 reports whether s is a dotted-quad IPv4 address.
func isDottedIPv4(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 3 || !isAllDigits(p) {
			return false
		}
		if n, _ := strconv.Atoi(p); n > 255 {
			return false
		}
	}
	return true
}

// skipFields returns the remainder of line after the first n whitespace-
// separated fields, keeping the remainder's own internal spacing intact.
func skipFields(line string, n int) string {
	s := line
	for range n {
		s = strings.TrimLeft(s, " \t")
		i := strings.IndexAny(s, " \t")
		if i < 0 {
			return ""
		}
		s = s[i:]
	}
	return strings.TrimLeft(s, " \t")
}
