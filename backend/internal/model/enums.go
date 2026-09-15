// Package model holds the types shared by the handlers, the rules engine and
// the database layer: the enumerations of the contract, the database rows and
// the JSON shapes sent to the agent and the phones.
package model

// Contexts of a request.
const (
	ContextSSH  = "ssh"
	ContextSudo = "sudo"
)

// Modes of a request.
const (
	ModeEnforce = "enforce"
	ModeNotify  = "notify"
)

// Request statuses.
const (
	StatusPending     = "pending"
	StatusApproved    = "approved"
	StatusDenied      = "denied"
	StatusTimeout     = "timeout"
	StatusWhitelisted = "whitelisted"
	StatusBlockedIP   = "blocked_ip"
	StatusBlockedGeo  = "blocked_geo"
	StatusNotified    = "notified"
)

// Values of requests.decided_by.
const (
	DecidedByAdmin     = "admin"
	DecidedByWhitelist = "whitelist"
	DecidedByTimeout   = "timeout"
	DecidedByAutoblock = "autoblock"
	DecidedByGeoRule   = "georule"
)

// Verdicts sent by the app.
const (
	VerdictApprove       = "approve"
	VerdictDeny          = "deny"
	VerdictApproveAlways = "approve_always"
)

// Reasons returned to the agent.
const (
	ReasonAdmin      = "admin"
	ReasonWhitelist  = "whitelist"
	ReasonTimeout    = "timeout"
	ReasonBlockedIP  = "blocked_ip"
	ReasonBlockedGeo = "blocked_geo"
	ReasonNotify     = "notify"
)

// Push message types.
const (
	PushAccessRequest  = "access_request"
	PushAccessNotice   = "access_notice"
	PushRequestDecided = "request_decided"
)

// Device platforms.
const (
	PlatformAndroid = "android"
	PlatformIOS     = "ios"
)

// Server operating systems.
const (
	OSLinux   = "linux"
	OSWindows = "windows"
	OSDarwin  = "darwin"
)

// ValidContext reports whether s is a context of the contract.
func ValidContext(s string) bool { return s == ContextSSH || s == ContextSudo }

// ValidMode reports whether s is a mode of the contract.
func ValidMode(s string) bool { return s == ModeEnforce || s == ModeNotify }

// ValidStatus reports whether s is a request status of the contract.
func ValidStatus(s string) bool {
	switch s {
	case StatusPending, StatusApproved, StatusDenied, StatusTimeout,
		StatusWhitelisted, StatusBlockedIP, StatusBlockedGeo, StatusNotified:
		return true
	}
	return false
}

// ValidPlatform reports whether s is a device platform of the contract.
func ValidPlatform(s string) bool { return s == PlatformAndroid || s == PlatformIOS }

// ValidOS reports whether s is a server operating system of the schema.
func ValidOS(s string) bool { return s == OSLinux || s == OSWindows || s == OSDarwin }

// VerdictFor maps a final request status to the verdict and reason returned
// to the agent. Pending has no verdict yet; the caller must not ask for it.
func VerdictFor(status string) (verdict, reason string) {
	switch status {
	case StatusApproved:
		return VerdictApprove, ReasonAdmin
	case StatusDenied:
		return VerdictDeny, ReasonAdmin
	case StatusWhitelisted:
		return VerdictApprove, ReasonWhitelist
	case StatusNotified:
		return VerdictApprove, ReasonNotify
	case StatusBlockedIP:
		return VerdictDeny, ReasonBlockedIP
	case StatusBlockedGeo:
		return VerdictDeny, ReasonBlockedGeo
	default: // timeout, and anything unexpected: fail closed
		return VerdictDeny, ReasonTimeout
	}
}
