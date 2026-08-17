import React, {useEffect, useMemo, useRef, useState} from 'react';
import {adb} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {Icon} from '../icons';
import {Badge, CommandPreview, FeatureNotice, SearchInput, confirmDialog, showToast} from '../ui';
import {useDeviceData, mutateData, getCached} from '../cache';

type Family = 'ipv4' | 'ipv6';
type Table = 'filter' | 'nat' | 'mangle' | 'raw';
const TABLES: Table[] = ['filter', 'nat', 'mangle', 'raw'];
const POLICIES = ['ACCEPT', 'DROP', 'REJECT'] as const;
const SAFE_POLICY_TIMEOUT_MS = 30_000;

export function IptablesScreen({device}: {device: adb.Device}) {
  const [family, setFamily] = useState<Family>('ipv4');
  const [table, setTable] = useState<Table>('filter');
  const [chain, setChain] = useState<string>('');
  const [showRaw, setShowRaw] = useState(false);
  const [raw, setRaw] = useState('');
  const [addOpen, setAddOpen] = useState(false);
  const safetyTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Cached-first load: show the last ruleset instantly, revalidate in the
  // background. Probe first so a device without iptables/nft shows a clean
  // banner instead of a "List failed" toast; listing needs root.
  const cacheKey = device?.id ? `iptables:${device.id}:${family}:${table}` : null;
  const {data, loading, refreshing, refresh} = useDeviceData(
    cacheKey,
    async (): Promise<{info: adb.IPTBackendInfo | null; snap: adb.IPTSnapshot | null}> => {
      const pb = await API.ProbeIptables(device.id, family);
      if (!pb?.available || !device.root) return {info: pb, snap: null};
      const sn = await API.ListIptables(device.id, family, table);
      return {info: pb, snap: sn};
    },
    {staleMs: 15000},
  );
  const info = data?.info || null;
  const snap = data?.snap || null;
  // Apply a mutation's returned snapshot straight into the cache (no re-list),
  // preserving the freshest backend info (read from the cache, not a stale closure).
  const applySnap = (sn: adb.IPTSnapshot) => {
    if (!cacheKey) return;
    const latest = getCached<{info: adb.IPTBackendInfo | null; snap: adb.IPTSnapshot | null}>(cacheKey);
    mutateData(cacheKey, {info: latest?.info ?? info, snap: sn});
  };

  useEffect(() => {
    if (snap?.chains?.length && !snap.chains.some(c => c.name === chain)) {
      setChain(snap.chains[0].name);
    }
  }, [snap, chain]);

  // What each action here will run, from Go: the table, the binary (iptables vs
  // ip6tables) and the su form all depend on state the frontend should not be
  // guessing at (CLAUDE.md §4.1).
  const [cmds, setCmds] = useState<adb.IptablesCommands | null>(null);
  useEffect(() => {
    if (!device?.id) { setCmds(null); return; }
    let live = true;
    API.IptablesCommands(device.id, {family, table, chain, pos: 0, num: 0, policy: '', spec: []} as adb.IptablesCommandRequest)
      .then(c => { if (live) setCmds(c); })
      .catch(() => { if (live) setCmds(null); });
    return () => { live = false; };
  }, [device?.id, family, table, chain]);

  // A one-off render for an action whose argument the user just chose (a rule
  // number, a policy, a spec). Cheap: string work, no device access.
  const commandsFor = (extra: Partial<adb.IptablesCommandRequest>) =>
    API.IptablesCommands(device.id, {
      family, table, chain, pos: 0, num: 0, policy: '', spec: [], ...extra,
    } as adb.IptablesCommandRequest).catch(() => null);

  const selected = useMemo(
    () => snap?.chains?.find(c => c.name === chain) || snap?.chains?.[0] || null,
    [snap, chain]
  );

  async function del(num: number) {
    if (!selected) return;
    const c = await commandsFor({chain: selected.name, num});
    const ok = await confirmDialog({
      title: `Delete rule #${num} in ${selected.name}?`,
      body: <CommandPreview commands={c?.deleteRule ?? []} defaultOpen/>,
      confirmLabel: 'Delete', danger: true,
    });
    if (!ok) return;
    try {
      const sn = await API.DeleteIptablesRule(device.id, family, table, selected.name, num);
      applySnap(sn);
      showToast({title: 'Rule deleted', body: `${selected.name} #${num}`, kind: 'ok'});
    } catch (e) { showToast({title: 'Delete failed', body: String(e), kind: 'err'}); }
  }

  async function flush() {
    if (!selected) return;
    const ok = await confirmDialog({
      title: `Flush ${selected.name}?`,
      body: <>
        Removes every rule in this chain. The default policy is unchanged.
        <CommandPreview commands={cmds?.flushChain ?? []} defaultOpen/>
      </>,
      confirmLabel: 'Flush', danger: true,
    });
    if (!ok) return;
    try {
      const sn = await API.FlushIptables(device.id, family, table, selected.name);
      applySnap(sn);
      showToast({title: 'Chain flushed', body: selected.name, kind: 'ok'});
    } catch (e) { showToast({title: 'Flush failed', body: String(e), kind: 'err'}); }
  }

  async function changePolicy(policy: string) {
    if (!selected) return;
    const dangerous = policy === 'DROP' || policy === 'REJECT';
    const c = await commandsFor({chain: selected.name, policy});
    const ok = await confirmDialog({
      title: `Set policy of ${selected.name} to ${policy}?`,
      body: <>
        {dangerous && 'Dangerous on INPUT/OUTPUT — can disconnect adbq from the device. A 30-second safety timer will auto-revert if you don\'t cancel it.'}
        <CommandPreview commands={c?.policy ?? []} defaultOpen/>
      </>,
      confirmLabel: dangerous ? `Set ${policy} (auto-revert in 30s)` : `Set ${policy}`,
      danger: dangerous,
    });
    if (!ok) return;
    try {
      await API.SetIptablesPolicy(device.id, family, table, selected.name, policy);
      await refresh();
      showToast({title: 'Policy applied', body: `${selected.name} → ${policy}`, kind: dangerous ? 'info' : 'ok'});
      if (dangerous) {
        if (safetyTimer.current) clearTimeout(safetyTimer.current);
        safetyTimer.current = setTimeout(async () => {
          try {
            await API.UndoIptables(device.id, family);
            showToast({title: 'Safety timer fired', body: 'Reverted to last snapshot', kind: 'info'});
            await refresh();
          } catch (e) { showToast({title: 'Auto-revert failed', body: String(e), kind: 'err'}); }
        }, SAFE_POLICY_TIMEOUT_MS);
      }
    } catch (e) { showToast({title: 'Policy failed', body: String(e), kind: 'err'}); }
  }

  function cancelSafety() {
    if (safetyTimer.current) { clearTimeout(safetyTimer.current); safetyTimer.current = null;
      showToast({title: 'Safety timer cancelled', body: 'Policy stays as-is', kind: 'ok'}); }
  }

  async function undo() {
    try {
      const sn = await API.UndoIptables(device.id, family);
      applySnap(sn);
      showToast({title: 'Undone', body: 'Reverted to last snapshot', kind: 'ok'});
    } catch (e) { showToast({title: 'Undo failed', body: String(e), kind: 'err'}); }
  }

  async function exportRules() {
    try {
      const blob = await API.ExportIptables(device.id, family);
      setRaw(blob);
      setShowRaw(true);
      try { await navigator.clipboard?.writeText(blob); showToast({title: 'Exported', body: 'Copied to clipboard', kind: 'ok'}); }
      catch { showToast({title: 'Exported', body: 'Open raw view to copy', kind: 'ok'}); }
    } catch (e) { showToast({title: 'Export failed', body: String(e), kind: 'err'}); }
  }
  async function importRules() {
    const ok = await confirmDialog({
      title: 'Apply iptables-restore blob?',
      body: <>
        Overwrites the entire ruleset for this family. A snapshot is kept so you can Undo.
        <CommandPreview commands={cmds?.import ?? []} defaultOpen/>
      </>,
      confirmLabel: 'Apply',
      danger: true,
    });
    if (!ok) return;
    try {
      await API.ImportIptables(device.id, family, raw);
      await refresh();
      showToast({title: 'Imported', body: 'Restored from blob', kind: 'ok'});
    } catch (e) { showToast({title: 'Import failed', body: String(e), kind: 'err'}); }
  }

  async function createChain() {
    const name = window.prompt('New chain name (letters/digits/_/-):');
    if (!name) return;
    try {
      await API.CreateIptablesChain(device.id, family, table, name);
      await refresh();
      setChain(name);
    } catch (e) { showToast({title: 'Create chain failed', body: String(e), kind: 'err'}); }
  }
  async function deleteChain() {
    if (!selected || ['INPUT', 'OUTPUT', 'FORWARD', 'PREROUTING', 'POSTROUTING'].includes(selected.name)) return;
    const ok = await confirmDialog({
      title: `Delete user chain ${selected.name}?`,
      body: <>
        The chain must be empty.
        <CommandPreview commands={cmds?.dropChain ?? []} defaultOpen/>
      </>,
      confirmLabel: 'Delete', danger: true,
    });
    if (!ok) return;
    try {
      await API.DeleteIptablesChain(device.id, family, table, selected.name);
      await refresh();
    } catch (e) { showToast({title: 'Delete chain failed', body: String(e), kind: 'err'}); }
  }

  if (!device.root) {
    return (
      <div className='screen'>
        <div className='screen-header'><h1>iptables</h1></div>
        <FeatureNotice state={{
          kind: 'requires-root',
          what: info && !info.available
            ? 'iptables/nft was not found on this device, and reading firewall rules'
            : 'Reading and editing firewall rules',
        }}/>
      </div>
    );
  }

  return (
    <div className='screen'>
      <div className='screen-header'>
        <h1>iptables{info?.mode && <span className='subtitle mono'>{info.mode}</span>}</h1>
        <select className='btn sm' value={family} onChange={e => setFamily(e.target.value as Family)}>
          <option value='ipv4'>IPv4</option>
          <option value='ipv6'>IPv6</option>
        </select>
        <select className='btn sm' value={table} onChange={e => setTable(e.target.value as Table)}>
          {TABLES.map(t => <option key={t} value={t}>{t}</option>)}
        </select>
        <div className='spacer' style={{flex: 1}}/>
        {safetyTimer.current && <button className='btn sm primary' onClick={cancelSafety}>Cancel safety timer</button>}
        <button className='btn sm' onClick={undo} title='Revert to previous snapshot'><Icon.Refresh/>Undo</button>
        <button className='btn sm' onClick={exportRules}><Icon.Download/>Export</button>
        <button className='btn sm' onClick={() => setShowRaw(true)}><Icon.Upload/>Raw / Import</button>
        <button className='btn sm' onClick={() => refresh()}><Icon.Refresh className={refreshing ? 'spin' : ''}/>Reload</button>
      </div>

      {cmds && (
        <div style={{margin: '0 18px 10px', display: 'grid', gap: 4}}>
          <CommandPreview commands={cmds.list ?? []} label='List'/>
          <CommandPreview commands={cmds.save ?? []} label='Export'/>
          <CommandPreview commands={cmds.undo ?? []} label='Undo'/>
        </div>
      )}

      {info && !info.available && (
        <div className='card' style={{margin: '0 18px 12px', padding: 10, borderColor: 'var(--err)'}}>
          <strong>iptables not found on device.</strong> Most Android 10+ ROMs ship it at <span className='mono'>/system/bin/iptables</span>; if yours doesn't, install via Magisk module. (No <span className='mono'>nft</span> either, so the ruleset can't be read.)
        </div>
      )}

      {info?.readOnly && (
        <div className='card' style={{margin: '0 18px 12px', padding: 10, borderColor: 'var(--warn)'}}>
          <strong>Read-only (nftables).</strong> This device has no iptables binary, so the ruleset is shown via <span className='mono'>nft list ruleset</span>. Viewing works; adding, deleting and flushing rules are disabled.
        </div>
      )}

      <div className='ipt-layout'>
        <div className='ipt-chains'>
          <div className='spread' style={{padding: '6px 8px'}}>
            <span className='muted' style={{fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.05em'}}>Chains</span>
            <button className='btn sm' onClick={createChain} title='Create user chain'><Icon.Plus/></button>
          </div>
          {snap?.chains?.map(c => (
            <div key={c.name} className={`ipt-chain-row${chain === c.name ? ' selected' : ''}`} onClick={() => setChain(c.name)}>
              <span className='name'>{c.name}</span>
              <Badge kind={c.policy === 'DROP' || c.policy === 'REJECT' ? 'err' : c.policy === 'ACCEPT' ? 'ok' : undefined}>
                {c.policy}
              </Badge>
              <span className='muted mono' style={{fontSize: 10}}>{c.rules?.length || 0}</span>
            </div>
          ))}
          {loading && <div className='muted' style={{padding: 12}}>Loading…</div>}
        </div>

        <div className='ipt-rules'>
          <div className='spread' style={{padding: '8px 12px', borderBottom: '1px solid var(--border)'}}>
            <div style={{display: 'flex', alignItems: 'center', gap: 8}}>
              <strong>{selected?.name || '—'}</strong>
              {selected && <Badge>{selected.rules?.length || 0} rules</Badge>}
            </div>
            <div style={{display: 'flex', gap: 6}}>
              {selected && ['INPUT', 'OUTPUT', 'FORWARD', 'PREROUTING', 'POSTROUTING'].includes(selected.name) && (
                <select className='btn sm' value={selected.policy} onChange={e => changePolicy(e.target.value)}>
                  {POLICIES.map(p => <option key={p} value={p}>policy: {p}</option>)}
                </select>
              )}
              <button className='btn sm' onClick={() => setAddOpen(true)} disabled={!selected}><Icon.Plus/>Add rule</button>
              <button className='btn sm' onClick={flush} disabled={!selected}>Flush</button>
              {selected && !['INPUT', 'OUTPUT', 'FORWARD', 'PREROUTING', 'POSTROUTING'].includes(selected.name) &&
                <button className='btn sm danger' onClick={deleteChain}><Icon.Trash/>Drop chain</button>}
            </div>
          </div>
          <div className='ipt-rule-list'>
            <div className='ipt-rule-row ipt-rule-head'>
              <span>#</span><span>pkts</span><span>bytes</span><span>target</span><span>proto</span>
              <span>in</span><span>out</span><span>src</span><span>dst</span><span>extra</span><span/>
            </div>
            {selected?.rules?.map(r => (
              <div key={r.num} className='ipt-rule-row'>
                <span className='mono'>{r.num}</span>
                <span className='mono'>{r.pkts}</span>
                <span className='mono'>{r.bytes}</span>
                <span><Badge kind={r.target === 'DROP' || r.target === 'REJECT' ? 'err' : r.target === 'ACCEPT' ? 'ok' : undefined}>{r.target}</Badge></span>
                <span className='mono'>{r.proto}</span>
                <span className='mono'>{r.inIface}</span>
                <span className='mono'>{r.outIface}</span>
                <span className='mono'>{r.source}</span>
                <span className='mono'>{r.dest}</span>
                <span className='mono truncate' title={r.extra}>{r.extra}</span>
                <span><button className='btn sm danger' onClick={() => del(r.num)}><Icon.Trash/></button></span>
              </div>
            ))}
            {(!selected || !selected.rules?.length) &&
              <div className='muted' style={{padding: 16, fontSize: 12}}>No rules in this chain.</div>}
          </div>
        </div>
      </div>

      {addOpen && selected && <AddRuleModal
        chain={selected.name}
        preview={(spec, pos) => commandsFor({chain: selected.name, spec, pos}).then(c => c?.addRule ?? [])}
        onClose={() => setAddOpen(false)}
        onSubmit={async (spec, pos) => {
          try {
            const sn = pos > 0
              ? await API.InsertIptablesRule(device.id, family, table, selected.name, pos, spec)
              : await API.AppendIptablesRule(device.id, family, table, selected.name, spec);
            applySnap(sn);
            setAddOpen(false);
            showToast({title: 'Rule added', body: spec.join(' '), kind: 'ok', mono: true});
          } catch (e) { showToast({title: 'Add rule failed', body: String(e), kind: 'err'}); }
        }}
      />}

      {showRaw && <RawModal
        text={raw}
        onChange={setRaw}
        onClose={() => setShowRaw(false)}
        onApply={importRules}
      />}
    </div>
  );
}

function AddRuleModal({chain, preview, onClose, onSubmit}: {
  chain: string;
  /** Renders the command this form would run; from the backend, per §4.1. */
  preview: (spec: string[], pos: number) => Promise<string[]>;
  onClose: () => void;
  onSubmit: (spec: string[], pos: number) => void;
}) {
  const [target, setTarget] = useState('ACCEPT');
  const [proto, setProto] = useState('any');
  const [src, setSrc] = useState('');
  const [dst, setDst] = useState('');
  const [sport, setSport] = useState('');
  const [dport, setDport] = useState('');
  const [inIf, setInIf] = useState('');
  const [outIf, setOutIf] = useState('');
  const [comment, setComment] = useState('');
  const [pos, setPos] = useState('');

  function build(): string[] {
    const spec: string[] = [];
    if (proto && proto !== 'any') { spec.push('-p', proto); }
    if (src) { spec.push('-s', src); }
    if (dst) { spec.push('-d', dst); }
    if (inIf) { spec.push('-i', inIf); }
    if (outIf) { spec.push('-o', outIf); }
    if (sport) { spec.push('--sport', sport); }
    if (dport) { spec.push('--dport', dport); }
    if (comment) { spec.push('-m', 'comment', '--comment', comment); }
    spec.push('-j', target);
    return spec;
  }
  // Re-rendered by the backend as the form changes: the table, the binary and
  // whether this is -A or -I N are all decided there.
  const [cmd, setCmd] = useState<string[]>([]);
  const spec = build();
  const specKey = spec.join(' ') + '|' + pos;
  useEffect(() => {
    let live = true;
    preview(spec, parseInt(pos, 10) || 0)
      .then(c => { if (live) setCmd(c); })
      .catch(() => { if (live) setCmd([]); });
    return () => { live = false; };
  }, [specKey]);

  return (
    <div className='modal-backdrop' onClick={onClose}>
      <div className='modal' onClick={e => e.stopPropagation()} style={{minWidth: 520, maxWidth: 720}}>
        <div className='modal-header'><span className='title'>Add rule to {chain}</span></div>
        <div className='modal-body' style={{display: 'grid', gridTemplateColumns: '120px 1fr', gap: 8, fontSize: 12}}>
          <label className='muted'>Target</label>
          <select className='btn sm' value={target} onChange={e => setTarget(e.target.value)}>
            <option>ACCEPT</option><option>DROP</option><option>REJECT</option>
            <option>RETURN</option><option>LOG</option><option>DNAT</option><option>SNAT</option><option>MASQUERADE</option><option>REDIRECT</option>
          </select>
          <label className='muted'>Protocol</label>
          <select className='btn sm' value={proto} onChange={e => setProto(e.target.value)}>
            <option>any</option><option>tcp</option><option>udp</option><option>icmp</option><option>icmpv6</option><option>all</option>
          </select>
          <label className='muted'>Source</label>
          <input className='btn sm mono' value={src} onChange={e => setSrc(e.target.value)} placeholder='IP/CIDR (e.g. 10.0.0.0/24)'/>
          <label className='muted'>Destination</label>
          <input className='btn sm mono' value={dst} onChange={e => setDst(e.target.value)} placeholder='IP/CIDR'/>
          <label className='muted'>Source port</label>
          <input className='btn sm mono' value={sport} onChange={e => setSport(e.target.value)} placeholder='80 or 1000:2000'/>
          <label className='muted'>Dest port</label>
          <input className='btn sm mono' value={dport} onChange={e => setDport(e.target.value)} placeholder='443'/>
          <label className='muted'>In interface</label>
          <input className='btn sm mono' value={inIf} onChange={e => setInIf(e.target.value)} placeholder='wlan0'/>
          <label className='muted'>Out interface</label>
          <input className='btn sm mono' value={outIf} onChange={e => setOutIf(e.target.value)} placeholder='rmnet0'/>
          <label className='muted'>Comment</label>
          <input className='btn sm' value={comment} onChange={e => setComment(e.target.value)} placeholder='Why this rule exists'/>
          <label className='muted'>Position</label>
          <input className='btn sm mono' value={pos} onChange={e => setPos(e.target.value)} placeholder='blank = append (-A); number = insert at -I N'/>
        </div>
        <div style={{margin: '12px 16px 0'}}>
          <CommandPreview commands={cmd} defaultOpen/>
        </div>
        <div className='modal-footer'>
          <button className='btn' onClick={onClose}>Cancel</button>
          <button className='btn primary' onClick={() => onSubmit(build(), parseInt(pos, 10) || 0)}>Add</button>
        </div>
      </div>
    </div>
  );
}

function RawModal({text, onChange, onClose, onApply}: {text: string; onChange: (v: string) => void; onClose: () => void; onApply: () => void}) {
  return (
    <div className='modal-backdrop' onClick={onClose}>
      <div className='modal' onClick={e => e.stopPropagation()} style={{minWidth: 640, maxWidth: 900, width: '70vw'}}>
        <div className='modal-header'><span className='title'>iptables-save / restore</span></div>
        <textarea
          value={text}
          onChange={e => onChange(e.target.value)}
          spellCheck={false}
          className='mono'
          style={{width: '100%', height: '60vh', fontSize: 11, padding: 10, background: 'var(--bg-inset)', color: 'var(--text)', border: '1px solid var(--border)', borderRadius: 4}}
        />
        <div className='modal-footer'>
          <button className='btn' onClick={onClose}>Close</button>
          <button className='btn primary' onClick={onApply} disabled={!text.trim()}>Apply (iptables-restore)</button>
        </div>
      </div>
    </div>
  );
}
