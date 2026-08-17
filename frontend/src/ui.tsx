import React, {useEffect, useRef, useState, useCallback} from 'react';
import {Icon} from './icons';
import {ToastMsg} from './types';

// ─── Theme tokens (light/dark/system) ──────────────────────────────────────

export type ThemeMode = 'light' | 'dark' | 'system';

export function useTheme() {
  const [mode, setMode] = useState<ThemeMode>(() =>
    (localStorage.getItem('adbq.theme') as ThemeMode) || 'system');
  const [accent, setAccent] = useState<string>(() =>
    localStorage.getItem('adbq.accent') || '#a07cf7');
  const [systemDark, setSystemDark] = useState(
    typeof window !== 'undefined' && window.matchMedia('(prefers-color-scheme: dark)').matches);

  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    const h = (e: MediaQueryListEvent) => setSystemDark(e.matches);
    mq.addEventListener('change', h);
    return () => mq.removeEventListener('change', h);
  }, []);

  const theme = mode === 'system' ? (systemDark ? 'dark' : 'light') : mode;

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
    const r = document.documentElement.style;
    r.setProperty('--accent', accent);
    r.setProperty('--accent-soft', hexToRGBA(accent, 0.14));
    r.setProperty('--accent-soft-strong', hexToRGBA(accent, 0.22));
    r.setProperty('--accent-strong', darken(accent, 0.08));
    localStorage.setItem('adbq.theme', mode);
    localStorage.setItem('adbq.accent', accent);
  }, [theme, accent, mode]);

  return {mode, setMode, theme, accent, setAccent};
}

function hexToRGBA(hex: string, a: number) {
  const m = hex.replace('#', '');
  const n = parseInt(m.length === 3 ? m.split('').map(c => c + c).join('') : m, 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${a})`;
}
function darken(hex: string, amt: number) {
  const m = hex.replace('#', '');
  const n = parseInt(m.length === 3 ? m.split('').map(c => c + c).join('') : m, 16);
  const fn = (x: number) => Math.max(0, Math.round(x * (1 - amt)));
  const r = fn((n >> 16) & 255), g = fn((n >> 8) & 255), b = fn(n & 255);
  return '#' + [r, g, b].map(x => x.toString(16).padStart(2, '0')).join('');
}

// ─── Toasts ───────────────────────────────────────────────────────────────

let toastSeq = 0;
let toastSub: ((m: ToastMsg) => void) | null = null;
export function showToast(t: Omit<ToastMsg, 'id'>) {
  toastSeq++;
  toastSub?.({...t, id: toastSeq});
}

export function ToastHost() {
  const [items, setItems] = useState<ToastMsg[]>([]);
  useEffect(() => {
    toastSub = (m) => {
      setItems((arr) => [...arr, m]);
      const ttl = m.ttl ?? (m.actions ? 7000 : 3800);
      setTimeout(() => setItems((arr) => arr.filter(x => x.id !== m.id)), ttl);
    };
    return () => { toastSub = null; };
  }, []);
  return (
    <div className='toast-host'>
      {items.map(t => (
        <div key={t.id} className={`toast ${t.kind}`}>
          <span className='ic'>
            {t.kind === 'ok' && <Icon.Activity width={14} height={14}/>}
            {t.kind === 'err' && <Icon.X width={14} height={14}/>}
            {t.kind === 'info' && <Icon.Activity width={14} height={14}/>}
          </span>
          <div style={{flex: 1, minWidth: 0}}>
            <div style={{fontWeight: 600}}>{t.title}</div>
            {t.body && <div className={t.mono ? 'mono subtle' : 'subtle'} style={{fontSize: 11, wordBreak: 'break-all'}}>{t.body}</div>}
          </div>
          {t.actions && t.actions.length > 0 && (
            <div style={{display: 'flex', gap: 4, marginLeft: 8}}>
              {t.actions.map((a, i) => (
                <button key={i} className='btn sm' onClick={() => { a.onClick(); setItems(arr => arr.filter(x => x.id !== t.id)); }}>
                  {a.label}
                </button>
              ))}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}

// ─── Common bits ──────────────────────────────────────────────────────────

export function Badge({children, kind}: {children: React.ReactNode; kind?: string}) {
  return <span className={`badge${kind ? ' ' + kind : ''}`}>{children}</span>;
}

export function IconBtn({title, onClick, active, children}:{title?: string; onClick?: ()=>void; active?: boolean; children: React.ReactNode}) {
  return <button className={`iconbtn${active ? ' active' : ''}`} title={title} onClick={onClick}>{children}</button>;
}

export function SearchInput({value, onChange, placeholder, autoFocus, style}:{value: string; onChange:(v:string)=>void; placeholder?: string; autoFocus?: boolean; style?: React.CSSProperties}) {
  return (
    <span className='search-wrap' style={style}>
      <Icon.Search width={14} height={14}/>
      <input className='input search' value={value} onChange={e => onChange(e.target.value)} placeholder={placeholder} autoFocus={autoFocus}/>
    </span>
  );
}

export function Switch({on, onChange}: {on: boolean; onChange:(v:boolean)=>void}) {
  return <button className={`switch${on ? ' on' : ''}`} onClick={() => onChange(!on)} aria-pressed={on}/>;
}

export function Spacer() { return <div className='spacer' style={{flex: 1}}/>; }

// ─── Modal ────────────────────────────────────────────────────────────────

export function Modal({open, onClose, title, children, footer, width}:{open: boolean; onClose:()=>void; title: string; children: React.ReactNode; footer?: React.ReactNode; width?: number}) {
  if (!open) return null;
  return (
    <div className='modal-backdrop' onClick={onClose}>
      <div className='modal' style={{width: width || 560}} onClick={e => e.stopPropagation()}>
        <div className='modal-header'>
          <div className='title'>{title}</div>
          <IconBtn onClick={onClose} title='Close'><Icon.X width={14} height={14}/></IconBtn>
        </div>
        <div className='modal-body'>{children}</div>
        {footer && <div className='modal-footer'>{footer}</div>}
      </div>
    </div>
  );
}

// ─── Confirm dialog (replacement for native confirm) ─────────────────────

let confirmFn: ((opts: ConfirmOpts) => Promise<boolean>) | null = null;
export interface ConfirmOpts {
  title: string;
  body?: React.ReactNode;
  confirmLabel?: string;
  danger?: boolean;
}
export function confirmDialog(opts: ConfirmOpts): Promise<boolean> {
  if (!confirmFn) return Promise.resolve(window.confirm(opts.title));
  return confirmFn(opts);
}

export function ConfirmHost() {
  const [opts, setOpts] = useState<ConfirmOpts | null>(null);
  const [resolver, setResolver] = useState<((v: boolean) => void) | null>(null);
  useEffect(() => {
    confirmFn = (o) => new Promise<boolean>((resolve) => {
      setOpts(o);
      setResolver(() => resolve);
    });
    return () => { confirmFn = null; };
  }, []);
  const finish = (v: boolean) => {
    resolver?.(v);
    setOpts(null);
    setResolver(null);
  };
  if (!opts) return null;
  return (
    <Modal open={!!opts} onClose={() => finish(false)} title={opts.title}
           footer={<>
             <button className='btn' onClick={() => finish(false)}>Cancel</button>
             <button className={`btn ${opts.danger ? 'danger' : 'primary'}`} onClick={() => finish(true)}>{opts.confirmLabel || 'Confirm'}</button>
           </>}>
      {opts.body && <div style={{fontSize: 13}}>{opts.body}</div>}
    </Modal>
  );
}

// ─── Prompt dialog (replacement for native prompt) ───────────────────────

let promptFn: ((opts: PromptOpts) => Promise<string | null>) | null = null;
export interface PromptOpts {
  title: string;
  label: string;
  defaultValue?: string;
  placeholder?: string;
}
export function promptDialog(opts: PromptOpts): Promise<string | null> {
  if (!promptFn) {
    const v = window.prompt(opts.title, opts.defaultValue);
    return Promise.resolve(v);
  }
  return promptFn(opts);
}

export function PromptHost() {
  const [opts, setOpts] = useState<PromptOpts | null>(null);
  const [value, setValue] = useState('');
  const [resolver, setResolver] = useState<((v: string | null) => void) | null>(null);
  useEffect(() => {
    promptFn = (o) => new Promise<string | null>((resolve) => {
      setOpts(o);
      setValue(o.defaultValue || '');
      setResolver(() => resolve);
    });
    return () => { promptFn = null; };
  }, []);
  const finish = (v: string | null) => {
    resolver?.(v);
    setOpts(null);
    setResolver(null);
  };
  if (!opts) return null;
  return (
    <Modal open={!!opts} onClose={() => finish(null)} title={opts.title}
           footer={<>
             <button className='btn' onClick={() => finish(null)}>Cancel</button>
             <button className='btn primary' onClick={() => finish(value)}>OK</button>
           </>}>
      <div className='field'>
        <label>{opts.label}</label>
        <input className='input mono' autoFocus value={value} placeholder={opts.placeholder}
               onChange={e => setValue(e.target.value)}
               onKeyDown={e => e.key === 'Enter' && finish(value)}/>
      </div>
    </Modal>
  );
}

// ─── Combobox primitive (searchable picker) ──────────────────────────────

export interface ComboItem {
  value: string;
  label: string;
  sub?: string;
  icon?: React.ReactNode;
  badge?: React.ReactNode;
}
export function Combobox({value, onChange, items, placeholder, width, footer, clearable}:{
  value: string;
  onChange: (v: string) => void;
  items: ComboItem[];
  placeholder?: string;
  width?: number;
  footer?: React.ReactNode;
  /**
   * Show an × on the trigger that resets the selection to "". Worth setting
   * whenever the empty value means "no filter": the option for it can be typed
   * out of the list by the search box, leaving no obvious way back.
   */
  clearable?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState('');
  // Index of the keyboard-highlighted option within the filtered list.
  const [active, setActive] = useState(0);
  const ref = useClickOutside<HTMLDivElement>(open, () => setOpen(false));
  const listRef = useRef<HTMLDivElement>(null);
  const sel = items.find(i => i.value === value);
  const filt = !q ? items : items.filter(i => i.label.toLowerCase().includes(q.toLowerCase()) || i.value.toLowerCase().includes(q.toLowerCase()));
  const at = Math.min(active, Math.max(0, filt.length - 1));

  function openAtSelection() {
    const i = filt.findIndex(x => x.value === value);
    setActive(i >= 0 ? i : 0);
    setOpen(true);
  }

  // Follow the highlight with the scroll position, otherwise arrowing past the
  // bottom of the list walks an option the user cannot see.
  useEffect(() => {
    if (!open) return;
    listRef.current?.querySelector('.combo-opt.active')?.scrollIntoView({block: 'nearest'});
  }, [at, open]);

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (!filt.length) return;
      const step = e.key === 'ArrowDown' ? 1 : -1;
      setActive((at + step + filt.length) % filt.length);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const pick = filt[at];
      if (pick) { onChange(pick.value); setOpen(false); setQ(''); }
    } else if (e.key === 'Escape') {
      e.preventDefault();
      setOpen(false);
      setQ('');
    }
  }

  return (
    <div className='combo' ref={ref}>
      <button className='combo-trigger' style={width ? {minWidth: width} : undefined}
              onClick={() => (open ? setOpen(false) : openAtSelection())}>
        <span className='val'>{sel?.label || placeholder || 'Select…'}</span>
        {clearable && value
          // A <button> cannot nest inside the trigger button, so this is a
          // span carrying the same affordances.
          ? <span className='combo-clear' role='button' tabIndex={0} title='Clear filter'
                  onClick={e => { e.stopPropagation(); onChange(''); setQ(''); setOpen(false); }}
                  onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.stopPropagation(); e.preventDefault(); onChange(''); setQ(''); setOpen(false); } }}>
              <Icon.X width={12} height={12}/>
            </span>
          : <Icon.Search width={12} height={12}/>}
      </button>
      {open && (
        <div className='combo-pop' style={width ? {minWidth: width + 40} : undefined}>
          <div className='combo-search'>
            <Icon.Search width={13} height={13}/>
            <input autoFocus value={q} placeholder='Search…' onKeyDown={onKeyDown}
                   onChange={e => { setQ(e.target.value); setActive(0); }}/>
          </div>
          <div className='combo-list' ref={listRef}>
            {filt.map((i, idx) => (
              <div key={i.value} className={`combo-opt${i.value === value ? ' selected' : ''}${idx === at ? ' active' : ''}`}
                   onMouseEnter={() => setActive(idx)}
                   onClick={() => { onChange(i.value); setOpen(false); setQ(''); }}>
                {i.icon}
                <div className='text'>
                  <div className='name'>{i.label}</div>
                  {i.sub && <div className='sub'>{i.sub}</div>}
                </div>
                {i.badge}
              </div>
            ))}
            {filt.length === 0 && <div className='muted' style={{padding: 12, textAlign: 'center', fontSize: 12}}>No matches</div>}
          </div>
          {footer && <div className='combo-foot'>{footer}</div>}
        </div>
      )}
    </div>
  );
}

// ─── Dropdown menu ───────────────────────────────────────────────────────

export interface MenuItem {
  label: string;
  onClick: () => void;
  danger?: boolean;
  icon?: React.ReactNode;
  divider?: boolean;
  active?: boolean;  // selected row: accent + trailing check
  header?: boolean;  // non-clickable section label
}
export function Dropdown({trigger, items}:{trigger: React.ReactNode; items: MenuItem[]}) {
  const [open, setOpen] = useState(false);
  const ref = useClickOutside<HTMLDivElement>(open, () => setOpen(false));
  return (
    <div ref={ref} style={{position: 'relative'}}>
      <span onClick={() => setOpen(o => !o)}>{trigger}</span>
      {open && (
        <div className='dropdown'>
          {items.map((it, i) =>
            it.divider
              ? <div key={i} className='sep'/>
              : it.header
                ? <div key={i} className='dropdown-header'>{it.label}</div>
                : <div key={i} className={`item${it.danger ? ' danger' : ''}${it.active ? ' active' : ''}`}
                       onClick={() => { it.onClick(); setOpen(false); }}>
                    {it.icon}
                    <span className='dd-label'>{it.label}</span>
                    {it.active && <Icon.Check className='dd-check' width={13} height={13}/>}
                  </div>
          )}
        </div>
      )}
    </div>
  );
}

// ─── CodeBlock: click-to-copy ───────────────────────────────────────────

export function CodeBlock({children, multiline}: {children: string; multiline?: boolean}) {
  const [copied, setCopied] = useState(false);
  const click = () => {
    navigator.clipboard?.writeText(children).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1100);
    });
  };
  return (
    <span onClick={click}
          title='Click to copy'
          className='mono'
          style={{
            display: multiline ? 'block' : 'inline-block',
            background: 'var(--bg-inset)', color: 'var(--text)',
            border: '1px solid var(--border)',
            padding: multiline ? '8px 10px' : '1px 6px',
            borderRadius: 4, cursor: 'copy',
            fontSize: multiline ? 11.5 : 11,
            whiteSpace: multiline ? 'pre-wrap' : 'nowrap',
            wordBreak: 'break-all',
            position: 'relative',
          }}>
      {children}
      {copied && <span style={{marginLeft: 8, color: 'var(--ok)', fontSize: 10}}>copied!</span>}
    </span>
  );
}

// ─── CommandPreview: the command, without taking over the panel ─────────
//
// Every device action has to be able to show the command it runs
// (CLAUDE.md §4.1), but a multi-line block per action crowds out everything
// else on a panel that has several of them. Collapsed this is one line: what
// runs, and how many steps there are. Expanding shows all of it, and copying
// takes the whole thing either way — the rule is that the command is always
// reachable, not that it is always in the way.
// defaultOpen is for the panels CLAUDE.md §4.1 requires to keep the command
// live — a running capture, a stream, a confirm dialog for something
// irreversible. Those start expanded and can be collapsed; everything else
// starts collapsed and can be expanded. The control is the same either way, so
// copying works identically wherever a command appears.
export function CommandPreview({commands, label = 'Command', defaultOpen}: {commands: string[]; label?: string; defaultOpen?: boolean}) {
  const [open, setOpen] = useState(!!defaultOpen);
  const [copied, setCopied] = useState(false);
  const lines = (commands ?? []).filter(c => c.trim() !== '');
  if (lines.length === 0) return null;
  const all = lines.join('\n');

  const copy = (e: React.MouseEvent) => {
    e.stopPropagation();
    navigator.clipboard?.writeText(all).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1100);
    });
  };

  return (
    <div style={{marginTop: 8}}>
      <div onClick={() => setOpen(o => !o)}
           title={open ? 'Hide the command' : 'Show the command'}
           style={{
             display: 'flex', alignItems: 'center', gap: 6, cursor: 'pointer',
             fontSize: 11, color: 'var(--text-dim)', userSelect: 'none',
           }}>
        <span style={{
          display: 'inline-block', transition: 'transform .12s',
          transform: open ? 'rotate(90deg)' : 'none', opacity: .7,
        }}>›</span>
        <span>{label}{lines.length > 1 ? ` · ${lines.length} steps` : ''}</span>
        <span className='mono' style={{
          flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis',
          whiteSpace: 'nowrap', opacity: open ? 0 : .55,
        }}>{open ? '' : lines[0]}</span>
        <button className='btn sm' onClick={copy} title='Copy every line'
                style={{padding: '0 6px', fontSize: 10}}>
          {copied ? 'copied!' : 'copy'}
        </button>
      </div>
      {open && <div style={{marginTop: 6}}><CodeBlock multiline>{all}</CodeBlock></div>}
    </div>
  );
}

// ─── Click-outside hook ──────────────────────────────────────────────────

export function useClickOutside<T extends HTMLElement>(active: boolean, onClose: () => void) {
  const ref = useRef<T>(null);
  useEffect(() => {
    if (!active) return;
    const h = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };
    document.addEventListener('mousedown', h);
    return () => document.removeEventListener('mousedown', h);
  }, [active, onClose]);
  return ref;
}

// FeatureState describes why a screen can't render its normal content, so each
// screen shows a consistent, honest state (loading / empty / unavailable /
// requires-root / error) instead of a blank panel or a vanishing toast.
export type FeatureState =
  | {kind: 'loading'}
  | {kind: 'empty'; hint?: string}
  | {kind: 'unavailable'; reason: string}
  | {kind: 'requires-root'; what?: string}
  | {kind: 'error'; message: string; retry?: () => void};

export function FeatureNotice({state}: {state: FeatureState}) {
  if (state.kind === 'loading')
    return <div className='muted' style={{padding: 28, textAlign: 'center'}}>Loading…</div>;
  if (state.kind === 'empty')
    return <div className='muted' style={{padding: 28, textAlign: 'center'}}>{state.hint || 'Nothing here yet.'}</div>;
  const title = state.kind === 'requires-root' ? 'Root required'
    : state.kind === 'unavailable' ? 'Not available on this device'
      : 'Something went wrong';
  const body = state.kind === 'requires-root'
    ? `${state.what || 'This feature'} needs root (su) on the device.`
    : state.kind === 'unavailable' ? state.reason : state.message;
  return (
    <div className='card' style={{margin: 24, padding: 16, maxWidth: 560}}>
      <strong>{title}</strong>
      <div className='muted' style={{fontSize: 13, marginTop: 6, lineHeight: 1.5}}>{body}</div>
      {state.kind === 'error' && state.retry &&
        <button className='btn sm' style={{marginTop: 12}} onClick={state.retry}>Retry</button>}
    </div>
  );
}
