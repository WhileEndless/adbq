// Shared UI primitives: toast, modal, combobox, dropdown
const { useState: useStateUI, useEffect: useEffectUI, useRef: useRefUI, useCallback: useCallbackUI } = React;

// ─── Toast bus (global) ─────────────────────────────────────────────────────
const __toastBus = { listeners: new Set() };
function showToast(opts) {
  const t = { id: Math.random().toString(36).slice(2), kind: "info", ttl: 2600, ...opts };
  __toastBus.listeners.forEach(fn => fn({ type: "add", t }));
}
function ToastHost() {
  const [toasts, setToasts] = useStateUI([]);
  useEffectUI(() => {
    const fn = (e) => {
      if (e.type === "add") {
        setToasts(ts => [...ts, e.t]);
        setTimeout(() => setToasts(ts => ts.filter(x => x.id !== e.t.id)), e.t.ttl);
      }
    };
    __toastBus.listeners.add(fn);
    return () => __toastBus.listeners.delete(fn);
  }, []);
  return (
    <div className="toast-host">
      {toasts.map(t => {
        const Icon = t.kind === "ok" ? Icons.Shield : t.kind === "err" ? Icons.Close : Icons.Bolt;
        return (
          <div key={t.id} className={`toast ${t.kind}`}>
            <span className="ic"><Icon size={14} /></span>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontWeight: 500 }}>{t.title}</div>
              {t.body && <div className="muted" style={{ fontSize: 11, marginTop: 1, fontFamily: t.mono ? "var(--font-mono)" : undefined }}>{t.body}</div>}
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ─── Modal ──────────────────────────────────────────────────────────────────
function Modal({ open, onClose, title, children, footer, width = 460 }) {
  useEffectUI(() => {
    if (!open) return;
    const h = (e) => { if (e.key === "Escape") onClose?.(); };
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, [open, onClose]);
  if (!open) return null;
  return (
    <div className="modal-backdrop" onMouseDown={onClose}>
      <div className="modal" style={{ width }} onMouseDown={e => e.stopPropagation()}>
        <div className="modal-header">
          <div className="title">{title}</div>
          <button className="iconbtn" onClick={onClose}><Icons.Close size={13} /></button>
        </div>
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-footer">{footer}</div>}
      </div>
    </div>
  );
}

// ─── Dropdown (positioned relative to a trigger button) ─────────────────────
function DropdownButton({ label, icon, items, primary, ghost, className }) {
  const [open, setOpen] = useStateUI(false);
  const ref = useRefUI();
  useEffectUI(() => {
    const h = (e) => { if (ref.current && !ref.current.contains(e.target)) setOpen(false); };
    window.addEventListener("mousedown", h);
    return () => window.removeEventListener("mousedown", h);
  }, []);
  const Cmp = icon ? Icons[icon] : null;
  const cls = primary ? "btn primary" : ghost ? "btn ghost" : "btn";
  return (
    <div ref={ref} style={{ position: "relative" }} className={className}>
      <button className={cls} onClick={() => setOpen(o => !o)}>
        {Cmp && <Cmp className="icon" />} {label} <Icons.ChevronDown size={11} />
      </button>
      {open && (
        <div className="dropdown">
          {items.map((it, i) => it.sep ? <div key={i} className="sep" /> : (
            <div key={i} className={`item ${it.danger ? "danger" : ""}`}
                 onClick={() => { setOpen(false); it.onClick?.(); }}>
              {it.icon && (() => { const I = Icons[it.icon]; return <I size={13} />; })()}
              <span style={{ flex: 1 }}>{it.label}</span>
              {it.right && <span className="muted mono" style={{ fontSize: 10.5 }}>{it.right}</span>}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ─── Combobox: searchable picker ────────────────────────────────────────────
function Combobox({ value, onChange, options, placeholder, triggerIcon, triggerStyle, popWidth = 320 }) {
  const [open, setOpen] = useStateUI(false);
  const [q, setQ] = useStateUI("");
  const inputRef = useRefUI();
  const ref = useRefUI();
  useEffectUI(() => {
    if (open) setTimeout(() => inputRef.current?.focus(), 10);
  }, [open]);
  useEffectUI(() => {
    const h = (e) => { if (ref.current && !ref.current.contains(e.target)) setOpen(false); };
    window.addEventListener("mousedown", h);
    return () => window.removeEventListener("mousedown", h);
  }, []);

  const filtered = useMemo(() => {
    if (!q) return options;
    const lq = q.toLowerCase();
    return options.filter(o => (o.label + " " + (o.sub || "")).toLowerCase().includes(lq));
  }, [q, options]);

  const TrigIcon = triggerIcon ? Icons[triggerIcon] : Icons.Filter;
  const cur = options.find(o => o.value === value);
  return (
    <div ref={ref} className="combo">
      <div className="combo-trigger" onClick={() => setOpen(o => !o)} style={triggerStyle}>
        <TrigIcon size={12} className="muted" />
        <span className="val">{cur ? cur.label : placeholder || "Choose…"}</span>
        <Icons.ChevronDown size={11} className="muted" />
      </div>
      {open && (
        <div className="combo-pop" style={{ width: popWidth }}>
          <div className="combo-search">
            <Icons.Search size={12} className="muted" />
            <input ref={inputRef} value={q} onChange={e => setQ(e.target.value)} placeholder="Filter…" />
            {q && <button className="iconbtn" onClick={() => setQ("")}><Icons.Close size={11} /></button>}
          </div>
          <div className="combo-list">
            {filtered.length === 0 && (
              <div className="combo-opt muted" style={{ padding: 12, textAlign: "center" }}>No matches</div>
            )}
            {filtered.map(o => (
              <div key={o.value}
                   className={`combo-opt ${o.value === value ? "selected" : ""}`}
                   onClick={() => { onChange(o.value); setOpen(false); setQ(""); }}>
                {o.icon && <div className="icon" style={{ background: o.icon.bg, color: o.icon.color || "white" }}>{o.icon.l}</div>}
                <div className="text">
                  <div className="name">{o.label}</div>
                  {o.sub && <div className="sub">{o.sub}</div>}
                </div>
                {o.right && <span className="mono muted" style={{ fontSize: 10.5 }}>{o.right}</span>}
              </div>
            ))}
          </div>
          <div className="combo-foot">
            <span>{filtered.length} / {options.length}</span>
            <span><span className="kbd">↑↓</span> <span className="kbd">⏎</span> select</span>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Confirm (uses Modal) ───────────────────────────────────────────────────
function Confirm({ open, onClose, title, body, danger, confirmLabel = "Confirm", onConfirm }) {
  return (
    <Modal open={open} onClose={onClose} title={title} width={400}
      footer={<>
        <button className="btn ghost" onClick={onClose}>Cancel</button>
        <button className={danger ? "btn danger" : "btn primary"}
                onClick={() => { onConfirm(); onClose(); }}>
          {confirmLabel}
        </button>
      </>}>
      <div style={{ fontSize: 13, color: "var(--text-muted)" }}>{body}</div>
    </Modal>
  );
}

Object.assign(window, { showToast, ToastHost, Modal, Confirm, DropdownButton, Combobox });
