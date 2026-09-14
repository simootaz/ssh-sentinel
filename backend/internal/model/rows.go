package model

import "time"

// The types below mirror the tables of docs/architecture.md, section 4. Ids
// are UUIDs kept as strings: the standard library has no UUID type and the
// database validates the format. Pointers mark nullable columns.

// Server is a row of servers: one protected machine, one bearer token.
type Server struct {
	ID         string
	Name       string
	TokenHash  string
	OS         string
	CreatedAt  time.Time
	LastSeenAt *time.Time
}

// Device is a row of devices: one phone, one FCM token. DeletedAt is set by
// DELETE /devices: the row stays so that history keeps the label, but the
// phone is no longer listed nor pushed to.
type Device struct {
	ID         string
	FCMToken   string
	Platform   string
	Label      *string
	CreatedAt  time.Time
	LastSeenAt *time.Time
	DeletedAt  *time.Time
}

// Geo is the geolocation of a source IP. Any key may be empty.
type Geo struct {
	Country string `json:"country,omitempty"`
	City    string `json:"city,omitempty"`
	ASN     string `json:"asn,omitempty"`
}

// Request is a row of requests, with the server name and the device label
// joined in for display.
type Request struct {
	ID                   string
	ServerID             string
	ServerName           string
	Context              string
	Username             string
	SourceIP             *string
	Hostname             string
	TTY                  *string
	Command              *string
	Geo                  *Geo
	Status               string
	DecidedBy            *string
	DecidedByDevice      *string // device id
	DecidedByDeviceLabel *string
	CreatedAt            time.Time
	ExpiresAt            time.Time
	DecidedAt            *time.Time
}

// WhitelistEntry is a row of whitelist, with the server name and the device
// label joined in. ServerID nil means every server; ExpiresAt nil means
// permanent.
type WhitelistEntry struct {
	ID                   string
	Username             string
	Context              string
	ServerID             *string
	ServerName           *string
	ExpiresAt            *time.Time
	CreatedAt            time.Time
	CreatedFrom          *string // request id
	CreatedByDevice      *string // device id
	CreatedByDeviceLabel *string
}

// BlockedIP is a row of blocked_ips. ExpiresAt nil means until unblocked;
// unblocking from the app sets ExpiresAt to the unblock time so that the
// denials before it are not counted again.
type BlockedIP struct {
	ID            string
	IP            string
	Reason        string
	DenialCount   int
	FirstDeniedAt time.Time
	LastDeniedAt  time.Time
	HitCount      int
	LastHitAt     *time.Time
	CreatedAt     time.Time
	ExpiresAt     *time.Time
}

// Active reports whether the block is in force at now.
func (b *BlockedIP) Active(now time.Time) bool {
	return b.ExpiresAt == nil || b.ExpiresAt.After(now)
}

// GeoRule is a row of geo_rules: one blocked country.
type GeoRule struct {
	ID        string
	Country   string
	Note      *string
	CreatedAt time.Time
}
