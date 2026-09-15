package model

import "time"

// WhitelistItem is one element of GET /whitelist and the body of POST /whitelist answers.
type WhitelistItem struct {
	ID                 string     `json:"id"`
	Username           string     `json:"username"`
	Context            string     `json:"context"`
	Server             *string    `json:"server"`
	ExpiresAt          *time.Time `json:"expires_at"`
	CreatedAt          time.Time  `json:"created_at"`
	CreatedFromRequest *string    `json:"created_from_request"`
	CreatedByDevice    *string    `json:"created_by_device"`
}

// WhitelistBody is what the app sends to POST /whitelist.
type WhitelistBody struct {
	Username   string  `json:"username"`
	Context    string  `json:"context"`
	Server     *string `json:"server"`
	TTLSeconds *int64  `json:"ttl_seconds"`
}

// DeviceBody is what the app sends to POST /devices.
type DeviceBody struct {
	FCMToken string  `json:"fcm_token"`
	Platform string  `json:"platform"`
	Label    *string `json:"label"`
}

// DeviceItem is a device as returned by the API. The FCM token is never returned.
type DeviceItem struct {
	ID         string     `json:"id"`
	Platform   string     `json:"platform"`
	Label      *string    `json:"label"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at"`
}

// BlockedIPItem is one element of GET /blocked-ips.
type BlockedIPItem struct {
	ID            string     `json:"id"`
	IP            string     `json:"ip"`
	Reason        string     `json:"reason"`
	DenialCount   int        `json:"denial_count"`
	FirstDeniedAt time.Time  `json:"first_denied_at"`
	LastDeniedAt  time.Time  `json:"last_denied_at"`
	HitCount      int        `json:"hit_count"`
	LastHitAt     *time.Time `json:"last_hit_at"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     *time.Time `json:"expires_at"`
}

// GeoRuleBody is what the app sends to POST /geo-rules.
type GeoRuleBody struct {
	Country string  `json:"country"`
	Note    *string `json:"note"`
}

// GeoRuleItem is one element of GET /geo-rules and the body of the POST answer.
type GeoRuleItem struct {
	ID        string    `json:"id"`
	Country   string    `json:"country"`
	Note      *string   `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

// ListResponse is the {"items": [...]} envelope of the unpaged list routes.
type ListResponse[T any] struct {
	Items []T `json:"items"`
}

// HealthResponse is the answer of GET /healthz.
type HealthResponse struct {
	Status string `json:"status"`
	DB     string `json:"db"`
}
