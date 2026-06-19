// Re-export shapes from the generated wailsjs models for ergonomics.
export type {adb} from '../wailsjs/go/models';

export type Screen =
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
