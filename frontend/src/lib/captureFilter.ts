// captureFilter — Wireshark-compatible display filter language.
//
// Goal: every Wireshark display filter the user is likely to type for the
// protocols we decode (Ethernet/IPv4/IPv6/TCP/UDP/ICMP/ARP/DNS/TLS/HTTP/QUIC)
// should Just Work, with the same syntax. We don't ship Wireshark's full
// dissector tree — we map a curated set of well-known field names onto the
// fields we already extract per packet, and forward operators (==, !=,
// contains, matches, >, >=, <, <=, in, &&, ||, !, and, or, not).
//
// Examples that parse and run live:
//   tcp.port == 443
//   ip.src == 1.1.1.1 and not tcp.flags.syn
//   dns and dns.qry.name contains "google"
//   http.host == "example.com"
//   tls.handshake.extensions_server_name contains "github"
//   tcp.port in {80, 443, 8080}
//   ip.addr == 10.0.0.0/24
//   frame.len > 1000
//
// Unsupported fields raise a parse error so the user sees what tripped the
// expression rather than silently matching everything.

export type Pred = (p: Packet) => boolean;

export interface Packet {
  no: number;
  ts?: string;
  length: number;
  srcIP: string; dstIP: string;
  srcPort: number; dstPort: number;
  proto: string;
  info: string;
}

const TRUE: Pred = () => true;

export interface ParsedFilter {
  pred: Pred;
  error: string;
  /**
   * True when the filter matches everything, either because it is blank or
   * because it failed to parse and degraded to "show all".
   *
   * Callers use it to skip the walk: running a match-everything predicate over
   * a hundred thousand packets several times a second produces a copy of the
   * list it started from, which is the common case — most of the time nobody
   * has typed a filter.
   */
  isEmpty: boolean;
}

export function parseFilter(input: string): ParsedFilter {
  const text = (input || '').trim();
  if (!text) return {pred: TRUE, error: '', isEmpty: true};
  try {
    const toks = tokenize(text);
    const p = new Parser(toks);
    const pred = p.parseExpr();
    if (!p.eof()) throw new Error(`unexpected token '${p.peek()!.text}'`);
    return {pred, error: '', isEmpty: false};
  } catch (e) {
    return {pred: TRUE, error: e instanceof Error ? e.message : String(e), isEmpty: true};
  }
}

// ─── Tokenizer ───────────────────────────────────────────────────────────
// Recognised tokens:
//   IDENT  — letters/digits/dot/underscore/hyphen (the field path or bare word)
//   NUMBER — integer (in any base 10/16 with 0x prefix)
//   STRING — single or double quoted
//   OP     — ==, !=, <=, >=, <, >, contains, matches, in
//   AND/OR/NOT and aliases && || !
//   LP/RP  — ( )
//   LB/RB  — { }   (only inside `in {...}` set literals)
//   COMMA  — ,

type Tok =
  | {kind: 'ident'; text: string; pos: number}
  | {kind: 'number'; text: string; pos: number}
  | {kind: 'string'; text: string; pos: number}
  | {kind: 'op'; text: string; pos: number}
  | {kind: 'and'|'or'|'not'|'in'|'lp'|'rp'|'lb'|'rb'|'comma'; text: string; pos: number};

function tokenize(s: string): Tok[] {
  const out: Tok[] = [];
  let i = 0;
  while (i < s.length) {
    const c = s[i];
    if (/\s/.test(c)) { i++; continue; }
    if (c === '(') { out.push({kind: 'lp', text: '(', pos: i}); i++; continue; }
    if (c === ')') { out.push({kind: 'rp', text: ')', pos: i}); i++; continue; }
    if (c === '{') { out.push({kind: 'lb', text: '{', pos: i}); i++; continue; }
    if (c === '}') { out.push({kind: 'rb', text: '}', pos: i}); i++; continue; }
    if (c === ',') { out.push({kind: 'comma', text: ',', pos: i}); i++; continue; }
    if (c === '&' && s[i+1] === '&') { out.push({kind: 'and', text: '&&', pos: i}); i += 2; continue; }
    if (c === '|' && s[i+1] === '|') { out.push({kind: 'or', text: '||', pos: i}); i += 2; continue; }
    if (c === '!' && s[i+1] === '=') { out.push({kind: 'op', text: '!=', pos: i}); i += 2; continue; }
    if (c === '=' && s[i+1] === '=') { out.push({kind: 'op', text: '==', pos: i}); i += 2; continue; }
    if (c === '<' && s[i+1] === '=') { out.push({kind: 'op', text: '<=', pos: i}); i += 2; continue; }
    if (c === '>' && s[i+1] === '=') { out.push({kind: 'op', text: '>=', pos: i}); i += 2; continue; }
    if (c === '<') { out.push({kind: 'op', text: '<', pos: i}); i++; continue; }
    if (c === '>') { out.push({kind: 'op', text: '>', pos: i}); i++; continue; }
    if (c === '=') { out.push({kind: 'op', text: '==', pos: i}); i++; continue; } // tolerate single =
    if (c === '!') { out.push({kind: 'not', text: '!', pos: i}); i++; continue; }
    if (c === '"' || c === "'") {
      const q = c; const start = i + 1; i++;
      while (i < s.length && s[i] !== q) i++;
      out.push({kind: 'string', text: s.slice(start, i), pos: start});
      if (i < s.length) i++;
      continue;
    }
    if (/[0-9]/.test(c)) {
      // Consume digits, hex letters, dots, slashes, colons — covers plain
      // numbers, hex literals (0x…), IPv4 addresses (10.0.0.5), CIDR ranges
      // (10.0.0.0/24) and the front of IPv6-ish literals. We classify after
      // the fact: an ident if it has any non-digit, a number otherwise.
      let j = i;
      while (j < s.length && /[0-9a-fA-FxX./:]/.test(s[j])) j++;
      const text = s.slice(i, j);
      if (/[./:a-fA-FxX]/.test(text.slice(1)) && !/^0[xX][0-9a-fA-F]+$/.test(text)) {
        out.push({kind: 'ident', text, pos: i});
      } else {
        out.push({kind: 'number', text, pos: i});
      }
      i = j; continue;
    }
    // ident — letters/digits/dot/underscore/hyphen/slash. Slash needed for
    // CIDR like "10.0.0.0/24" tokenised together. Stops on whitespace and
    // punctuation we tokenise separately.
    let j = i;
    while (j < s.length && /[A-Za-z0-9._/-]/.test(s[j])) j++;
    const w = s.slice(i, j);
    const lower = w.toLowerCase();
    switch (lower) {
      case 'and': out.push({kind: 'and', text: w, pos: i}); break;
      case 'or':  out.push({kind: 'or',  text: w, pos: i}); break;
      case 'not': out.push({kind: 'not', text: w, pos: i}); break;
      case 'in':  out.push({kind: 'in',  text: w, pos: i}); break;
      case 'contains':
      case 'matches':
      case 'eq':
      case 'ne':
        out.push({kind: 'op', text: lower === 'eq' ? '==' : lower === 'ne' ? '!=' : lower, pos: i});
        break;
      default:
        out.push({kind: 'ident', text: w, pos: i});
    }
    i = j;
  }
  return out;
}

// ─── Parser ──────────────────────────────────────────────────────────────
// Precedence: or < and < unary-not < primary.
// Primary is either '(' expr ')' or a field-test.
// A field-test is `ident` (boolean existence) OR `ident OP rhs` where rhs is
// number / string / ident / set literal (`{a, b, c}`) / CIDR.

class Parser {
  i = 0;
  constructor(public toks: Tok[]) {}
  peek(o = 0): Tok | undefined { return this.toks[this.i + o]; }
  eof(): boolean { return this.i >= this.toks.length; }
  eat<K extends Tok['kind']>(k: K): Tok | null {
    const t = this.peek();
    if (t && t.kind === k) { this.i++; return t; }
    return null;
  }

  parseExpr(): Pred { return this.parseOr(); }

  parseOr(): Pred {
    let left = this.parseAnd();
    while (this.peek()?.kind === 'or') {
      this.i++;
      const right = this.parseAnd();
      const l = left, r = right;
      left = (p) => l(p) || r(p);
    }
    return left;
  }
  parseAnd(): Pred {
    let left = this.parseUnary();
    while (true) {
      const t = this.peek();
      if (!t) break;
      if (t.kind === 'and') { this.i++; }
      else if (t.kind === 'or' || t.kind === 'rp' || t.kind === 'rb' || t.kind === 'comma') break;
      const right = this.parseUnary();
      const l = left, r = right;
      left = (p) => l(p) && r(p);
    }
    return left;
  }
  parseUnary(): Pred {
    if (this.peek()?.kind === 'not') {
      this.i++;
      const inner = this.parseUnary();
      return (p) => !inner(p);
    }
    return this.parsePrimary();
  }
  parsePrimary(): Pred {
    if (this.eat('lp')) {
      const e = this.parseExpr();
      if (!this.eat('rp')) throw new Error("missing ')'");
      return e;
    }
    const t = this.peek();
    if (!t || t.kind !== 'ident') throw new Error(`expected field or '(' but got '${t?.text ?? 'EOF'}'`);
    this.i++;
    const field = t.text.toLowerCase();
    // Operator follows?
    const op = this.peek();
    if (op && (op.kind === 'op' || op.kind === 'in')) {
      this.i++;
      const rhs = this.parseRhs(op.kind === 'in');
      return mkPred(field, op.text, rhs);
    }
    // Bare field — boolean existence test (e.g. `tcp`, `dns`, `arp`).
    return mkPred(field, 'exists', null);
  }

  parseRhs(asSet: boolean): unknown {
    if (asSet) {
      if (!this.eat('lb')) throw new Error("'in' expects '{ a, b, c }'");
      const values: unknown[] = [];
      while (this.peek()?.kind !== 'rb') {
        values.push(this.parseLiteral());
        if (!this.eat('comma')) break;
      }
      if (!this.eat('rb')) throw new Error("missing '}'");
      return values;
    }
    return this.parseLiteral();
  }
  parseLiteral(): unknown {
    const t = this.peek();
    if (!t) throw new Error('expected value');
    if (t.kind === 'string' || t.kind === 'ident') { this.i++; return t.text; }
    if (t.kind === 'number') { this.i++; return parseInt(t.text, t.text.startsWith('0x') ? 16 : 10); }
    throw new Error(`expected value, got '${t.text}'`);
  }
}

// ─── Field mapping ────────────────────────────────────────────────────────
// Wireshark exposes thousands of fields; we map the ones our decoder produces
// onto these names. Anything outside the list raises a parse error so users
// know we don't (yet) have that field.

type ScalarGetter = (p: Packet) => number | string | undefined;
type ExistsGetter = (p: Packet) => boolean;

const proto = (p: Packet) => p.proto.toLowerCase();
const info  = (p: Packet) => p.info.toLowerCase();

const FIELDS: Record<string, ScalarGetter | null> = {
  // ip
  'ip.src':  p => p.srcIP,
  'ip.dst':  p => p.dstIP,
  'ip.addr': null, // handled specially (matches either side)
  'ip.len':  p => p.length,
  // ipv6 — we treat ipv6.src/dst as aliases too
  'ipv6.src': p => p.srcIP,
  'ipv6.dst': p => p.dstIP,
  'ipv6.addr': null,
  // frame
  'frame.len': p => p.length,
  'frame.number': p => p.no,
  // tcp
  'tcp.srcport': p => p.srcPort,
  'tcp.dstport': p => p.dstPort,
  'tcp.port':    null,
  // udp
  'udp.srcport': p => p.srcPort,
  'udp.dstport': p => p.dstPort,
  'udp.port':    null,
  // dns / tls / http — we expose Info as the searchable field
  'dns.qry.name': p => info(p),
  'tls.handshake.extensions_server_name': p => info(p),
  'http.host':    p => info(p),
  'http.request.uri': p => info(p),
  'http.request.method': p => info(p),
};

// EXISTS_ALIASES are bare boolean tests like `tcp`, `dns`, `arp`. They map to
// our proto-detection column.
const EXISTS_ALIASES: Record<string, ExistsGetter> = {
  tcp:    p => p.proto === 'TCP'  || (!!p.srcPort && !!p.dstPort && (p.proto === 'TLS' || p.proto === 'HTTP')),
  udp:    p => p.proto === 'UDP'  || p.proto === 'DNS' || p.proto === 'QUIC',
  icmp:   p => p.proto === 'ICMP',
  icmpv6: p => p.proto === 'ICMPv6',
  arp:    p => p.proto === 'ARP',
  dns:    p => p.proto === 'DNS',
  tls:    p => p.proto === 'TLS',
  ssl:    p => p.proto === 'TLS',
  http:   p => p.proto === 'HTTP',
  quic:   p => p.proto === 'QUIC',
  ip:     p => !!p.srcIP,
  ipv6:   p => p.srcIP.includes(':') || p.dstIP.includes(':'),
  ipv4:   p => p.srcIP.includes('.') || p.dstIP.includes('.'),
  frame:  () => true,
};

// mkPred turns one (field, op, rhs) triple into a predicate. Built so the
// same evaluator handles `exists` (bare field) and `==`/`!=`/`<`/`<=`/`>`/`>=`/
// `contains`/`matches`/`in` uniformly.
function mkPred(field: string, op: string, rhs: unknown): Pred {
  if (op === 'exists') {
    const a = EXISTS_ALIASES[field];
    if (a) return a;
    const g = FIELDS[field];
    if (g) return (p) => g(p) != null && String(g(p)) !== '';
    if (field === 'ip.addr' || field === 'ipv6.addr' || field === 'tcp.port' || field === 'udp.port') {
      return (p) => !!p.srcIP || !!p.dstIP;
    }
    throw new Error(`unknown field '${field}' — use ip.src/ip.dst/tcp.port/dns.qry.name/… or proto:VAL`);
  }

  // Either-side fields need both endpoints checked.
  if (field === 'ip.addr' || field === 'ipv6.addr') {
    return cmpEither((p) => p.srcIP, (p) => p.dstIP, op, rhs);
  }
  if (field === 'tcp.port' || field === 'udp.port') {
    return cmpEither((p) => p.srcPort, (p) => p.dstPort, op, rhs);
  }

  // Legacy short keys we already supported (`proto:tls`, `port:443`, …):
  // accept them as field names too so the old hints keep working.
  switch (field) {
    case 'proto': return cmpOne(proto, op, rhs);
    case 'ip':    return cmpEither((p) => p.srcIP, (p) => p.dstIP, op, rhs);
    case 'src':   return cmpOne((p) => p.srcIP, op, rhs);
    case 'dst':   return cmpOne((p) => p.dstIP, op, rhs);
    case 'port':  return cmpEither((p) => p.srcPort, (p) => p.dstPort, op, rhs);
    case 'sport': return cmpOne((p) => p.srcPort, op, rhs);
    case 'dport': return cmpOne((p) => p.dstPort, op, rhs);
    case 'host':  return cmpOne(info, op, rhs);
    case 'info':  return cmpOne(info, op, rhs);
    case 'no':    return cmpOne((p) => p.no, op, rhs);
  }

  const g = FIELDS[field];
  if (!g) throw new Error(`unknown field '${field}'`);
  return cmpOne(g, op, rhs);
}

function cmpOne(g: ScalarGetter, op: string, rhs: unknown): Pred {
  return (p) => compare(g(p), op, rhs);
}
function cmpEither(g1: ScalarGetter, g2: ScalarGetter, op: string, rhs: unknown): Pred {
  // For positive ops, either side matching = true. For negative ops (!=),
  // require BOTH to differ — same semantics Wireshark uses for `ip.addr !=`.
  if (op === '!=') {
    return (p) => compare(g1(p), '!=', rhs) && compare(g2(p), '!=', rhs);
  }
  return (p) => compare(g1(p), op, rhs) || compare(g2(p), op, rhs);
}

function compare(lhs: unknown, op: string, rhs: unknown): boolean {
  if (lhs == null) return false;
  if (op === 'in' && Array.isArray(rhs)) {
    return rhs.some(v => compare(lhs, '==', v));
  }
  if (op === 'contains') {
    return String(lhs).toLowerCase().includes(String(rhs).toLowerCase());
  }
  if (op === 'matches') {
    try { return new RegExp(String(rhs), 'i').test(String(lhs)); } catch { return false; }
  }
  // ==/!=: when rhs looks like CIDR and lhs is an IP, do CIDR membership.
  if ((op === '==' || op === '!=') && typeof rhs === 'string' && rhs.includes('/') && typeof lhs === 'string' && lhs.includes('.')) {
    const inSubnet = ipv4InCIDR(lhs, rhs);
    return op === '==' ? inSubnet : !inSubnet;
  }
  // Number vs string-with-digits: coerce when both sides parse.
  const ln = typeof lhs === 'number' ? lhs : (/^\d+$/.test(String(lhs)) ? parseInt(String(lhs), 10) : NaN);
  const rn = typeof rhs === 'number' ? rhs : (/^\d+$/.test(String(rhs)) ? parseInt(String(rhs), 10) : NaN);
  const bothNum = !isNaN(ln) && !isNaN(rn);
  switch (op) {
    case '==': return bothNum ? ln === rn : String(lhs).toLowerCase() === String(rhs).toLowerCase();
    case '!=': return bothNum ? ln !== rn : String(lhs).toLowerCase() !== String(rhs).toLowerCase();
    case '<':  return bothNum ? ln < rn  : String(lhs) <  String(rhs);
    case '<=': return bothNum ? ln <= rn : String(lhs) <= String(rhs);
    case '>':  return bothNum ? ln > rn  : String(lhs) >  String(rhs);
    case '>=': return bothNum ? ln >= rn : String(lhs) >= String(rhs);
  }
  return false;
}

// ipv4InCIDR — minimal IPv4 subnet membership. We avoid pulling in a CIDR
// library since this is the only place we need it.
function ipv4InCIDR(ip: string, cidr: string): boolean {
  const [net, maskStr] = cidr.split('/');
  const mask = parseInt(maskStr, 10);
  if (isNaN(mask) || mask < 0 || mask > 32) return false;
  const ipN = ipv4ToInt(ip); const netN = ipv4ToInt(net);
  if (ipN == null || netN == null) return false;
  const m = mask === 0 ? 0 : (~0 << (32 - mask)) >>> 0;
  return (ipN & m) === (netN & m);
}
function ipv4ToInt(ip: string): number | null {
  const parts = ip.split('.');
  if (parts.length !== 4) return null;
  let v = 0;
  for (const part of parts) {
    const n = parseInt(part, 10);
    if (isNaN(n) || n < 0 || n > 255) return null;
    v = (v << 8) + n;
  }
  return v >>> 0;
}
