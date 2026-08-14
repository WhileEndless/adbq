import React from 'react';
import * as API from '../../wailsjs/go/main/App';
import {CodeBlock, confirmDialog, showToast} from '../ui';

// pickApkAndInstall runs the whole install flow: pick a file, read the plan
// off the device, show what will run, then install. Installing is not tied to
// any selected app, so this lives next to the screens rather than inside one.
//
// An .apks/.xapk holds several APKs and only some of them apply to this
// device, so the confirm step shows the resolved command and everything that
// was dropped — the user commits to the real thing, not to a label.
export async function pickApkAndInstall(serial: string): Promise<boolean> {
  const file = await API.PickApkFile();
  if (!file) return false;
  const name = file.replace(/^.*[\\/]/, '');
  let plan: Awaited<ReturnType<typeof API.PlanApkInstall>>;
  try {
    plan = await API.PlanApkInstall(serial, file);
  } catch (e) {
    showToast({title: 'Cannot install ' + name, body: String(e), kind: 'err'});
    return false;
  }
  const ok = await confirmDialog({
    title: `Install ${name}?`,
    body: (
      <div style={{fontSize: 12}}>
        <div style={{marginBottom: 6}}>
          {plan.split
            ? `${plan.install?.length ?? 0} APKs will be committed in one pm session.`
            : 'A single APK will be installed.'}
        </div>
        <CodeBlock multiline>{(plan.commands ?? []).join('\n')}</CodeBlock>
        {(plan.skipped?.length ?? 0) > 0 && (
          <div className='muted' style={{marginTop: 8}}>
            Not installed on this device:
            <ul style={{margin: '4px 0 0 16px', padding: 0}}>
              {(plan.skipped ?? []).map(s => <li key={s}>{s}</li>)}
            </ul>
          </div>
        )}
      </div>
    ),
    confirmLabel: 'Install',
  });
  if (!ok) return false;
  try {
    await API.InstallApkBundleFromPath(serial, file);
    showToast({title: 'Install started', body: 'Watch the Tasks panel for progress', kind: 'info'});
    return true;
  } catch (e) {
    showToast({title: 'Install failed', body: String(e), kind: 'err'});
    return false;
  }
}
