package adb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

// osCreateTemp / osRemove kept as locals so test helpers can override them.
var (
	osCreateTemp = func(pattern string) (*os.File, error) { return os.CreateTemp("", pattern) }
	osRemove     = os.Remove
)

// IPFamily selects iptables vs ip6tables.
type IPFamily string

const (
	IPv4 IPFamily = "ipv4"
	IPv6 IPFamily = "ipv6"
)

// IPTable is one of the standard tables iptables exposes.
type IPTable string

const (
	TableFilter   IPTable = "filter"
	TableNat      IPTable = "nat"
	TableMangle   IPTable = "mangle"
	TableRaw      IPTable = "raw"
	TableSecurity IPTable = "security"
)

// IPTBackendInfo describes what's actually available on the device. iptables
// on modern Android is usually a multi-call symlink to either the legacy or
// the nftables backend; we report which one (and let the UI surface it).
type IPTBackendInfo struct {
	Family    IPFamily `json:"family"`
	Available bool     `json:"available"`
	Path      string   `json:"path"`    // resolved path (e.g. /system/bin/iptables)
	Version   string   `json:"version"` // `iptables --version` first line
	Mode      string   `json:"mode"`    // "legacy" | "nft" | "unknown"
	HasSave   bool     `json:"hasSave"` // iptables-save present
	NeedsRoot bool     `json:"needsRoot"`
}

// IPTRule is one rule line from `iptables -t <table> -nvL --line-numbers`.
type IPTRule struct {
	Num      int    `json:"num"`
	Pkts     uint64 `json:"pkts"`
	Bytes    uint64 `json:"bytes"`
	Target   string `json:"target"`
	Proto    string `json:"proto"`
	Opt      string `json:"opt"`
	InIface  string `json:"inIface"`
	OutIface string `json:"outIface"`
	Source   string `json:"source"`
	Dest     string `json:"dest"`
	Extra    string `json:"extra"` // matches, comments, dport/sport, mark, ...
	Raw      string `json:"raw"`
}

// IPTChain bundles a chain header with its parsed rules.
type IPTChain struct {
	Name   string    `json:"name"`
	Policy string    `json:"policy"` // ACCEPT/DROP for built-ins, "-" for user chains
	Pkts   uint64    `json:"pkts"`
	Bytes  uint64    `json:"bytes"`
	Rules  []IPTRule `json:"rules"`
}

// IPTSnapshot is the full picture for one (family, table) pair. Restore is the
// iptables-save text that ImportIptables / UndoIptables can apply atomically.
type IPTSnapshot struct {
	Family  IPFamily   `json:"family"`
	Table   IPTable    `json:"table"`
	Mode    string     `json:"mode"`
	Chains  []IPTChain `json:"chains"`
	Restore string     `json:"restore"`
}

// undoRing keeps the last N iptables-save blobs per (serial, family) for the
// Undo button. The depth is small because each blob can be tens of KB and we
// only need a "step back" affordance, not full audit history.
const iptablesUndoDepth = 10

type undoKey struct {
	Serial string
	Family IPFamily
}

type iptablesState struct {
	mu    sync.Mutex
	undos map[undoKey][]string
}

func (s *iptablesState) pushUndo(k undoKey, blob string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.undos == nil {
		s.undos = map[undoKey][]string{}
	}
	ring := s.undos[k]
	ring = append(ring, blob)
	if len(ring) > iptablesUndoDepth {
		ring = ring[len(ring)-iptablesUndoDepth:]
	}
	s.undos[k] = ring
}

func (s *iptablesState) popUndo(k undoKey) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ring := s.undos[k]
	if len(ring) == 0 {
		return "", false
	}
	blob := ring[len(ring)-1]
	s.undos[k] = ring[:len(ring)-1]
	return blob, true
}

// Iptables is the per-Client state holder for undo snapshots. Lazy-init.
var iptablesGlobal = &iptablesState{}

// ProbeIptables reports whether the requested family is usable on the device
// and what backend mode it's in. Never returns an error for "missing" — the
// Available flag covers that case.
func (c *Client) ProbeIptables(ctx context.Context, serial string, fam IPFamily) (*IPTBackendInfo, error) {
	bin := iptablesBinary(fam)
	info := &IPTBackendInfo{Family: fam, NeedsRoot: true}
	out, err := c.Shell(ctx, serial, "command -v "+bin)
	if err == nil {
		info.Path = strings.TrimSpace(out)
	}
	// PATH lookup misses Magisk modules. Probe the well-known module tree as
	// a fallback — many privacy/firewall modules ship their own iptables.
	if info.Path == "" {
		probe := "for f in /data/adb/modules/*/system/bin/" + bin + " /data/adb/modules/*/system/xbin/" + bin + "; do [ -f \"$f\" ] && echo \"$f\" && exit 0; done"
		if out, _ := c.Shell(ctx, serial, "su -c '"+probe+"'"); strings.TrimSpace(out) != "" {
			info.Path = strings.TrimSpace(out)
		}
	}
	if info.Path == "" {
		return info, nil
	}
	info.Available = true
	// Try --version both as user and via su; OEMs lock down iptables to root.
	v, _ := c.Shell(ctx, serial, "su -c '"+info.Path+" --version' 2>&1 | head -1")
	v = strings.TrimSpace(v)
	if v == "" || strings.Contains(v, "Permission denied") {
		v, _ = c.Shell(ctx, serial, info.Path+" --version 2>&1 | head -1")
		v = strings.TrimSpace(v)
	}
	info.Version = v
	switch {
	case strings.Contains(v, "nf_tables"):
		info.Mode = "nft"
	case strings.Contains(v, "legacy"):
		info.Mode = "legacy"
	case v != "":
		info.Mode = "legacy" // pre-1.8.x had no marker
	default:
		info.Mode = "unknown"
	}
	saveBin := strings.TrimSuffix(bin, "tables") + "tables-save"
	if _, err := c.Shell(ctx, serial, "command -v "+saveBin); err == nil {
		info.HasSave = true
	}
	return info, nil
}

func iptablesBinary(fam IPFamily) string {
	if fam == IPv6 {
		return "ip6tables"
	}
	return "iptables"
}

func iptablesSaveBinary(fam IPFamily) string {
	if fam == IPv6 {
		return "ip6tables-save"
	}
	return "iptables-save"
}

func iptablesRestoreBinary(fam IPFamily) string {
	if fam == IPv6 {
		return "ip6tables-restore"
	}
	return "iptables-restore"
}

// ListIptables runs `iptables -t <table> -nvL --line-numbers` under su and
// parses the result into a snapshot. Also captures iptables-save output for
// the Raw mode + undo ring.
func (c *Client) ListIptables(ctx context.Context, serial string, fam IPFamily, table IPTable) (*IPTSnapshot, error) {
	bin := iptablesBinary(fam)
	tbl := string(table)
	if tbl == "" {
		tbl = string(TableFilter)
	}
	list, err := c.Shell(ctx, serial, "su -c '"+bin+" -t "+tbl+" -nvL --line-numbers'")
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	snap := &IPTSnapshot{Family: fam, Table: IPTable(tbl)}
	snap.Chains = parseIptablesL(list)
	// iptables-save gives us a transaction blob for import/export/undo. Errors
	// here are non-fatal (some devices restrict the binary harder than -L).
	if save, err := c.Shell(ctx, serial, "su -c '"+iptablesSaveBinary(fam)+" -t "+tbl+"'"); err == nil {
		snap.Restore = save
	}
	if probe, _ := c.ProbeIptables(ctx, serial, fam); probe != nil {
		snap.Mode = probe.Mode
	}
	return snap, nil
}

// parseIptablesL parses the output of `iptables -nvL --line-numbers`.
// Returns one IPTChain per chain header, with rule rows attached. Robust to
// blank lines, the column-name header repeated per chain, and either the
// "policy ACCEPT 12 packets, 1024 bytes" or "0 references" suffix for user
// chains.
func parseIptablesL(out string) []IPTChain {
	var chains []IPTChain
	var cur *IPTChain
	for _, raw := range strings.Split(out, "\n") {
		ln := strings.TrimRight(raw, " \t")
		if ln == "" {
			continue
		}
		if strings.HasPrefix(ln, "Chain ") {
			if cur != nil {
				chains = append(chains, *cur)
			}
			cur = parseChainHeader(ln)
			continue
		}
		if cur == nil {
			continue
		}
		// Column header row: "num   pkts bytes target ..."
		if strings.HasPrefix(strings.TrimSpace(ln), "num") {
			continue
		}
		if r, ok := parseIptablesRule(ln); ok {
			cur.Rules = append(cur.Rules, r)
		}
	}
	if cur != nil {
		chains = append(chains, *cur)
	}
	return chains
}

func parseChainHeader(ln string) *IPTChain {
	// Examples:
	//   Chain INPUT (policy ACCEPT 12 packets, 1024 bytes)
	//   Chain mychain (0 references)
	c := &IPTChain{Policy: "-"}
	rest := strings.TrimPrefix(ln, "Chain ")
	openParen := strings.Index(rest, "(")
	if openParen < 0 {
		c.Name = strings.TrimSpace(rest)
		return c
	}
	c.Name = strings.TrimSpace(rest[:openParen])
	inside := strings.TrimSuffix(strings.TrimSpace(rest[openParen+1:]), ")")
	if strings.HasPrefix(inside, "policy ") {
		parts := strings.Fields(inside)
		if len(parts) >= 2 {
			c.Policy = parts[1]
		}
		// "policy ACCEPT 12 packets, 1024 bytes"
		for i, p := range parts {
			if p == "packets," && i >= 1 {
				if n, err := strconv.ParseUint(parts[i-1], 10, 64); err == nil {
					c.Pkts = n
				}
			}
			if p == "bytes)" || p == "bytes" {
				if i >= 1 {
					if n, err := strconv.ParseUint(parts[i-1], 10, 64); err == nil {
						c.Bytes = n
					}
				}
			}
		}
	}
	return c
}

// parseIptablesRule reads one rule line. Columns are space-separated but the
// "Extra" tail can contain its own spaces (matches like `tcp dpt:443`), so we
// take the first 9 fields verbatim and treat the remainder as Extra.
func parseIptablesRule(ln string) (IPTRule, bool) {
	fields := strings.Fields(ln)
	if len(fields) < 9 {
		return IPTRule{}, false
	}
	num, err := strconv.Atoi(fields[0])
	if err != nil {
		return IPTRule{}, false
	}
	pkts, _ := strconv.ParseUint(fields[1], 10, 64)
	bytesN, _ := strconv.ParseUint(fields[2], 10, 64)
	r := IPTRule{
		Num: num, Pkts: pkts, Bytes: bytesN,
		Target: fields[3], Proto: fields[4], Opt: fields[5],
		InIface: fields[6], OutIface: fields[7],
		Source: fields[8],
	}
	if len(fields) >= 10 {
		r.Dest = fields[9]
	}
	if len(fields) > 10 {
		r.Extra = strings.Join(fields[10:], " ")
	}
	r.Raw = strings.TrimSpace(ln)
	return r, true
}

// IptablesSpec is one well-formed rule specification. We accept it as a slice
// of strings (the same way iptables takes argv) so we can validate each piece
// individually and never glue a shell string together.
type IptablesSpec []string

// validate refuses anything containing shell metacharacters. iptables CLI
// itself doesn't need them, and forbidding them removes a whole class of
// injection footguns when we feed `su -c '...spec...'`.
func (s IptablesSpec) validate() error {
	if len(s) == 0 {
		return errors.New("empty rule spec")
	}
	for _, a := range s {
		if strings.ContainsAny(a, "'\"`\\\n\r;&|<>$") {
			return fmt.Errorf("disallowed character in rule arg %q", a)
		}
	}
	return nil
}

// AppendIptablesRule snapshots, then runs `iptables -t <table> -A <chain> ...`.
// Returns the post-mutation snapshot.
func (c *Client) AppendIptablesRule(ctx context.Context, serial string, fam IPFamily, table IPTable, chain string, spec IptablesSpec) (*IPTSnapshot, error) {
	return c.runIptablesMutation(ctx, serial, fam, table, "-A", chain, 0, spec)
}

// InsertIptablesRule places the rule at position pos (1-indexed).
func (c *Client) InsertIptablesRule(ctx context.Context, serial string, fam IPFamily, table IPTable, chain string, pos int, spec IptablesSpec) (*IPTSnapshot, error) {
	return c.runIptablesMutation(ctx, serial, fam, table, "-I", chain, pos, spec)
}

// DeleteIptablesRule removes rule number `num` from the chain.
func (c *Client) DeleteIptablesRule(ctx context.Context, serial string, fam IPFamily, table IPTable, chain string, num int) (*IPTSnapshot, error) {
	if num <= 0 {
		return nil, fmt.Errorf("rule number must be >= 1, got %d", num)
	}
	if err := c.snapshotForUndo(ctx, serial, fam); err != nil {
		return nil, err
	}
	bin := iptablesBinary(fam)
	cmd := fmt.Sprintf("%s -t %s -D %s %d", bin, table, chain, num)
	if out, err := c.Shell(ctx, serial, "su -c '"+cmd+"'"); err != nil {
		return nil, fmt.Errorf("delete: %w (%s)", err, strings.TrimSpace(out))
	}
	return c.ListIptables(ctx, serial, fam, table)
}

// FlushIptables clears one chain when chain != "", or the whole table when
// chain == "". Snapshots first.
func (c *Client) FlushIptables(ctx context.Context, serial string, fam IPFamily, table IPTable, chain string) (*IPTSnapshot, error) {
	if err := c.snapshotForUndo(ctx, serial, fam); err != nil {
		return nil, err
	}
	bin := iptablesBinary(fam)
	var cmd string
	if chain == "" {
		cmd = fmt.Sprintf("%s -t %s -F", bin, table)
	} else {
		cmd = fmt.Sprintf("%s -t %s -F %s", bin, table, chain)
	}
	if out, err := c.Shell(ctx, serial, "su -c '"+cmd+"'"); err != nil {
		return nil, fmt.Errorf("flush: %w (%s)", err, strings.TrimSpace(out))
	}
	return c.ListIptables(ctx, serial, fam, table)
}

// SetIptablesPolicy changes a built-in chain's default policy. Dangerous —
// "INPUT DROP" can lock the user out of the device, so the UI should pair
// this with the safety timer.
func (c *Client) SetIptablesPolicy(ctx context.Context, serial string, fam IPFamily, table IPTable, chain, policy string) error {
	if policy != "ACCEPT" && policy != "DROP" && policy != "REJECT" {
		return fmt.Errorf("invalid policy %q (want ACCEPT/DROP/REJECT)", policy)
	}
	if err := c.snapshotForUndo(ctx, serial, fam); err != nil {
		return err
	}
	bin := iptablesBinary(fam)
	cmd := fmt.Sprintf("%s -t %s -P %s %s", bin, table, chain, policy)
	if out, err := c.Shell(ctx, serial, "su -c '"+cmd+"'"); err != nil {
		return fmt.Errorf("policy: %w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}

// CreateIptablesChain adds a user chain (-N).
func (c *Client) CreateIptablesChain(ctx context.Context, serial string, fam IPFamily, table IPTable, chain string) error {
	if !looksLikeIdent(chain) {
		return fmt.Errorf("invalid chain name %q", chain)
	}
	if err := c.snapshotForUndo(ctx, serial, fam); err != nil {
		return err
	}
	bin := iptablesBinary(fam)
	cmd := fmt.Sprintf("%s -t %s -N %s", bin, table, chain)
	if out, err := c.Shell(ctx, serial, "su -c '"+cmd+"'"); err != nil {
		return fmt.Errorf("create chain: %w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}

// DeleteIptablesChain removes an empty user chain (-X).
func (c *Client) DeleteIptablesChain(ctx context.Context, serial string, fam IPFamily, table IPTable, chain string) error {
	if !looksLikeIdent(chain) {
		return fmt.Errorf("invalid chain name %q", chain)
	}
	if err := c.snapshotForUndo(ctx, serial, fam); err != nil {
		return err
	}
	bin := iptablesBinary(fam)
	cmd := fmt.Sprintf("%s -t %s -X %s", bin, table, chain)
	if out, err := c.Shell(ctx, serial, "su -c '"+cmd+"'"); err != nil {
		return fmt.Errorf("delete chain: %w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}

// ExportIptables returns the full `iptables-save` blob across all tables.
func (c *Client) ExportIptables(ctx context.Context, serial string, fam IPFamily) (string, error) {
	return c.Shell(ctx, serial, "su -c '"+iptablesSaveBinary(fam)+"'")
}

// ImportIptables applies a `iptables-restore` blob. Snapshots before applying.
func (c *Client) ImportIptables(ctx context.Context, serial string, fam IPFamily, blob string) error {
	if err := c.snapshotForUndo(ctx, serial, fam); err != nil {
		return err
	}
	// We can't pass multi-line input through `adb shell`'s `su -c '...'` form
	// reliably — newlines and quotes break the single-quoted string. Stage
	// the blob in /data/local/tmp and feed it via input redirection.
	tmp := "/data/local/tmp/adbq-ipt-restore.rules"
	if err := c.pushBlob(ctx, serial, tmp, blob); err != nil {
		return fmt.Errorf("stage blob: %w", err)
	}
	cmd := iptablesRestoreBinary(fam) + " < " + tmp
	if out, err := c.Shell(ctx, serial, "su -c '"+cmd+"'"); err != nil {
		return fmt.Errorf("restore: %w (%s)", err, strings.TrimSpace(out))
	}
	_, _ = c.Shell(ctx, serial, "rm -f "+tmp)
	return nil
}

// UndoIptables pops the last snapshot for (serial, fam) and restores it.
func (c *Client) UndoIptables(ctx context.Context, serial string, fam IPFamily) (*IPTSnapshot, error) {
	k := undoKey{Serial: serial, Family: fam}
	blob, ok := iptablesGlobal.popUndo(k)
	if !ok {
		return nil, errors.New("no undo snapshot available")
	}
	// We deliberately don't re-snapshot before undo: that would let the user
	// "undo the undo" into the same state forever and confuse the ring.
	tmp := "/data/local/tmp/adbq-ipt-undo.rules"
	if err := c.pushBlob(ctx, serial, tmp, blob); err != nil {
		return nil, err
	}
	cmd := iptablesRestoreBinary(fam) + " < " + tmp
	if out, err := c.Shell(ctx, serial, "su -c '"+cmd+"'"); err != nil {
		return nil, fmt.Errorf("undo restore: %w (%s)", err, strings.TrimSpace(out))
	}
	_, _ = c.Shell(ctx, serial, "rm -f "+tmp)
	return c.ListIptables(ctx, serial, fam, TableFilter)
}

// snapshotForUndo captures iptables-save output and pushes it onto the per-
// (serial, family) undo ring. Non-fatal: a snapshot failure logs but the
// mutation still proceeds — better to allow the user to act than to refuse
// every change because we couldn't take a backup.
func (c *Client) snapshotForUndo(ctx context.Context, serial string, fam IPFamily) error {
	blob, err := c.ExportIptables(ctx, serial, fam)
	if err != nil || strings.TrimSpace(blob) == "" {
		return nil
	}
	iptablesGlobal.pushUndo(undoKey{Serial: serial, Family: fam}, blob)
	return nil
}

// runIptablesMutation is the shared backbone for -A / -I. Validates the spec,
// snapshots, runs the command, and re-lists.
func (c *Client) runIptablesMutation(ctx context.Context, serial string, fam IPFamily, table IPTable, op string, chain string, pos int, spec IptablesSpec) (*IPTSnapshot, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	if !looksLikeIdent(chain) {
		return nil, fmt.Errorf("invalid chain name %q", chain)
	}
	if err := c.snapshotForUndo(ctx, serial, fam); err != nil {
		return nil, err
	}
	bin := iptablesBinary(fam)
	var cmd string
	if op == "-I" && pos > 0 {
		cmd = fmt.Sprintf("%s -t %s %s %s %d %s", bin, table, op, chain, pos, strings.Join(spec, " "))
	} else {
		cmd = fmt.Sprintf("%s -t %s %s %s %s", bin, table, op, chain, strings.Join(spec, " "))
	}
	if out, err := c.Shell(ctx, serial, "su -c '"+cmd+"'"); err != nil {
		return nil, fmt.Errorf("%s: %w (%s)", op, err, strings.TrimSpace(out))
	}
	return c.ListIptables(ctx, serial, fam, table)
}

// pushBlob writes content to remote via a local temp file + adb push. Keeps
// us from quoting nightmares with multi-line iptables-save data.
func (c *Client) pushBlob(ctx context.Context, serial, remote, content string) error {
	tmp, err := osCreateTemp("adbq-blob-*.txt")
	if err != nil {
		return err
	}
	defer osRemove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	if _, err := c.PushFile(ctx, serial, tmp.Name(), remote); err != nil {
		return err
	}
	return nil
}

// looksLikeIdent enforces the iptables chain-name grammar (letters, digits,
// underscore, hyphen) to keep us from emitting shell-busting input.
func looksLikeIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
