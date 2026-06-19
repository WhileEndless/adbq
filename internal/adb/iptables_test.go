package adb

import "testing"

const iptablesLegacyFixture = `Chain INPUT (policy ACCEPT 12 packets, 1024 bytes)
num   pkts bytes target     prot opt in     out     source               destination
1        5   320 ACCEPT     all  --  lo     *       0.0.0.0/0            0.0.0.0/0
2        0     0 DROP       tcp  --  *      *       0.0.0.0/0            0.0.0.0/0            tcp dpt:9999

Chain FORWARD (policy DROP 0 packets, 0 bytes)
num   pkts bytes target     prot opt in     out     source               destination

Chain OUTPUT (policy ACCEPT 8 packets, 512 bytes)
num   pkts bytes target     prot opt in     out     source               destination
1        2   128 ACCEPT     udp  --  *      *       0.0.0.0/0            8.8.8.8              udp dpt:53

Chain my_chain (0 references)
num   pkts bytes target     prot opt in     out     source               destination
`

func TestParseIptablesL(t *testing.T) {
	chains := parseIptablesL(iptablesLegacyFixture)
	if len(chains) != 4 {
		t.Fatalf("want 4 chains, got %d", len(chains))
	}
	in := chains[0]
	if in.Name != "INPUT" || in.Policy != "ACCEPT" {
		t.Errorf("INPUT name/policy = %q/%q", in.Name, in.Policy)
	}
	if len(in.Rules) != 2 {
		t.Fatalf("INPUT rules = %d", len(in.Rules))
	}
	r := in.Rules[1]
	if r.Num != 2 || r.Target != "DROP" || r.Proto != "tcp" || r.Extra != "tcp dpt:9999" {
		t.Errorf("rule[1] = %+v", r)
	}
	fwd := chains[1]
	if fwd.Policy != "DROP" || len(fwd.Rules) != 0 {
		t.Errorf("FORWARD = %+v", fwd)
	}
	user := chains[3]
	if user.Name != "my_chain" || user.Policy != "-" {
		t.Errorf("user chain = %+v", user)
	}
}

func TestIptablesSpecValidate(t *testing.T) {
	if err := (IptablesSpec{}).validate(); err == nil {
		t.Errorf("empty spec should fail")
	}
	if err := (IptablesSpec{"-p", "tcp", "--dport", "443", "-j", "ACCEPT"}).validate(); err != nil {
		t.Errorf("valid spec failed: %v", err)
	}
	bad := IptablesSpec{"-p", "tcp; rm -rf /"}
	if err := bad.validate(); err == nil {
		t.Errorf("spec with shell metacharacters should fail")
	}
	if err := (IptablesSpec{"-m", "comment", "--comment", "hi\nthere"}).validate(); err == nil {
		t.Errorf("spec with newline should fail")
	}
}

func TestLooksLikeIdent(t *testing.T) {
	good := []string{"INPUT", "my_chain", "Chain-1"}
	bad := []string{"", "with space", "semi;colon", "tab\there"}
	for _, s := range good {
		if !looksLikeIdent(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range bad {
		if looksLikeIdent(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestTcpdumpManifestPinned(t *testing.T) {
	// Refuse to ship a manifest that's missing required fields. Catches the
	// "forgot to fill SHA256 after bumping the pin" footgun before release.
	for _, b := range tcpdumpManifest {
		if b.Abi == "" {
			t.Errorf("manifest entry with empty Abi")
		}
		if len(b.SHA256) != 64 {
			t.Errorf("ABI %q has malformed SHA256 (%d chars, want 64)", b.Abi, len(b.SHA256))
		}
		if b.URL == "" || b.Size <= 0 {
			t.Errorf("ABI %q missing URL or Size", b.Abi)
		}
	}
}
