import React, {useEffect, useMemo, useState} from 'react';
import {adb} from '../../wailsjs/go/models';
import * as API from '../../wailsjs/go/main/App';
import {Icon} from '../icons';
import {Badge, CommandChip, CommandPreview, Modal, Switch, confirmDialog, promptDialog, showToast} from '../ui';
import {useDeviceData} from '../cache';

const ROOTS = ['/', '/sdcard', '/storage/emulated/0', '/data/local/tmp', '/data', '/system'];

export function FilesScreen({device}: {device: adb.Device}) {
  const [path, setPath] = useState('/');
  // Auto-enable root usage on rooted devices so /data and /system_ext browsing works out of the box.
  const [asRoot, setAsRoot] = useState<boolean>(!!device?.root);
  const [sel, setSel] = useState<adb.FileEntry | null>(null);
  const [treeExpanded, setTreeExpanded] = useState<Record<string, boolean>>({'/': true});
  const [pushOpen, setPushOpen] = useState(false);
  const [pushMode, setPushMode] = useState('');
  const [pushOwner, setPushOwner] = useState('');

  // Cached per (device, root, path) so revisiting a directory is instant; a
  // background revalidate then refreshes it. Navigation just changes `path`.
  const dirKey = device?.id ? `dir:${device.id}:${asRoot ? 'r' : 'u'}:${path}` : null;
  const {data, loading, refreshing, error, refresh} = useDeviceData(
    dirKey, () => API.ListDir(device.id, path, asRoot), {staleMs: 20000},
  );
  const entries = data || [];

  // The commands behind this screen's actions, from the backend: the root
  // wrapper depends on what the device accepts, so the frontend cannot spell
  // these out itself (CLAUDE.md §4.1).
  const [cmds, setCmds] = useState<adb.FileCommands | null>(null);
  useEffect(() => {
    if (!device?.id) { setCmds(null); return; }
    let live = true;
    API.FileCommands(device.id, {
      dir: path, name: sel?.name || '', isDir: sel?.type === 'dir', asRoot,
      mode: pushMode, owner: pushOwner,
    } as adb.FileCommandRequest)
      .then(c => { if (live) setCmds(c); })
      .catch(() => { if (live) setCmds(null); });
    return () => { live = false; };
  }, [device?.id, path, sel?.name, sel?.type, asRoot, pushMode, pushOwner]);
  const load = (p: string = path) => { setSel(null); if (p === path) refresh(); else setPath(p); };

  function navigate(e: adb.FileEntry) {
    if (e.type === 'up') {
      const parent = path.replace(/\/[^\/]+\/?$/, '') || '/';
      load(parent);
    } else if (e.type === 'dir') {
      load(joinPath(path, e.name));
    } else {
      setSel(e);
    }
  }

  function crumbs() {
    const parts = path.split('/').filter(Boolean);
    let acc = '';
    const out: React.ReactNode[] = [<span key='root' className='crumb' onClick={() => load('/')}>/</span>];
    parts.forEach((p, i) => {
      acc += '/' + p;
      const at = acc;
      if (i > 0) out.push(<span key={'sep' + i} className='sep'>/</span>);
      out.push(<span key={p + i} className='crumb' onClick={() => load(at)}>{p}</span>);
    });
    return out;
  }

  async function doMkdir() {
    const name = await promptDialog({
      title: 'Create folder', label: 'Folder name', placeholder: 'new-folder',
      hint: <CommandPreview commands={cmds?.mkdir ?? []} defaultOpen/>,
    });
    if (!name) return;
    API.Mkdir(device.id, joinPath(path, name), asRoot).then(() => load()).catch(e => showToast({title: 'Mkdir failed', body: String(e), kind: 'err'}));
  }

  async function doDelete(e: adb.FileEntry) {
    // The row's own command, not the selected entry's: the trash icon can be
    // clicked on a row the panel isn't showing.
    const rm = await API.FileCommands(device.id, {
      dir: path, name: e.name, isDir: e.type === 'dir', asRoot, mode: '', owner: '',
    } as adb.FileCommandRequest).then(c => c.delete ?? []).catch(() => []);
    const ok = await confirmDialog({
      title: `Delete "${e.name}"?`,
      body: <>
        <span className='mono'>{joinPath(path, e.name)}</span>{e.type === 'dir' ? '  (recursive)' : ''}
        <CommandPreview commands={rm} defaultOpen/>
      </>,
      confirmLabel: 'Delete',
      danger: true,
    });
    if (!ok) return;
    API.DeleteFile(device.id, joinPath(path, e.name), e.type === 'dir', asRoot)
      .then(() => { showToast({title: 'Deleted', body: e.name, kind: 'ok'}); load(); })
      .catch(err => showToast({title: 'Delete failed', body: String(err), kind: 'err'}));
  }

  function doPull(e: adb.FileEntry) {
    API.PullFileWithPicker(device.id, joinPath(path, e.name))
      .then(o => o && showToast({title: 'Pulled', body: o, kind: 'ok', mono: true}))
      .catch(err => showToast({title: 'Pull failed', body: String(err), kind: 'err'}));
  }

  const stats = useMemo(() => {
    const files = entries.filter(e => e.type === 'file');
    const dirs = entries.filter(e => e.type === 'dir');
    const totalBytes = files.reduce((a, e) => a + e.size, 0);
    return {files: files.length, dirs: dirs.length, totalBytes};
  }, [entries]);

  return (
    <div className='screen'>
      <div className='screen-header'>
        <h1>Files <span className='subtitle mono'>{stats.dirs} dir{stats.dirs === 1 ? '' : 's'} · {stats.files} file{stats.files === 1 ? '' : 's'} · {fmtSize(stats.totalBytes)}</span></h1>
        <div className='spacer' style={{flex: 1}}/>
        <label style={{display: 'flex', alignItems: 'center', gap: 8, fontSize: 12}} className='muted'>
          <Switch on={asRoot} onChange={setAsRoot}/> Root <span className='subtle'>(su -c)</span>
        </label>
        <button className='btn' onClick={() => setPushOpen(true)}>
          <Icon.Upload/>Push
        </button>
        <button className='btn' onClick={doMkdir}><Icon.Plus/>New folder</button>
        <CommandChip label={sel ? sel.name : path} groups={[
          {label: 'List this directory', commands: cmds?.list},
          {label: 'Push into it', commands: cmds?.push},
          {label: 'New folder', commands: cmds?.mkdir},
          {label: 'Pull the selected file', commands: cmds?.pull},
          {label: 'Delete the selection', commands: cmds?.delete},
        ]}/>
        <button className='btn' onClick={() => load()}><Icon.Refresh className={refreshing ? 'spin' : ''}/></button>
      </div>

      <div className='path-bar'>{crumbs()}</div>

      <div className='files-layout' style={{flex: 1, minHeight: 0}}>
        <div className='files-tree'>
          {ROOTS.map(r => (
            <TreeNode key={r} path={r} active={path === r || path.startsWith(r + '/')} current={path}
                      expanded={treeExpanded} setExpanded={setTreeExpanded} onSelect={load}
                      device={device} asRoot={asRoot}/>
          ))}
        </div>
        <div className='files-main'>
          <div style={{flex: 1, minHeight: 0, overflow: 'auto'}}>
            {loading
              ? <div className='muted' style={{padding: 16}}>Loading…</div>
              : (error && entries.length === 0)
                ? <div className='muted' style={{padding: 16, color: 'var(--err)'}}>{String(error)}</div>
                : null}
            <table className='table'>
              <thead><tr><th>Name</th><th>Size</th><th>Perms</th><th>Owner</th><th>Modified</th><th className='actions'></th></tr></thead>
              <tbody>
              {entries.map((e, i) => (
                <tr key={i} onClick={() => setSel(e)} onDoubleClick={() => navigate(e)} style={{cursor: 'pointer'}}>
                  <td>
                    <span style={{display: 'inline-flex', alignItems: 'center', gap: 8}}>
                      <FileIcon entry={e}/>
                      <span style={{fontWeight: e.type === 'dir' ? 500 : 400}}>{e.name}</span>
                      {e.link && <span className='subtle mono'> → {e.link}</span>}
                    </span>
                  </td>
                  <td className='mono'>{e.type === 'file' ? fmtSize(e.size) : ''}</td>
                  <td className='mono subtle'>{e.perms}</td>
                  <td className='mono subtle'>{e.owner}{e.group ? `:${e.group}` : ''}</td>
                  <td className='mono subtle'>{e.mtime}</td>
                  <td className='actions'>
                    {e.type === 'file' && <button className='iconbtn' title='Pull' onClick={(ev) => { ev.stopPropagation(); doPull(e); }}><Icon.Download width={13} height={13}/></button>}
                    {e.type !== 'up' && <button className='iconbtn' title='Delete' onClick={(ev) => { ev.stopPropagation(); doDelete(e); }}><Icon.Trash width={13} height={13}/></button>}
                  </td>
                </tr>
              ))}
              </tbody>
            </table>
          </div>
        </div>
        <div className='file-detail'>
          {sel ? (
            <>
              <div style={{display: 'flex', alignItems: 'center', gap: 10, marginBottom: 10}}>
                <FileIcon entry={sel} large/>
                <div style={{minWidth: 0}}>
                  <div style={{fontWeight: 600, fontSize: 14, wordBreak: 'break-all'}}>{sel.name}</div>
                  <div className='subtle mono' style={{fontSize: 11, wordBreak: 'break-all'}}>{joinPath(path, sel.name)}</div>
                </div>
              </div>
              <div style={{margin: '14px 0', display: 'flex', gap: 6, flexWrap: 'wrap'}}>
                <Badge>{sel.type}</Badge>
                {sel.type === 'file' && <Badge>{fmtSize(sel.size)}</Badge>}
                <Badge>{sel.perms}</Badge>
                <Badge>{sel.owner}:{sel.group}</Badge>
              </div>
              {sel.mtime && <div className='spread' style={{fontSize: 12, padding: '4px 0'}}><span className='muted'>Modified</span><span className='mono'>{sel.mtime}</span></div>}
              <div style={{display: 'grid', gap: 6, marginTop: 14}}>
                {sel.type === 'dir' && <button className='btn' onClick={() => navigate(sel)}><Icon.Folder/>Open</button>}
                {sel.type === 'file' && <button className='btn' onClick={() => doPull(sel)}><Icon.Download/>Pull</button>}
                <button className='btn danger' onClick={() => doDelete(sel)}><Icon.Trash/>Delete</button>
              </div>
              {sel.name.includes('frida-server') && (
                <div className='card' style={{marginTop: 14}}>
                  <div className='card-body'>
                    <div className='muted' style={{fontSize: 11, marginBottom: 6}}>Looks like a frida-server binary</div>
                    <button className='btn primary' style={{width: '100%'}} onClick={() =>
                      API.StartFrida(device.id, joinPath(path, sel.name), '0.0.0.0', 27042)
                        .then(o => showToast({title: 'frida-server started', body: o || '27042', kind: 'ok', mono: true}))
                        .catch(e => showToast({title: 'frida start failed', body: String(e), kind: 'err'}))}>
                      <Icon.Zap/>Run as frida-server
                    </button>
                  </div>
                </div>
              )}
            </>
          ) : <div className='muted' style={{padding: 20}}>Select a file or folder</div>}
        </div>
      </div>

      <Modal open={pushOpen} onClose={() => setPushOpen(false)} title={`Push file to ${path}`}
             footer={<>
               <button className='btn' onClick={() => setPushOpen(false)}>Cancel</button>
               <button className='btn primary' onClick={() => {
                 setPushOpen(false);
                 API.PushFileWithOptions(device.id, path, pushMode, pushOwner, asRoot)
                   .then(() => setTimeout(load, 500))
                   .catch(e => showToast({title: 'Push failed', body: String(e), kind: 'err'}));
               }}>Pick &amp; push</button>
             </>}>
        <div style={{display: 'grid', gap: 10}}>
          <div className='field'>
            <label>Mode (chmod) — optional</label>
            <input className='input mono' placeholder='755 / 644 / u+x' value={pushMode} onChange={e => setPushMode(e.target.value)}/>
          </div>
          <div className='field'>
            <label>Owner (chown) — optional</label>
            <input className='input mono' placeholder='shell  ·  root:root  ·  10042' value={pushOwner} onChange={e => setPushOwner(e.target.value)}/>
          </div>
          <CommandPreview commands={cmds?.push ?? []} defaultOpen/>
          <div className='muted' style={{fontSize: 11}}>
            chmod/chown run after the push completes. {asRoot ? 'Will use su -c.' : <span>Will use shell user; enable <strong>Root</strong> toggle for restricted paths.</span>}
          </div>
        </div>
      </Modal>
    </div>
  );
}

function joinPath(a: string, b: string) {
  if (a === '/') return '/' + b;
  return a.replace(/\/$/, '') + '/' + b;
}

function FileIcon({entry, large}: {entry: adb.FileEntry; large?: boolean}) {
  const size = large ? 26 : 15;
  if (entry.type === 'up')   return <span style={{color: 'var(--text-subtle)', fontSize: size, lineHeight: 1}}>↰</span>;
  if (entry.type === 'dir')  return <Icon.Folder width={size} height={size} style={{color: 'var(--accent)'}}/>;
  if (entry.type === 'link') return <Icon.Arrows width={size} height={size} style={{color: 'var(--info)'}}/>;
  const n = entry.name.toLowerCase();
  const isExec = entry.perms && entry.perms.includes('x');
  if (n.endsWith('.png') || n.endsWith('.jpg') || n.endsWith('.jpeg') || n.endsWith('.gif') || n.endsWith('.webp')) return <Icon.Camera width={size} height={size} style={{color: '#6fb3ff'}}/>;
  if (n.endsWith('.mp4') || n.endsWith('.mov') || n.endsWith('.mkv') || n.endsWith('.webm')) return <Icon.Monitor width={size} height={size} style={{color: '#6fb3ff'}}/>;
  if (n.endsWith('.apk') || n.endsWith('.apks') || n.endsWith('.aab')) return <Icon.Grid width={size} height={size} style={{color: 'var(--ok)'}}/>;
  if (n.endsWith('.zip') || n.endsWith('.tar') || n.endsWith('.gz') || n.endsWith('.xz') || n.endsWith('.7z')) return <Icon.Download width={size} height={size} style={{color: 'var(--warn)'}}/>;
  if (n.endsWith('.txt') || n.endsWith('.log') || n.endsWith('.md') || n.endsWith('.json') || n.endsWith('.xml') || n.endsWith('.conf')) return <Icon.Terminal width={size} height={size} style={{color: 'var(--text-muted)'}}/>;
  if (n.endsWith('.pem') || n.endsWith('.crt') || n.endsWith('.cer') || n.endsWith('.der') || n.endsWith('.0')) return <Icon.Shield width={size} height={size} style={{color: 'var(--warn)'}}/>;
  if (n.endsWith('.sh') || n.endsWith('.so') || isExec) return <Icon.Terminal width={size} height={size} style={{color: 'var(--ok)'}}/>;
  if (n.includes('frida')) return <Icon.Zap width={size} height={size} style={{color: 'var(--accent)'}}/>;
  return <span style={{display: 'inline-block', width: size, height: size, opacity: 0.5}}>📄</span>;
}

function fmtSize(n: number) {
  if (!n) return '';
  if (n < 1024) return n + ' B';
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
  if (n < 1024 * 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + ' MB';
  return (n / 1024 / 1024 / 1024).toFixed(2) + ' GB';
}

interface TreeProps {
  path: string;
  active: boolean;
  current: string;
  expanded: Record<string, boolean>;
  setExpanded: React.Dispatch<React.SetStateAction<Record<string, boolean>>>;
  onSelect: (p: string) => void;
  device: adb.Device;
  asRoot: boolean;
  depth?: number;
}
function TreeNode({path, active, current, expanded, setExpanded, onSelect, device, asRoot, depth = 0}: TreeProps) {
  const [children, setChildren] = useState<string[] | null>(null);
  const isOpen = expanded[path];
  useEffect(() => {
    if (!isOpen || children !== null) return;
    API.ListDir(device.id, path, asRoot).then(es =>
      setChildren((es || []).filter(e => e.type === 'dir').map(e => e.name)));
  }, [isOpen, device?.id, path, asRoot]);
  const label = path === '/' ? '/' : path.split('/').filter(Boolean).slice(-1)[0];
  return (
    <>
      <div className={`tree-row${active ? ' active' : ''}`} style={{paddingLeft: 8 + depth * 12}} onClick={() => { onSelect(path); setExpanded(e => ({...e, [path]: !e[path]})); }}>
        <span style={{display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 16, height: 16, color: 'var(--text)', fontSize: 13, lineHeight: 1, flexShrink: 0}}>{isOpen ? '▾' : '▸'}</span>
        <Icon.Folder width={14} height={14}/>
        <span style={{flex: 1, overflow: 'hidden', textOverflow: 'ellipsis'}}>{label}</span>
      </div>
      {isOpen && children?.map(c => (
        <TreeNode key={path + '/' + c} path={joinPath(path, c)}
                  active={current === joinPath(path, c) || current.startsWith(joinPath(path, c) + '/')}
                  current={current} expanded={expanded} setExpanded={setExpanded}
                  onSelect={onSelect} device={device} asRoot={asRoot} depth={depth + 1}/>
      ))}
    </>
  );
}
