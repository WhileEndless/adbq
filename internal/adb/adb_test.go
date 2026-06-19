package adb

import (
	"context"
	"testing"
)

func TestRootWrapStyles(t *testing.T) {
	cases := []struct {
		name  string
		style suStyle
		inner string
		want  string
	}{
		{"bare-root runs unwrapped", suBareRoot, "iptables -L", "iptables -L"},
		{"magisk simple", suSimple, "id", "su -c " + shQuote("id")},
		{"aosp sh-wrap", suShWrap, "id", "su -c sh -c " + shQuote("id")},
		{"uid-positional simple", suZeroSimple, "id", "su 0 -c " + shQuote("id")},
		{"uid-positional sh-wrap", suZeroShWrap, "id", "su 0 sh -c " + shQuote("id")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient()
			// Pre-seed the per-serial cache so rootWrap resolves the style
			// without probing a device.
			c.setSuStyle("dev", tc.style)
			got, err := c.rootWrap(context.Background(), "dev", tc.inner)
			if err != nil {
				t.Fatalf("rootWrap: %v", err)
			}
			if got != tc.want {
				t.Errorf("style %d: got %q, want %q", tc.style, got, tc.want)
			}
		})
	}
}

func TestRootWrapUnknownFailsClosed(t *testing.T) {
	// A cached suUnknown forces a re-probe; with no device the probe errors,
	// so rootWrap must return an error rather than an empty/half command.
	c := NewClient()
	c.SetBinary("/nonexistent/adb-binary")
	if _, err := c.rootWrap(context.Background(), "dev", "id"); err == nil {
		t.Fatal("expected error when root style is unresolved")
	}
}

func TestHasUID0(t *testing.T) {
	root := []string{
		"uid=0(root) gid=0(root) groups=0(root)",
		"uid=0 gid=0",
		"uid=0",
		"  uid=0(root)\n",
	}
	notRoot := []string{
		"uid=2000(shell) gid=2000(shell) groups=2000(shell),3009(readproc)",
		"uid=1000(system) gid=1000(system) groups=1000(system),0(root)", // gid list contains 0 — must NOT match
		"uid=10234(u0_a234)",
		"",
		"su: not found",
	}
	for _, s := range root {
		if !hasUID0(s) {
			t.Errorf("hasUID0(%q) = false, want true", s)
		}
	}
	for _, s := range notRoot {
		if hasUID0(s) {
			t.Errorf("hasUID0(%q) = true, want false", s)
		}
	}
}
