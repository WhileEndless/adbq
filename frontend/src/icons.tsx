import React from 'react';

const I = (p: React.SVGProps<SVGSVGElement>) => ({
  width: 16, height: 16, viewBox: '0 0 24 24', fill: 'none',
  stroke: 'currentColor', strokeWidth: 1.8, strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const, ...p,
});

export const Icon = {
  Phone: (p: any) => <svg {...I(p)}><rect x='7' y='2' width='10' height='20' rx='2'/><path d='M11 18h2'/></svg>,
  Activity: (p: any) => <svg {...I(p)}><path d='M22 12h-4l-3 9L9 3l-3 9H2'/></svg>,
  Terminal: (p: any) => <svg {...I(p)}><path d='m4 8 4 4-4 4M12 16h8'/></svg>,
  Grid: (p: any) => <svg {...I(p)}><rect x='3' y='3' width='7' height='7'/><rect x='14' y='3' width='7' height='7'/><rect x='14' y='14' width='7' height='7'/><rect x='3' y='14' width='7' height='7'/></svg>,
  Folder: (p: any) => <svg {...I(p)}><path d='M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z'/></svg>,
  Arrows: (p: any) => <svg {...I(p)}><path d='M7 7h10M7 17h10M17 4l3 3-3 3M7 14l-3 3 3 3'/></svg>,
  Zap: (p: any) => <svg {...I(p)}><path d='M13 2 3 14h7l-1 8 10-12h-7l1-8z'/></svg>,
  Check: (p: any) => <svg {...I(p)}><path d='M20 6 9 17l-5-5'/></svg>,
  ChevronDown: (p: any) => <svg {...I(p)}><path d='m6 9 6 6 6-6'/></svg>,
  Wifi: (p: any) => <svg {...I(p)}><path d='M5 12.55a11 11 0 0 1 14 0M1.42 9a16 16 0 0 1 21.16 0M8.53 16.11a6 6 0 0 1 6.95 0'/><line x1='12' y1='20' x2='12.01' y2='20'/></svg>,
  Settings: (p: any) => <svg {...I(p)}><circle cx='12' cy='12' r='3'/><path d='M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3h0a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8v0a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z'/></svg>,
  Search: (p: any) => <svg {...I(p)}><circle cx='11' cy='11' r='7'/><path d='m20 20-3-3'/></svg>,
  Play: (p: any) => <svg {...I(p)}><polygon points='6 4 20 12 6 20 6 4'/></svg>,
  Pause: (p: any) => <svg {...I(p)}><rect x='6' y='4' width='4' height='16'/><rect x='14' y='4' width='4' height='16'/></svg>,
  Stop: (p: any) => <svg {...I(p)}><rect x='5' y='5' width='14' height='14' rx='1'/></svg>,
  Plus: (p: any) => <svg {...I(p)}><path d='M12 5v14M5 12h14'/></svg>,
  X: (p: any) => <svg {...I(p)}><path d='M18 6 6 18M6 6l12 12'/></svg>,
  Clipboard: (p: any) => <svg {...I(p)}><rect x='9' y='2' width='6' height='4' rx='1'/><path d='M5 6h2m10 0h2a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2'/></svg>,
  Trash: (p: any) => <svg {...I(p)}><path d='M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2'/></svg>,
  Download: (p: any) => <svg {...I(p)}><path d='M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3'/></svg>,
  Upload: (p: any) => <svg {...I(p)}><path d='M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M17 8l-5-5-5 5M12 3v12'/></svg>,
  Refresh: (p: any) => <svg {...I(p)}><path d='M23 4v6h-6M1 20v-6h6'/><path d='M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15'/></svg>,
  Camera: (p: any) => <svg {...I(p)}><path d='M23 19a2 2 0 0 1-2 2H3a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h4l2-3h6l2 3h4a2 2 0 0 1 2 2z'/><circle cx='12' cy='13' r='4'/></svg>,
  Shield: (p: any) => <svg {...I(p)}><path d='M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z'/></svg>,
  Sun: (p: any) => <svg {...I(p)}><circle cx='12' cy='12' r='5'/><path d='M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42'/></svg>,
  Moon: (p: any) => <svg {...I(p)}><path d='M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z'/></svg>,
  Monitor: (p: any) => <svg {...I(p)}><rect x='2' y='3' width='20' height='14' rx='2'/><path d='M8 21h8M12 17v4'/></svg>,
  Cpu: (p: any) => <svg {...I(p)}><rect x='4' y='4' width='16' height='16' rx='2'/><rect x='9' y='9' width='6' height='6'/><path d='M9 1v3M15 1v3M9 20v3M15 20v3M20 9h3M20 14h3M1 9h3M1 14h3'/></svg>,
  Globe: (p: any) => <svg {...I(p)}><circle cx='12' cy='12' r='10'/><path d='M2 12h20M12 2a15 15 0 0 1 0 20M12 2a15 15 0 0 0 0 20'/></svg>,
};
