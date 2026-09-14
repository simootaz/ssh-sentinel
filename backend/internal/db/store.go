// Package db is the database layer: the Store interface the handlers and the
// rules talk to, and its PostgreSQL implementation. Standard SQL only, any
// PostgreSQL 14 or newer.
package db

import (
	"context"
	"errors"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// Errors returned by the store. Handlers map them to 404 and 409.
var (
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("already exists")
	ErrAlreadyDecided = errors.New("request already decided")
)

// RequestFilter selects rows for GET /history. Empty strings and nil mean "no filter".
type RequestFilter struct {
	Limit    int
	Before   *time.Time // created strictly before this instant
	Server   string     // server name
	Username string
	Context  string
	Status   string
}

// DenialStats summarises the admin denials from one IP inside a window.
type DenialStats struct {
	Count int
	First time.Time // earliest decided_at counted; zero when Count is 0
	Last  time.Time // latest decided_at counted; zero when Count is 0
}

// Store is every query the backend runs. The "now" parameters make expiry
// checks deterministic: the caller's clock decides, not the database's.
// Every time.Time returned is in UTC.
type Store interface {
	// Ping checks that the database answers. Used by GET /healthz.
	Ping(ctx context.Context) error

	// Servers. Enrollment is a script or the enroll subcommand, never a route.
	CreateServer(ctx context.Context, name, tokenHash, os string, now time.Time) (*model.Server, error) // ErrConflict on a duplicate name or hash
	ServerByTokenHash(ctx context.Context, hash string) (*model.Server, error)                          // ErrNotFound
	ServerByName(ctx context.Context, name string) (*model.Server, error)                               // ErrNotFound
	TouchServer(ctx context.Context, id string, now time.Time) error                                    // sets last_seen_at

	// Devices.
	UpsertDevice(ctx context.Context, fcmToken, platform string, label *string, now time.Time) (dev *model.Device, created bool, err error) // upsert on fcm_token; refreshes platform, label and last_seen_at and clears deleted_at, so a deleted phone that registers again comes back (created is false)
	DeviceByID(ctx context.Context, id string) (*model.Device, error)                                                                       // ErrNotFound, also for a deleted device
	ListDevices(ctx context.Context) ([]model.Device, error)                                                                                // devices not deleted, ordered by created_at
	DeleteDevice(ctx context.Context, id string, now time.Time) error                                                                       // sets deleted_at = now; the row stays so history keeps the label (contract, DELETE /devices). ErrNotFound when unknown or already deleted

	// Requests.
	CreateRequest(ctx context.Context, r *model.Request) (*model.Request, error)                                   // inserts r as given (status, decided_by, decided_at, created_at, expires_at); returns the row with its id and server name
	RequestByID(ctx context.Context, id string) (*model.Request, error)                                            // ErrNotFound; server name and device label joined
	DecideRequest(ctx context.Context, id, status string, deviceID *string, now time.Time) (*model.Request, error) // UPDATE ... WHERE status = 'pending' AND expires_at > now; sets decided_by = 'admin', decided_by_device, decided_at = now. ErrNotFound, ErrAlreadyDecided
	MarkTimeout(ctx context.Context, id string, now time.Time) (bool, error)                                       // status = 'timeout', decided_by = 'timeout', decided_at stays NULL; false when the row was not pending
	ListRequests(ctx context.Context, f RequestFilter) ([]model.Request, error)                                    // newest first, at most f.Limit rows
	CountDenials(ctx context.Context, ip string, since time.Time) (DenialStats, error)                             // rows with status = 'denied', this source_ip, decided_at >= since

	// Whitelist.
	FindWhitelist(ctx context.Context, username, context, serverID string, now time.Time) (*model.WhitelistEntry, error)                // unexpired entry for this server, else the global one; ErrNotFound
	ListWhitelist(ctx context.Context, serverID *string, now time.Time) ([]model.WhitelistEntry, error)                                 // nil: every unexpired entry; else that server's plus the global ones. Deletes expired rows first
	UpsertWhitelist(ctx context.Context, e *model.WhitelistEntry, now time.Time) (entry *model.WhitelistEntry, created bool, err error) // on (username, context, server_id): insert, or refresh expires_at of the existing row
	WhitelistByID(ctx context.Context, id string) (*model.WhitelistEntry, error)                                                        // ErrNotFound; server name and device label joined
	DeleteWhitelist(ctx context.Context, id string) error                                                                               // ErrNotFound

	// Blocked IPs.
	BlockedIPByIP(ctx context.Context, ip string) (*model.BlockedIP, error)            // ErrNotFound; returns the row whether expired or not
	RecordBlockedIPHit(ctx context.Context, id string, now time.Time) error            // hit_count + 1, last_hit_at = now
	ListBlockedIPs(ctx context.Context, now time.Time) ([]model.BlockedIP, error)      // active blocks only, newest first
	UpsertBlockedIP(ctx context.Context, b *model.BlockedIP) (*model.BlockedIP, error) // insert, or on the same ip re-arm the row: reason, denial_count, first/last_denied_at, created_at and expires_at from b, hit_count back to 0 and last_hit_at to NULL; the id is kept
	UnblockIP(ctx context.Context, id string, now time.Time) error                     // expires_at = now; ErrNotFound when unknown or already expired

	// Geo rules.
	ListGeoRules(ctx context.Context) ([]model.GeoRule, error)                                              // ordered by country
	GeoRuleByCountry(ctx context.Context, country string) (*model.GeoRule, error)                           // ErrNotFound
	CreateGeoRule(ctx context.Context, country string, note *string, now time.Time) (*model.GeoRule, error) // ErrConflict
	DeleteGeoRule(ctx context.Context, id string) error                                                     // ErrNotFound
}
