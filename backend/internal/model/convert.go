package model

import "time"

// Conversions from rows to the JSON shapes of the contract. They live here
// because several route groups need the same shape (a whitelist entry is
// returned by the whitelist routes and by the verdict route, for instance).

// ToHistoryItem converts a request row to its GET /history shape.
func ToHistoryItem(r *Request) HistoryItem {
	return HistoryItem{
		ID:              r.ID,
		Server:          r.ServerName,
		Hostname:        r.Hostname,
		Context:         r.Context,
		Username:        r.Username,
		SourceIP:        r.SourceIP,
		TTY:             r.TTY,
		Command:         r.Command,
		Geo:             r.Geo,
		Status:          r.Status,
		DecidedBy:       r.DecidedBy,
		DecidedByDevice: r.DecidedByDeviceLabel,
		CreatedAt:       r.CreatedAt,
		ExpiresAt:       r.ExpiresAt,
		DecidedAt:       r.DecidedAt,
	}
}

// ToWhitelistItem converts a whitelist row to its GET /whitelist shape.
func ToWhitelistItem(e *WhitelistEntry) WhitelistItem {
	return WhitelistItem{
		ID:                 e.ID,
		Username:           e.Username,
		Context:            e.Context,
		Server:             e.ServerName,
		ExpiresAt:          e.ExpiresAt,
		CreatedAt:          e.CreatedAt,
		CreatedFromRequest: e.CreatedFrom,
		CreatedByDevice:    e.CreatedByDeviceLabel,
	}
}

// ToVerdictWhitelistEntry converts a whitelist row to the whitelist_entry
// field of the POST /verdict answer.
func ToVerdictWhitelistEntry(e *WhitelistEntry) VerdictWhitelistEntry {
	return VerdictWhitelistEntry{
		ID:        e.ID,
		Username:  e.Username,
		Context:   e.Context,
		Server:    e.ServerName,
		ExpiresAt: e.ExpiresAt,
	}
}

// ToDeviceItem converts a device row to its API shape, without the FCM token.
func ToDeviceItem(d *Device) DeviceItem {
	return DeviceItem{
		ID:         d.ID,
		Platform:   d.Platform,
		Label:      d.Label,
		CreatedAt:  d.CreatedAt,
		LastSeenAt: d.LastSeenAt,
	}
}

// ToBlockedIPItem converts a blocked_ips row to its GET /blocked-ips shape.
func ToBlockedIPItem(b *BlockedIP) BlockedIPItem {
	return BlockedIPItem{
		ID:            b.ID,
		IP:            b.IP,
		Reason:        b.Reason,
		DenialCount:   b.DenialCount,
		FirstDeniedAt: b.FirstDeniedAt,
		LastDeniedAt:  b.LastDeniedAt,
		HitCount:      b.HitCount,
		LastHitAt:     b.LastHitAt,
		CreatedAt:     b.CreatedAt,
		ExpiresAt:     b.ExpiresAt,
	}
}

// ToGeoRuleItem converts a geo_rules row to its API shape.
func ToGeoRuleItem(g *GeoRule) GeoRuleItem {
	return GeoRuleItem{ID: g.ID, Country: g.Country, Note: g.Note, CreatedAt: g.CreatedAt}
}

// FormatTime renders a time the way the contract wants it in push messages:
// RFC 3339, UTC.
func FormatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// Push payloads. FCM data messages carry strings only; an empty string means
// unknown (docs/architecture.md, section 5, "Push messages").

// AccessPush builds the access_request or access_notice payload for a request.
func AccessPush(typ string, r *Request) map[string]string {
	country, city := "", ""
	if r.Geo != nil {
		country, city = r.Geo.Country, r.Geo.City
	}
	return map[string]string{
		"type":        typ,
		"request_id":  r.ID,
		"context":     r.Context,
		"server":      r.ServerName,
		"username":    r.Username,
		"source_ip":   deref(r.SourceIP),
		"geo_country": country,
		"geo_city":    city,
		"command":     deref(r.Command),
		"created_at":  FormatTime(r.CreatedAt),
		"expires_at":  FormatTime(r.ExpiresAt),
	}
}

// DecidedPush builds the request_decided payload sent after a verdict.
func DecidedPush(r *Request) map[string]string {
	return map[string]string{
		"type":              PushRequestDecided,
		"request_id":        r.ID,
		"status":            r.Status,
		"decided_by_device": deref(r.DecidedByDeviceLabel),
	}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
