package dbtest

import (
	"context"
	"sort"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// Whitelist

func (m *Memory) whitelistView(e *model.WhitelistEntry) *model.WhitelistEntry {
	c := *e
	if e.ServerID != nil {
		n := m.serverName(*e.ServerID)
		c.ServerName = &n
	}
	c.CreatedByDeviceLabel = m.deviceLabel(e.CreatedByDevice)
	return &c
}

func unexpired(exp *time.Time, now time.Time) bool { return exp == nil || exp.After(now) }

func (m *Memory) FindWhitelist(ctx context.Context, username, context, serverID string, now time.Time) (*model.WhitelistEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var global *model.WhitelistEntry
	for _, e := range m.whitelist {
		if e.Username != username || e.Context != context || !unexpired(e.ExpiresAt, now) {
			continue
		}
		if e.ServerID == nil {
			global = e
			continue
		}
		if *e.ServerID == serverID {
			return m.whitelistView(e), nil
		}
	}
	if global != nil {
		return m.whitelistView(global), nil
	}
	return nil, db.ErrNotFound
}

func (m *Memory) ListWhitelist(ctx context.Context, serverID *string, now time.Time) ([]model.WhitelistEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, e := range m.whitelist { // lazy deletion of expired rows
		if !unexpired(e.ExpiresAt, now) {
			delete(m.whitelist, id)
		}
	}
	out := make([]model.WhitelistEntry, 0)
	for _, e := range m.whitelist {
		if serverID != nil && e.ServerID != nil && *e.ServerID != *serverID {
			continue
		}
		out = append(out, *m.whitelistView(e))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (m *Memory) UpsertWhitelist(ctx context.Context, e *model.WhitelistEntry, now time.Time) (*model.WhitelistEntry, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.ServerID != nil {
		if _, ok := m.servers[*e.ServerID]; !ok {
			return nil, false, db.ErrNotFound
		}
	}
	for _, x := range m.whitelist {
		sameServer := (x.ServerID == nil && e.ServerID == nil) ||
			(x.ServerID != nil && e.ServerID != nil && *x.ServerID == *e.ServerID)
		if x.Username == e.Username && x.Context == e.Context && sameServer {
			x.ExpiresAt = utcp(e.ExpiresAt)
			return m.whitelistView(x), false, nil
		}
	}
	c := *e
	c.ID = NewID()
	c.CreatedAt = now.UTC()
	c.ExpiresAt = utcp(e.ExpiresAt)
	m.whitelist[c.ID] = &c
	return m.whitelistView(&c), true, nil
}

func (m *Memory) WhitelistByID(ctx context.Context, id string) (*model.WhitelistEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.whitelist[id]
	if !ok {
		return nil, db.ErrNotFound
	}
	return m.whitelistView(e), nil
}

func (m *Memory) DeleteWhitelist(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.whitelist[id]; !ok {
		return db.ErrNotFound
	}
	delete(m.whitelist, id)
	return nil
}

// Blocked IPs

func (m *Memory) BlockedIPByIP(ctx context.Context, ip string) (*model.BlockedIP, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.blocked {
		if b.IP == ip {
			c := *b
			return &c, nil
		}
	}
	return nil, db.ErrNotFound
}

func (m *Memory) RecordBlockedIPHit(ctx context.Context, id string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.blocked[id]
	if !ok {
		return db.ErrNotFound
	}
	t := now.UTC()
	b.HitCount++
	b.LastHitAt = &t
	return nil
}

func (m *Memory) ListBlockedIPs(ctx context.Context, now time.Time) ([]model.BlockedIP, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]model.BlockedIP, 0)
	for _, b := range m.blocked {
		if b.Active(now) {
			out = append(out, *b)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (m *Memory) UpsertBlockedIP(ctx context.Context, b *model.BlockedIP) (*model.BlockedIP, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := *b
	c.CreatedAt = b.CreatedAt.UTC()
	c.FirstDeniedAt = b.FirstDeniedAt.UTC()
	c.LastDeniedAt = b.LastDeniedAt.UTC()
	c.ExpiresAt = utcp(b.ExpiresAt)
	c.HitCount = 0 // a re-armed block starts counting hits again
	c.LastHitAt = nil
	if c.Reason == "" {
		c.Reason = "autoblock"
	}
	for _, x := range m.blocked {
		if x.IP == b.IP {
			c.ID = x.ID
			*x = c
			return &c, nil
		}
	}
	c.ID = NewID()
	m.blocked[c.ID] = &c
	out := c
	return &out, nil
}

func (m *Memory) UnblockIP(ctx context.Context, id string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.blocked[id]
	if !ok || !b.Active(now) {
		return db.ErrNotFound
	}
	t := now.UTC()
	b.ExpiresAt = &t
	return nil
}
