package runs

import (
	"context"
	"sync"
)

type Memory struct {
	mu    sync.Mutex
	rows  map[string]Run
	order []string
}

func NewMemory() *Memory { return &Memory{rows: map[string]Run{}} }
func clone(r Run) Run    { r.Events = append([]Event(nil), r.Events...); return r }
func (m *Memory) Create(_ context.Context, r Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[r.ID] = clone(r)
	m.order = append(m.order, r.ID)
	return nil
}
func (m *Memory) Get(_ context.Context, id string) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return Run{}, ErrNotFound
	}
	return clone(r), nil
}
func (m *Memory) Save(_ context.Context, r Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[r.ID]; !ok {
		return ErrNotFound
	}
	m.rows[r.ID] = clone(r)
	return nil
}
func (m *Memory) Claim(_ context.Context) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range m.order {
		r := m.rows[id]
		if r.Status == Queued {
			if err := r.Advance(Investigating, "Worker claimed run."); err != nil {
				return Run{}, err
			}
			m.rows[id] = clone(r)
			return clone(r), nil
		}
	}
	return Run{}, ErrEmpty
}
func (m *Memory) Recover(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, r := range m.rows {
		if r.Status != Queued && r.Status != Failed && r.Status != AwaitingReview {
			r.Error = "Worker stopped before completion; submit a new run."
			_ = r.Advance(Failed, r.Error)
			m.rows[id] = clone(r)
		}
	}
	return nil
}
