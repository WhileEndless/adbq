package adb

import "testing"

func TestParseNftRuleset(t *testing.T) {
	out := `table inet filter {
	chain input {
		type filter hook input priority filter; policy drop;
		ct state established,related accept
		iifname "lo" accept
		tcp dport 22 accept
	}
	chain output {
		type filter hook output priority filter; policy accept;
	}
}
table ip nat {
	chain prerouting {
		type nat hook prerouting priority dstnat; policy accept;
		tcp dport 80 redirect to :8080
	}
}`
	chains := parseNftRuleset(out, IPv4)
	// inet (dual) chains + ip nat chain are relevant to IPv4.
	in := findChain(chains, "input")
	if in == nil {
		t.Fatal("input chain missing")
	}
	if in.Policy != "DROP" {
		t.Errorf("input policy = %q, want DROP", in.Policy)
	}
	if len(in.Rules) != 3 {
		t.Fatalf("input rules = %d, want 3", len(in.Rules))
	}
	if in.Rules[0].Target != "ACCEPT" || in.Rules[0].Raw != "ct state established,related accept" {
		t.Errorf("rule[0] = %+v", in.Rules[0])
	}
	if pre := findChain(chains, "prerouting"); pre == nil || pre.Policy != "ACCEPT" || len(pre.Rules) != 1 {
		t.Errorf("prerouting chain = %+v", pre)
	}
}

func TestParseNftRulesetFamilyFilter(t *testing.T) {
	out := `table ip6 filter {
	chain input {
		type filter hook input priority filter; policy accept;
		tcp dport 443 accept
	}
}`
	// An IPv6-only table must not surface for an IPv4 request.
	if chains := parseNftRuleset(out, IPv4); len(chains) != 0 {
		t.Errorf("IPv4 request got ip6 chains: %+v", chains)
	}
	if chains := parseNftRuleset(out, IPv6); len(chains) != 1 {
		t.Errorf("IPv6 request want 1 chain, got %d", len(chains))
	}
}

func findChain(chains []IPTChain, name string) *IPTChain {
	for i := range chains {
		if chains[i].Name == name {
			return &chains[i]
		}
	}
	return nil
}
