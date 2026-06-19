package adb

import (
	"context"
	"sync"
	"sync/atomic"
)

// TaskState describes a long-running operation that the UI can observe.
type TaskState struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // "install", "uninstall", "export-apk", "export-data", "screen-record", "tcpdump"
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Status   string `json:"status"`   // "running", "ok", "err", "cancelled"
	Progress int    `json:"progress"` // 0..100; -1 = indeterminate
	Output   string `json:"output"`
	Err      string `json:"err"`
}

// TaskManager tracks running operations and lets the UI poll or subscribe.
type TaskManager struct {
	mu     sync.Mutex
	tasks  map[string]*TaskState
	cancel map[string]context.CancelFunc
	seq    atomic.Int64
	notify func(*TaskState)
}

func NewTaskManager() *TaskManager {
	return &TaskManager{
		tasks:  map[string]*TaskState{},
		cancel: map[string]context.CancelFunc{},
	}
}

func (m *TaskManager) OnUpdate(f func(*TaskState)) { m.notify = f }

func (m *TaskManager) Create(kind, title, detail string) (string, context.Context) {
	id := "t-" + itoa(m.seq.Add(1))
	ctx, cancel := context.WithCancel(context.Background())
	t := &TaskState{ID: id, Kind: kind, Title: title, Detail: detail, Status: "running", Progress: -1}
	m.mu.Lock()
	m.tasks[id] = t
	m.cancel[id] = cancel
	m.mu.Unlock()
	m.emit(t)
	return id, ctx
}

func (m *TaskManager) Update(id string, mutate func(*TaskState)) {
	// IIFE so defer releases the lock even if mutate panics.
	var cp *TaskState
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		t := m.tasks[id]
		if t == nil {
			return
		}
		mutate(t)
		cpy := *t
		cp = &cpy
	}()
	if cp != nil {
		m.emit(cp)
	}
}

func (m *TaskManager) Finish(id, status, output, errMsg string) {
	m.Update(id, func(t *TaskState) {
		t.Status = status
		t.Output = output
		t.Err = errMsg
		t.Progress = 100
	})
}

func (m *TaskManager) Cancel(id string) {
	m.mu.Lock()
	cf := m.cancel[id]
	m.mu.Unlock()
	if cf != nil {
		cf()
	}
	m.Update(id, func(t *TaskState) { t.Status = "cancelled" })
}

func (m *TaskManager) List() []TaskState {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]TaskState, 0, len(m.tasks))
	for _, t := range m.tasks {
		out = append(out, *t)
	}
	return out
}

func (m *TaskManager) Remove(id string) {
	m.mu.Lock()
	delete(m.tasks, id)
	delete(m.cancel, id)
	m.mu.Unlock()
}

func (m *TaskManager) emit(t *TaskState) {
	// Read m.notify under lock to avoid a data race with OnUpdate.
	m.mu.Lock()
	fn := m.notify
	m.mu.Unlock()
	if fn != nil {
		fn(t)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
