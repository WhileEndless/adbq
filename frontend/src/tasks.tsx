// Tasks panel: shows long-running operations (install, export, screen record…)
// with live status updates pushed from Go via the `task:update` Wails event.
import React, {useCallback, useEffect, useMemo, useState} from 'react';
import {adb} from '../wailsjs/go/models';
import * as API from '../wailsjs/go/main/App';
import {EventsOn, EventsOff} from '../wailsjs/runtime/runtime';
import {Icon} from './icons';
import {Badge} from './ui';

interface UseTasksApi {
  tasks: adb.TaskState[];
  remove: (id: string) => void;
  removeAll: (ids: string[]) => void;
}

export function useTasks(): UseTasksApi {
  const [tasks, setTasks] = useState<adb.TaskState[]>([]);
  useEffect(() => {
    // No tasks yet marshals as null, not [].
    API.ListTasks().then(t => setTasks(t || [])).catch(() => {});
    EventsOn('task:update', (t: adb.TaskState) => {
      setTasks(prev => {
        const idx = prev.findIndex(x => x.id === t.id);
        if (idx >= 0) {
          const next = prev.slice();
          next[idx] = t;
          return next;
        }
        return [...prev, t];
      });
    });
    return () => EventsOff('task:update');
  }, []);
  // Local removal is the source of truth — the Go RemoveTask call doesn't emit
  // a `task:update` event (there's nothing to update), so we have to optimistically
  // drop the row from our state here.
  const remove = useCallback((id: string) => {
    setTasks(prev => prev.filter(t => t.id !== id));
    API.RemoveTask(id).catch(() => {});
  }, []);
  const removeAll = useCallback((ids: string[]) => {
    const set = new Set(ids);
    setTasks(prev => prev.filter(t => !set.has(t.id)));
    ids.forEach(id => API.RemoveTask(id).catch(() => {}));
  }, []);
  return {tasks, remove, removeAll};
}

export function TasksTray() {
  const {tasks: all, remove, removeAll} = useTasks();
  const active = useMemo(() => all.filter(t => t.status === 'running'), [all]);
  const recent = useMemo(() => all.filter(t => t.status !== 'running').slice(-10).reverse(), [all]);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    // Auto-open when a new task starts.
    if (active.length > 0) setOpen(true);
  }, [active.length]);

  if (all.length === 0) return null;

  return (
    <div style={{position: 'fixed', bottom: 18, right: 18, zIndex: 80, width: 320}}>
      <button className='btn' style={{width: '100%', justifyContent: 'space-between', marginBottom: 6}}
              onClick={() => setOpen(o => !o)}>
        <span style={{display: 'inline-flex', alignItems: 'center', gap: 6}}>
          {active.length > 0 && <span className='pulse'/>}
          Tasks · {active.length} running{recent.length > 0 ? ` · ${recent.length} done` : ''}
        </span>
        <span className='subtle'>{open ? '▾' : '▴'}</span>
      </button>
      {open && (
        <div className='card' style={{maxHeight: 380, overflow: 'auto', boxShadow: 'var(--shadow-lg)'}}>
          {active.map(t => <TaskRow key={t.id} task={t} live onRemove={remove}/>)}
          {active.length > 0 && recent.length > 0 && <div className='divider'/>}
          {recent.map(t => <TaskRow key={t.id} task={t} onRemove={remove}/>)}
          {recent.length > 0 && (
            <div style={{padding: '6px 12px', borderTop: '1px solid var(--border)'}}>
              <button className='btn sm' style={{width: '100%'}} onClick={() => removeAll(recent.map(t => t.id))}>
                Clear completed ({recent.length})
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function TaskRow({task, live, onRemove}: {task: adb.TaskState; live?: boolean; onRemove?: (id: string) => void}) {
  const ic = task.status === 'running' ? <Icon.Refresh width={13} height={13}/> :
             task.status === 'ok'      ? <Icon.Activity width={13} height={13} style={{color: 'var(--ok)'}}/> :
             task.status === 'err'     ? <Icon.X width={13} height={13} style={{color: 'var(--err)'}}/> :
                                          <Icon.Pause width={13} height={13}/>;
  return (
    <div style={{padding: '10px 14px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'flex-start', gap: 8}}>
      <span style={{marginTop: 1}}>{ic}</span>
      <div style={{flex: 1, minWidth: 0}}>
        <div style={{fontWeight: 500, fontSize: 12, display: 'flex', alignItems: 'center', gap: 6}}>
          {task.title}
          {task.status === 'running' && <Badge kind='accent'>running</Badge>}
          {task.status === 'ok' && <Badge kind='ok'>done</Badge>}
          {task.status === 'err' && <Badge kind='err'>failed</Badge>}
          {task.status === 'cancelled' && <Badge>cancelled</Badge>}
        </div>
        <div className='mono subtle' style={{fontSize: 11, marginTop: 2, wordBreak: 'break-all'}}>
          {task.detail || task.kind}
        </div>
        {task.err && <div style={{color: 'var(--err)', fontSize: 11, marginTop: 4}}>{task.err}</div>}
        {task.output && task.status === 'ok' && (
          <div style={{marginTop: 6, display: 'flex', gap: 4}}>
            <button className='btn sm' onClick={() => API.OpenPath(task.output)}><Icon.Play/>Open</button>
            <button className='btn sm' onClick={() => API.RevealPath(task.output)}>Reveal</button>
          </div>
        )}
        <div style={{marginTop: 6, display: 'flex', gap: 4}}>
          {live && (
            <button className='btn sm danger' onClick={() => API.CancelTask(task.id)}>
              Cancel
            </button>
          )}
          {!live && onRemove && (
            <button className='btn sm ghost' onClick={() => onRemove(task.id)}>
              <Icon.X width={10} height={10}/>Dismiss
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
