// Small icon library — uses outline strokes, currentColor. Minimal Lucide-style.
const Icon = ({ d, fill, size = 14, stroke = 1.6, viewBox = "0 0 24 24", children }) => (
  <svg width={size} height={size} viewBox={viewBox} fill={fill || "none"} stroke="currentColor" strokeWidth={stroke} strokeLinecap="round" strokeLinejoin="round">
    {d ? <path d={d} /> : children}
  </svg>
);

const Icons = {
  Dashboard: (p) => <Icon {...p}><rect x="3" y="3" width="7" height="9" rx="1.5"/><rect x="14" y="3" width="7" height="5" rx="1.5"/><rect x="14" y="12" width="7" height="9" rx="1.5"/><rect x="3" y="16" width="7" height="5" rx="1.5"/></Icon>,
  Terminal: (p) => <Icon {...p}><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></Icon>,
  Logcat: (p) => <Icon {...p}><path d="M4 6h16"/><path d="M4 10h12"/><path d="M4 14h16"/><path d="M4 18h8"/></Icon>,
  Apps: (p) => <Icon {...p}><rect x="3" y="3" width="7" height="7" rx="1.3"/><rect x="14" y="3" width="7" height="7" rx="1.3"/><rect x="3" y="14" width="7" height="7" rx="1.3"/><rect x="14" y="14" width="7" height="7" rx="1.3"/></Icon>,
  Folder: (p) => <Icon {...p}><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></Icon>,
  Network: (p) => <Icon {...p}><circle cx="12" cy="12" r="9"/><path d="M3 12h18"/><path d="M12 3a13 13 0 0 1 0 18M12 3a13 13 0 0 0 0 18"/></Icon>,
  Forward: (p) => <Icon {...p}><path d="M3 12h13"/><path d="M12 7l5 5-5 5"/><path d="M20 5v14"/></Icon>,
  Reverse: (p) => <Icon {...p}><path d="M21 12H8"/><path d="M12 7l-5 5 5 5"/><path d="M4 5v14"/></Icon>,
  Bug: (p) => <Icon {...p}><path d="M8 3l1.5 1.8M16 3l-1.5 1.8"/><rect x="6" y="7" width="12" height="11" rx="6"/><path d="M3 11h3M3 17h3M18 11h3M18 17h3M12 7v13"/></Icon>,
  Camera: (p) => <Icon {...p}><rect x="3" y="6" width="18" height="14" rx="2"/><circle cx="12" cy="13" r="3.5"/><path d="M8 6l1.5-2h5L16 6"/></Icon>,
  Search: (p) => <Icon {...p}><circle cx="11" cy="11" r="7"/><path d="m20 20-3.6-3.6"/></Icon>,
  Plus: (p) => <Icon {...p}><path d="M12 5v14M5 12h14"/></Icon>,
  Close: (p) => <Icon {...p}><path d="M6 6l12 12M18 6 6 18"/></Icon>,
  Refresh: (p) => <Icon {...p}><path d="M3 12a9 9 0 0 1 15-6.7L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-15 6.7L3 16"/><path d="M3 21v-5h5"/></Icon>,
  Power: (p) => <Icon {...p}><path d="M12 3v9"/><path d="M18.4 6.6a9 9 0 1 1-12.8 0"/></Icon>,
  Battery: (p) => <Icon {...p}><rect x="3" y="8" width="16" height="9" rx="2"/><path d="M21 11v3"/><path d="M6 11v3M9 11v3M12 11v3"/></Icon>,
  Cpu: (p) => <Icon {...p}><rect x="5" y="5" width="14" height="14" rx="2"/><rect x="9" y="9" width="6" height="6"/><path d="M9 1v3M15 1v3M9 20v3M15 20v3M1 9h3M1 15h3M20 9h3M20 15h3"/></Icon>,
  Memory: (p) => <Icon {...p}><rect x="3" y="6" width="18" height="12" rx="2"/><path d="M8 6v12M16 6v12"/><circle cx="6" cy="12" r="0.5" fill="currentColor"/><circle cx="18" cy="12" r="0.5" fill="currentColor"/></Icon>,
  Download: (p) => <Icon {...p}><path d="M12 4v12"/><path d="M7 11l5 5 5-5"/><path d="M5 20h14"/></Icon>,
  Upload: (p) => <Icon {...p}><path d="M12 20V8"/><path d="M7 13l5-5 5 5"/><path d="M5 4h14"/></Icon>,
  Trash: (p) => <Icon {...p}><path d="M3 6h18"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/></Icon>,
  Settings: (p) => <Icon {...p}><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/></Icon>,
  Wifi: (p) => <Icon {...p}><path d="M5 12.55a11 11 0 0 1 14 0"/><path d="M2 8.82a16 16 0 0 1 20 0"/><path d="M8.5 16.43a6 6 0 0 1 7 0"/><circle cx="12" cy="20" r="0.5" fill="currentColor"/></Icon>,
  Globe: (p) => <Icon {...p}><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a13 13 0 0 1 0 18M12 3a13 13 0 0 0 0 18"/></Icon>,
  Lock: (p) => <Icon {...p}><rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/></Icon>,
  Shield: (p) => <Icon {...p}><path d="M12 3l8 3v6c0 5-3.4 8.4-8 9-4.6-.6-8-4-8-9V6z"/><path d="m9 12 2 2 4-4"/></Icon>,
  Phone: (p) => <Icon {...p}><rect x="6" y="2" width="12" height="20" rx="2.5"/><circle cx="12" cy="18.5" r="0.7" fill="currentColor"/></Icon>,
  Copy: (p) => <Icon {...p}><rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/></Icon>,
  Play: (p) => <Icon {...p}><polygon points="6 4 20 12 6 20" fill="currentColor" stroke="none"/></Icon>,
  Stop: (p) => <Icon {...p}><rect x="6" y="6" width="12" height="12" rx="1" fill="currentColor" stroke="none"/></Icon>,
  Filter: (p) => <Icon {...p}><path d="M3 5h18l-7 9v6l-4-2v-4z"/></Icon>,
  ChevronDown: (p) => <Icon {...p}><path d="m6 9 6 6 6-6"/></Icon>,
  ChevronRight: (p) => <Icon {...p}><path d="m9 6 6 6-6 6"/></Icon>,
  Pause: (p) => <Icon {...p}><rect x="6" y="5" width="4" height="14" rx="1" fill="currentColor" stroke="none"/><rect x="14" y="5" width="4" height="14" rx="1" fill="currentColor" stroke="none"/></Icon>,
  Bolt: (p) => <Icon {...p}><polygon points="13 2 4 14 11 14 10 22 20 10 13 10" fill="currentColor" stroke="none"/></Icon>,
  Eye: (p) => <Icon {...p}><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/></Icon>,
  Sun: (p) => <Icon {...p}><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41"/></Icon>,
  Moon: (p) => <Icon {...p}><path d="M21 12.8A8 8 0 1 1 11.2 3 6.4 6.4 0 0 0 21 12.8z"/></Icon>,
  Clipboard: (p) => <Icon {...p}><rect x="6" y="4" width="12" height="17" rx="2"/><rect x="9" y="2" width="6" height="4" rx="1"/></Icon>,
  Code: (p) => <Icon {...p}><polyline points="8 6 2 12 8 18"/><polyline points="16 6 22 12 16 18"/></Icon>,
  File: (p) => <Icon {...p}><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><polyline points="14 3 14 8 19 8"/></Icon>,
  Image: (p) => <Icon {...p}><rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="9" cy="9" r="1.5"/><path d="m21 15-5-5L5 21"/></Icon>,
};

Object.assign(window, { Icon, Icons });
