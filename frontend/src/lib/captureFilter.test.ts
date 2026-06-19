// Minimal smoke tests for the Wireshark-style display filter. Wired so
// `npx vitest` would run them if vitest is added; today they double as a
// type-check-only safety net (the file compiles, the predicates evaluate
// against the synthetic fixtures below).

import {parseFilter, Packet} from './captureFilter';

const pkts: Packet[] = [
  {no: 1, length: 80,  srcIP: '10.0.0.5',  dstIP: '1.1.1.1',  srcPort: 49152, dstPort: 443, proto: 'TLS',  info: 'ClientHello sni=example.com'},
  {no: 2, length: 80,  srcIP: '1.1.1.1',   dstIP: '10.0.0.5', srcPort: 443,   dstPort: 49152, proto: 'TLS',  info: 'Application Data'},
  {no: 3, length: 120, srcIP: '10.0.0.5',  dstIP: '8.8.8.8',  srcPort: 51234, dstPort: 53,  proto: 'DNS',  info: 'Query google.com A'},
  {no: 4, length: 1500,srcIP: '10.0.0.5',  dstIP: '93.184.216.34', srcPort: 49200, dstPort: 80,  proto: 'HTTP', info: 'GET /index.html HTTP/1.1'},
  {no: 5, length: 64,  srcIP: '10.0.0.5',  dstIP: '8.8.8.8',  srcPort: 0,     dstPort: 0,   proto: 'ICMP', info: 'EchoRequest'},
];

function match(expr: string): number[] {
  const {pred, error} = parseFilter(expr);
  if (error) throw new Error(`${expr}: ${error}`);
  return pkts.filter(pred).map(p => p.no);
}

function assert(name: string, expr: string, want: number[]) {
  const got = match(expr);
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    // eslint-disable-next-line no-console
    console.error(`FAIL ${name}: ${expr} → ${got} want ${want}`);
  }
}

assert('tcp.port equality',    'tcp.port == 443',                              [1, 2]);
assert('dns boolean',          'dns',                                          [3]);
assert('ip.src equality',      'ip.src == 10.0.0.5',                           [1, 3, 4, 5]);
assert('ip.addr either',       'ip.addr == 1.1.1.1',                           [1, 2]);
assert('ip.addr CIDR',         'ip.addr == 10.0.0.0/24',                       [1, 2, 3, 4, 5]);
assert('not + and',            'tls and not ip.src == 1.1.1.1',                [1]);
assert('contains',             'info contains "google"',                       [3]);
assert('http alias',           'http',                                         [4]);
assert('in set',               'tcp.port in {80, 443}',                        [1, 2, 4]);
assert('frame.len threshold',  'frame.len > 100',                              [3, 4]);
assert('dns.qry.name contains','dns.qry.name contains "google"',               [3]);
assert('tls SNI',              'tls.handshake.extensions_server_name contains "example"', [1]);
assert('or short-circuit',     'dns or icmp',                                  [3, 5]);
assert('paren precedence',     '(tcp.port == 443 or tcp.port == 80) and not http', [1, 2]);

export {};
