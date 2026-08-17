package adb

import (
	"context"
	"fmt"
	"strings"
)

// The iptables commands, as functions shared by the mutations and the previews.
//
// This screen showed rule *text* but never the invocation: no table, no `-I N`,
// no `su`. That is the preview most likely to mislead — someone reads
// "iptables -A INPUT -j DROP", pastes it into a shell, and hits the wrong table
// (CLAUDE.md §4.1 K1).

// iptRuleCmd builds an -A or -I mutation. pos only applies to -I.
func iptRuleCmd(fam IPFamily, table IPTable, op, chain string, pos int, spec IptablesSpec) string {
	bin := iptablesBinary(fam)
	if op == "-I" && pos > 0 {
		return fmt.Sprintf("%s -t %s %s %s %d %s", bin, table, op, chain, pos, strings.Join(spec, " "))
	}
	return fmt.Sprintf("%s -t %s %s %s %s", bin, table, op, chain, strings.Join(spec, " "))
}

func iptDeleteRuleCmd(fam IPFamily, table IPTable, chain string, num int) string {
	return fmt.Sprintf("%s -t %s -D %s %d", iptablesBinary(fam), table, chain, num)
}

// iptFlushCmd clears one chain, or the whole table when chain is empty.
func iptFlushCmd(fam IPFamily, table IPTable, chain string) string {
	if chain == "" {
		return fmt.Sprintf("%s -t %s -F", iptablesBinary(fam), table)
	}
	return fmt.Sprintf("%s -t %s -F %s", iptablesBinary(fam), table, chain)
}

func iptPolicyCmd(fam IPFamily, table IPTable, chain, policy string) string {
	return fmt.Sprintf("%s -t %s -P %s %s", iptablesBinary(fam), table, chain, policy)
}

func iptNewChainCmd(fam IPFamily, table IPTable, chain string) string {
	return fmt.Sprintf("%s -t %s -N %s", iptablesBinary(fam), table, chain)
}

func iptDeleteChainCmd(fam IPFamily, table IPTable, chain string) string {
	return fmt.Sprintf("%s -t %s -X %s", iptablesBinary(fam), table, chain)
}

func iptListCmd(fam IPFamily, table IPTable) string {
	return fmt.Sprintf("%s -t %s -nvL --line-numbers", iptablesBinary(fam), table)
}

func iptRestoreCmd(fam IPFamily, from string) string {
	return iptablesRestoreBinary(fam) + " < " + from
}

// iptRestoreStaged is where a blob is staged before iptables-restore reads it:
// multi-line input cannot travel through `su -c '…'` intact.
const iptRestoreStaged = "/data/local/tmp/adbq-ipt-restore.rules"

// iptUndoStaged is the same for the undo ring's snapshot.
const iptUndoStaged = "/data/local/tmp/adbq-ipt-undo.rules"

// IptablesCommandRequest is the screen's current selection: what the previews
// should be rendered for. Fields the request leaves empty simply produce no
// command for the actions that need them.
type IptablesCommandRequest struct {
	Family string   `json:"family"` // "ipv4" | "ipv6"
	Table  string   `json:"table"`
	Chain  string   `json:"chain"`
	Pos    int      `json:"pos"`    // insert position; 0 means append
	Num    int      `json:"num"`    // rule number to delete
	Policy string   `json:"policy"` // policy to set
	Spec   []string `json:"spec"`   // rule spec, as argv
}

// IptablesCommands is one command list per action the screen offers. Every
// mutation runs as root, so each line carries the device's `su` form.
type IptablesCommands struct {
	List       []string `json:"list"`
	Save       []string `json:"save"`
	AddRule    []string `json:"addRule"`
	DeleteRule []string `json:"deleteRule"`
	FlushChain []string `json:"flushChain"`
	FlushTable []string `json:"flushTable"`
	Policy     []string `json:"policy"`
	NewChain   []string `json:"newChain"`
	DropChain  []string `json:"dropChain"`
	Import     []string `json:"import"`
	Undo       []string `json:"undo"`
}

// IptablesCommandsFor renders the screen's previews. Nothing here validates the
// request: a spec adbq would refuse still renders, because seeing the command
// that was rejected is how someone works out why.
func IptablesCommandsFor(serial string, req IptablesCommandRequest, render CommandRenderer) IptablesCommands {
	fam := IPFamily(req.Family)
	if fam != IPv6 {
		fam = IPv4
	}
	table := IPTable(req.Table)
	if table == "" {
		table = TableFilter
	}
	ic := IptablesCommands{
		List:       []string{render(iptListCmd(fam, table), true)},
		Save:       []string{render(iptablesSaveBinary(fam), true)},
		FlushTable: []string{render(iptFlushCmd(fam, table, ""), true)},
		// Both restores read a staged file, so both are three steps.
		Import: []string{
			DeviceCommandText(serial, "push", "<rules-file>", iptRestoreStaged),
			render(iptRestoreCmd(fam, iptRestoreStaged), true),
			render("rm -f "+iptRestoreStaged, false),
		},
		Undo: []string{
			"# the snapshot adbq took before the last change, staged again:",
			DeviceCommandText(serial, "push", "<snapshot>", iptUndoStaged),
			render(iptRestoreCmd(fam, iptUndoStaged), true),
			render("rm -f "+iptUndoStaged, false),
		},
	}
	if req.Chain == "" {
		return ic
	}
	ic.FlushChain = []string{render(iptFlushCmd(fam, table, req.Chain), true)}
	ic.NewChain = []string{render(iptNewChainCmd(fam, table, req.Chain), true)}
	ic.DropChain = []string{render(iptDeleteChainCmd(fam, table, req.Chain), true)}
	if len(req.Spec) > 0 {
		op := "-A"
		if req.Pos > 0 {
			op = "-I"
		}
		ic.AddRule = []string{render(iptRuleCmd(fam, table, op, req.Chain, req.Pos, req.Spec), true)}
	}
	if req.Num > 0 {
		ic.DeleteRule = []string{render(iptDeleteRuleCmd(fam, table, req.Chain, req.Num), true)}
	}
	if req.Policy != "" {
		ic.Policy = []string{render(iptPolicyCmd(fam, table, req.Chain, req.Policy), true)}
	}
	return ic
}

// IptablesCommandsFor is the device-aware entry point.
func (c *Client) IptablesCommandsFor(ctx context.Context, serial string, req IptablesCommandRequest) IptablesCommands {
	return IptablesCommandsFor(serial, req, c.Renderer(ctx, serial))
}
