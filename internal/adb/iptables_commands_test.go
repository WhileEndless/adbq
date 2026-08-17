package adb

import (
	"strings"
	"testing"
)

// The table and the su wrapper are the two things the old rule-text preview
// left out, and both change what the command does.
func TestIptablesCommandsNameTheTableAndTheWrapper(t *testing.T) {
	got := IptablesCommandsFor("emulator-5554", IptablesCommandRequest{
		Family: "ipv4", Table: "nat", Chain: "OUTPUT",
		Spec: []string{"-p", "tcp", "--dport", "443", "-j", "ACCEPT"},
	}, PlainRenderer("emulator-5554"))

	want := "adb -s emulator-5554 shell 'su -c '\\''iptables -t nat -A OUTPUT -p tcp --dport 443 -j ACCEPT'\\'''"
	if got.AddRule[0] != want {
		t.Errorf("add rule:\n got  %s\n want %s", got.AddRule[0], want)
	}
}

func TestIptablesCommandsInsertCarriesThePosition(t *testing.T) {
	got := IptablesCommandsFor("emulator-5554", IptablesCommandRequest{
		Family: "ipv4", Table: "filter", Chain: "INPUT", Pos: 3,
		Spec: []string{"-j", "DROP"},
	}, PlainRenderer("emulator-5554"))
	if !strings.Contains(got.AddRule[0], "-I INPUT 3 -j DROP") {
		t.Errorf("add rule: %s", got.AddRule[0])
	}
}

func TestIptablesCommandsIPv6UsesIp6tables(t *testing.T) {
	got := IptablesCommandsFor("emulator-5554", IptablesCommandRequest{
		Family: "ipv6", Table: "filter", Chain: "INPUT", Num: 2, Policy: "DROP",
	}, PlainRenderer("emulator-5554"))
	for _, line := range [][]string{got.List, got.Save, got.DeleteRule, got.Policy, got.FlushChain} {
		if !strings.Contains(line[0], "ip6tables") {
			t.Errorf("ipv6 command used the v4 binary: %s", line[0])
		}
	}
	if !strings.Contains(got.DeleteRule[0], "-D INPUT 2") {
		t.Errorf("delete: %s", got.DeleteRule[0])
	}
	if !strings.Contains(got.Policy[0], "-P INPUT DROP") {
		t.Errorf("policy: %s", got.Policy[0])
	}
}

// Without a selected chain, only the table-wide actions have a command; a
// chain-shaped preview with an empty name would paste as a broken command.
func TestIptablesCommandsWithoutAChain(t *testing.T) {
	got := IptablesCommandsFor("emulator-5554", IptablesCommandRequest{Family: "ipv4"}, PlainRenderer("emulator-5554"))
	if len(got.FlushChain) != 0 || len(got.DropChain) != 0 || len(got.AddRule) != 0 {
		t.Errorf("unexpected chain commands: %#v", got)
	}
	if !strings.Contains(got.FlushTable[0], "-t filter -F'") {
		t.Errorf("flush table: %s", got.FlushTable[0])
	}
}

// Both restores go through a staged file, and the preview has to show all three
// steps or it reads like a single atomic command.
func TestIptablesRestorePreviewsShowEveryStep(t *testing.T) {
	got := IptablesCommandsFor("emulator-5554", IptablesCommandRequest{Family: "ipv4"}, PlainRenderer("emulator-5554"))
	if len(got.Import) != 3 {
		t.Fatalf("import: %#v", got.Import)
	}
	if !strings.Contains(got.Import[1], "iptables-restore < "+iptRestoreStaged) {
		t.Errorf("import: %s", got.Import[1])
	}
	if !strings.Contains(strings.Join(got.Undo, "\n"), iptUndoStaged) {
		t.Errorf("undo: %#v", got.Undo)
	}
}
