package model

import "time"

// JSON shapes of CONTRACT v1 (docs/architecture.md, section 5). The field
// names and their order follow the contract examples. Times are marshalled
// by encoding/json as RFC 3339; every time.Time coming out of the database
// layer is in UTC, so the output ends with "Z".

// ErrorResponse is the body of every error answer.
type ErrorResponse struct {
	Error string `json:"error"`
}

// AccessRequestBody is what the agent sends to POST /access-request.
type AccessRequestBody struct {
	Context  string  `json:"context"`
	Mode     string  `json:"mode"`
	Username string  `json:"username"`
	SourceIP *string `json:"source_ip"`
	Hostname string  `json:"hostname"`
	TTY      *string `json:"tty"`
	Command  *string `json:"command"`
}

// AccessRequestResponse is the 200 answer of POST /access-request.
type AccessRequestResponse struct {
	RequestID string     `json:"request_id"`
	Verdict   string     `json:"verdict"`
	Reason    string     `json:"reason"`
	DecidedAt *time.Time `json:"decided_at"`
}

// VerdictBody is what the app sends to POST /verdict.
type VerdictBody struct {
	RequestID  string  `json:"request_id"`
	Verdict    string  `json:"verdict"`
	TTLSeconds *int64  `json:"ttl_seconds"`
	DeviceID   *string `json:"device_id"`
}

// VerdictWhitelistEntry is the whitelist_entry field of the verdict answer.
type VerdictWhitelistEntry struct {
	ID        string     `json:"id"`
	Username  string     `json:"username"`
	Context   string     `json:"context"`
	Server    *string    `json:"server"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// VerdictAutoBlocked is the auto_blocked field of the verdict answer.
type VerdictAutoBlocked struct {
	ID          string `json:"id"`
	IP          string `json:"ip"`
	DenialCount int    `json:"denial_count"`
}

// VerdictResponse is the 200 answer of POST /verdict.
type VerdictResponse struct {
	RequestID       string                 `json:"request_id"`
	Status          string                 `json:"status"`
	DecidedAt       *time.Time             `json:"decided_at"`
	DecidedByDevice *string                `json:"decided_by_device"`
	WhitelistEntry  *VerdictWhitelistEntry `json:"whitelist_entry,omitempty"`
	AutoBlocked     *VerdictAutoBlocked    `json:"auto_blocked"`
}

// VerdictConflict is the 409 answer of POST /verdict.
type VerdictConflict struct {
	Error           string     `json:"error"`
	Status          string     `json:"status"`
	DecidedAt       *time.Time `json:"decided_at"`
	DecidedByDevice *string    `json:"decided_by_device"`
}

// HistoryItem is one element of GET /history.
type HistoryItem struct {
	ID              string     `json:"id"`
	Server          string     `json:"server"`
	Hostname        string     `json:"hostname"`
	Context         string     `json:"context"`
	Username        string     `json:"username"`
	SourceIP        *string    `json:"source_ip"`
	TTY             *string    `json:"tty"`
	Command         *string    `json:"command"`
	Geo             *Geo       `json:"geo"`
	Status          string     `json:"status"`
	DecidedBy       *string    `json:"decided_by"`
	DecidedByDevice *string    `json:"decided_by_device"`
	CreatedAt       time.Time  `json:"created_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
	DecidedAt       *time.Time `json:"decided_at"`
}

// HistoryResponse is the answer of GET /history.
type HistoryResponse struct {
	Items      []HistoryItem `json:"items"`
	NextBefore *time.Time    `json:"next_before,omitempty"`
}
