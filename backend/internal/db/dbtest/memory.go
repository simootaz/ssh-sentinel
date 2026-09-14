// Package dbtest provides an in-memory Store for the handler and rules tests.
// It follows the semantics documented on db.Store (conditional updates,
// expiry, lazy deletion) so that the tests exercise the same paths as the
// PostgreSQL implementation, without a database.
package dbtest

import (
	"context"
	"crypto/rand"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// Memory is an in-memory db.Store. Safe for concurrent use.
type Memory struct {
	mu        sync.Mutex
	servers   map[string]*model.Server
	devices   map[string]*model.Device
	requests  map[string]*model.Request
	whitelist map[string]*model.WhitelistEntry
	blocked   map[string]*model.BlockedIP
	geoRules  map[string]*model.GeoRule

	// PingErr makes Ping fail, to test GET /healthz degraded.
	PingErr error
}

var _ db.Store = (*Memory)(nil)

// New returns an empty store.
func New() *Memory {
	return &Memory{
		servers:   map[string]*model.Server{},
		devices:   map[string]*model.Device{},
		requests:  map[string]*model.Request{},
		whitelist: map[string]*model.WhitelistEntry{},
		blocked:   map[string]*model.BlockedIP{},
		geoRules:  map[string]*model.GeoRule{},
	}
}

// NewID returns a random UUID v4 string.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (m *Memory) Ping(ctx context.Context) error { return m.PingErr }

// Servers

func (m *Memory) CreateServer(ctx context.Context, name, tokenHash, os string, now time.Time) (*model.Server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.servers {
		if s.Name == name || s.TokenHash == tokenHash {
			return nil, db.ErrConflict
		}
	}
	s := &model.Server{ID: NewID(), Name: name, TokenHash: tokenHash, OS: os, CreatedAt: now.UTC()}
	m.servers[s.ID] = s
	c := *s
	return &c, nil
}

func (m *Memory) ServerByTokenHash(ctx context.Context, hash string) (*model.Server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.servers {
		if s.TokenHash == hash {
			c := *s
			return &c, nil
		}
	}
	return nil, db.ErrNotFound
}

func (m *Memory) ServerByName(ctx context.Context, name string) (*model.Server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.servers {
		if s.Name == name {
			c := *s
			return &c, nil
		}
	}
	return nil, db.ErrNotFound
}

func (m *Memory) TouchServer(ctx context.Context, id string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.servers[id]
	if !ok {
		return db.ErrNotFound
	}
	t := now.UTC()
	s.LastSeenAt = &t
	return nil
}

func (m *Memory) serverName(id string) string {
	if s, ok := m.servers[id]; ok {
		return s.Name
	}
	return ""
}

func (m *Memory) deviceLabel(id *string) *string {
	if id == nil {
		return nil
	}
	if d, ok := m.devices[*id]; ok {
		return d.Label
	}
	return nil
}

// Devices

func (m *Memory) UpsertDevice(ctx context.Context, fcmToken, platform string, label *string, now time.Time) (*model.Device, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := now.UTC()
	for _, d := range m.devices {
		if d.FCMToken == fcmToken {
			d.Platform = platform
			d.Label = label
			d.LastSeenAt = &t
			d.DeletedAt = nil // registering again brings a deleted phone back
			c := *d
			return &c, false, nil
		}
	}
	d := &model.Device{ID: NewID(), FCMToken: fcmToken, Platform: platform, Label: label, CreatedAt: t, LastSeenAt: &t}
	m.devices[d.ID] = d
	c := *d
	return &c, true, nil
}

func (m *Memory) DeviceByID(ctx context.Context, id string) (*model.Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[id]
	if !ok || d.DeletedAt != nil {
		return nil, db.ErrNotFound
	}
	c := *d
	return &c, nil
}

func (m *Memory) ListDevices(ctx context.Context) ([]model.Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]model.Device, 0, len(m.devices))
	for _, d := range m.devices {
		if d.DeletedAt == nil {
			out = append(out, *d)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (m *Memory) DeleteDevice(ctx context.Context, id string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[id]
	if !ok || d.DeletedAt != nil {
		return db.ErrNotFound
	}
	// Soft delete: the row stays so that requests and whitelist entries keep
	// showing the label; the phone is simply no longer listed nor pushed to.
	t := now.UTC()
	d.DeletedAt = &t
	return nil
}

// Geo rules

func (m *Memory) ListGeoRules(ctx context.Context) ([]model.GeoRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]model.GeoRule, 0, len(m.geoRules))
	for _, g := range m.geoRules {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Country < out[j].Country })
	return out, nil
}

func (m *Memory) GeoRuleByCountry(ctx context.Context, country string) (*model.GeoRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, g := range m.geoRules {
		if g.Country == country {
			c := *g
			return &c, nil
		}
	}
	return nil, db.ErrNotFound
}

func (m *Memory) CreateGeoRule(ctx context.Context, country string, note *string, now time.Time) (*model.GeoRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, g := range m.geoRules {
		if g.Country == country {
			return nil, db.ErrConflict
		}
	}
	g := &model.GeoRule{ID: NewID(), Country: country, Note: note, CreatedAt: now.UTC()}
	m.geoRules[g.ID] = g
	c := *g
	return &c, nil
}

func (m *Memory) DeleteGeoRule(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.geoRules[id]; !ok {
		return db.ErrNotFound
	}
	delete(m.geoRules, id)
	return nil
}
