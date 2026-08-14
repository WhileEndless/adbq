// Re-export shapes from the generated wailsjs models for ergonomics.
export type {adb} from '../wailsjs/go/models';

export type Screen =
  // Host-side screens work without a device attached; see HOST_SCREENS in App.tsx.
  | 'emulators'
  | 'overview'
  | 'logcat'
  | 'shell'
  | 'apps'
  | 'files'
  | 'forwards'
  | 'frida'
  | 'network'
  | 'capture'
  | 'iptables'
  | 'processes';

export interface LogEntry {
  time: string;
  pid: number;
  tid: number;
  lvl: string;
  tag: string;
  msg: string;
  /** Owning process name, resolved host-side from the device's pid table. */
  proc?: string;
  /** True when the owning process is an installed app rather than the OS. */
  app?: boolean;

  // ── Filled in by the frontend, never sent by the backend ──────────────
  /** Milliseconds used for repeat detection: device clock when parseable. */
  t?: number;
  /** True when an identical line was already shown moments earlier. */
  dup?: boolean;
}

export interface ToastAction {
  label: string;
  onClick: () => void;
}
export interface ToastMsg {
  id: number;
  title: string;
  body?: string;
  kind: 'ok' | 'err' | 'info';
  mono?: boolean;
  actions?: ToastAction[];
  ttl?: number;
}
