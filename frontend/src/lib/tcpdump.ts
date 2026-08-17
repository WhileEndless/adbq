import React from 'react';
import * as API from '../../wailsjs/go/main/App';
import {CommandPreview, confirmDialog, showToast} from '../ui';

// installTcpdumpAuto walks the user through the manifest-pinned auto-install
// flow: plan → confirm with URL + hash on screen → fetch/verify → push. The
// confirm step is non-negotiable; we never reach the network without it.
//
// Returns true when tcpdump is usable on the device afterwards (whether we
// installed it or it was already there). Caller should re-probe / restart
// its stream on a true return.
export async function installTcpdumpAuto(serial: string): Promise<boolean> {
  let plan;
  try {
    plan = await API.PlanTcpdumpAutoInstall(serial);
  } catch (e) {
    // Most common cause on emulators: x86/x86_64 ABI not in the pinned
    // manifest. Steer the user to the file-picker path instead of just
    // dumping the error.
    const msg = String(e);
    const isAbi = /no pinned tcpdump build for ABI/i.test(msg);
    showToast({
      title: isAbi ? 'Auto-install not available for this ABI' : 'Auto-install unavailable',
      body: isAbi
        ? msg + ' — use "Install from file" with an NDK-built tcpdump matching the device.'
        : msg,
      kind: 'err',
    });
    return false;
  }
  if (!plan) return false;

  const shortHash = plan.sha256.slice(0, 16) + '…' + plan.sha256.slice(-8);
  // What it does to the device, beside where the bytes come from: the download
  // is verified here, but the push and chmod are the device's business.
  const cmds = await API.TcpdumpInstallCommands(serial).then(c => c ?? []).catch(() => [] as string[]);
  const body = React.createElement('div', {style: {display: 'grid', gap: 6, fontSize: 12}},
    React.createElement('div', null,
      `adbq will download a tcpdump binary matching this device's ABI (${plan.abi}), `,
      'verify its SHA256, and push it to ',
      React.createElement('span', {className: 'mono'}, '/data/local/tmp/tcpdump'),
      '.'),
    React.createElement('div', null, React.createElement('strong', null, 'Source: '), plan.source),
    React.createElement('div', {className: 'mono', style: {fontSize: 11, wordBreak: 'break-all'}}, plan.url),
    React.createElement('div', null, React.createElement('strong', null, 'SHA256: '),
      React.createElement('span', {className: 'mono'}, shortHash)),
    plan.cached
      ? React.createElement('div', {style: {color: 'var(--ok)'}}, 'Already verified in local cache — no download needed.')
      : null,
    React.createElement(CommandPreview, {commands: cmds, defaultOpen: true}),
  );
  const ok = await confirmDialog({
    title: 'Install tcpdump on device?',
    body,
    confirmLabel: plan.cached ? 'Install' : 'Download & install',
  });
  if (!ok) return false;
  try {
    const info = await API.InstallTcpdumpAuto(serial, true);
    if (info && info.available) {
      showToast({title: 'tcpdump installed', body: info.version || info.path, kind: 'ok', mono: true});

      return true;
    }
    showToast({title: 'Install finished but tcpdump is not usable', body: 'Check device logs', kind: 'err'});
    return false;
  } catch (e) {
    showToast({title: 'Install failed', body: String(e), kind: 'err'});
    return false;
  }
}
