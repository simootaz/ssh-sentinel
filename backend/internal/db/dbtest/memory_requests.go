package dbtest

import (
	"context"
	"sort"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

func utcp(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// requestView returns a copy of r with the joined columns filled in.
func (m *Memory) requestView(r *model.Request) *model.Request {
	c := *r
	c.ServerName = m.serverName(r.ServerID)
	c.DecidedByDeviceLabel = m.deviceLabel(r.DecidedByDevice)
	return &c
}

func (m *Memory) CreateRequest(ctx context.Context, r *model.Request) (*model.Request, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.servers[r.ServerID]; !ok {
		return nil, db.ErrNotFound
	}
	c := *r
	c.ID = NewID()
	c.CreatedAt = r.CreatedAt.UTC()
	c.ExpiresAt = r.ExpiresAt.UTC()
	c.DecidedAt = utcp(r.DecidedAt)
	if c.Status == "" {
		c.Status = model.StatusPending
	}
	m.requests[c.ID] = &c
	return m.requestView(&c), nil
}

func (m *Memory) RequestByID(ctx context.Context, id string) (*model.Request, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.requests[id]
	if !ok {
		return nil, db.ErrNotFound
	}
	return m.requestView(r), nil
}

func (m *Memory) DecideRequest(ctx context.Context, id, status string, deviceID *string, now time.Time) (*model.Request, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.requests[id]
	if !ok {
		return nil, db.ErrNotFound
	}
	if r.Status != model.StatusPending || !r.ExpiresAt.After(now) {
		return nil, db.ErrAlreadyDecided
	}
	t := now.UTC()
	by := model.DecidedByAdmin
	r.Status = status
	r.DecidedBy = &by
	r.DecidedByDevice = deviceID
	r.DecidedAt = &t
	return m.requestView(r), nil
}

func (m *Memory) MarkTimeout(ctx context.Context, id string, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.requests[id]
	if !ok {
		return false, db.ErrNotFound
	}
	if r.Status != model.StatusPending {
		return false, nil
	}
	by := model.DecidedByTimeout
	r.Status = model.StatusTimeout
	r.DecidedBy = &by
	r.DecidedAt = nil
	return true, nil
}

func (m *Memory) ListRequests(ctx context.Context, f db.RequestFilter) ([]model.Request, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]model.Request, 0)
	for _, r := range m.requests {
		if f.Before != nil && !r.CreatedAt.Before(*f.Before) {
			continue
		}
		if f.Server != "" && m.serverName(r.ServerID) != f.Server {
			continue
		}
		if f.Username != "" && r.Username != f.Username {
			continue
		}
		if f.Context != "" && r.Context != f.Context {
			continue
		}
		if f.Status != "" && r.Status != f.Status {
			continue
		}
		out = append(out, *m.requestView(r))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (m *Memory) CountDenials(ctx context.Context, ip string, since time.Time) (db.DenialStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var st db.DenialStats
	for _, r := range m.requests {
		if r.Status != model.StatusDenied || r.SourceIP == nil || *r.SourceIP != ip || r.DecidedAt == nil {
			continue
		}
		if r.DecidedAt.Before(since) {
			continue
		}
		st.Count++
		if st.First.IsZero() || r.DecidedAt.Before(st.First) {
			st.First = *r.DecidedAt
		}
		if st.Last.IsZero() || r.DecidedAt.After(st.Last) {
			st.Last = *r.DecidedAt
		}
	}
	return st, nil
}
