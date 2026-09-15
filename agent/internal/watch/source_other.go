//go:build !windows

package watch

import "time"

// NewSystemSource returns nil outside Windows: there is no event log to follow, and the
// Linux agent is the PAM hook instead.
func NewSystemSource(channel string, lookback time.Duration) Source { return nil }
