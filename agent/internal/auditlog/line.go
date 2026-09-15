// Package auditlog writes the one-line decision record documented in agent/README.md
// ("Log format"). The format is stable: fail2ban and operators parse it.
package auditlog

import (
	"fmt"
	"io"
	"strings"
	"unicode"
)

// Decision values.
const (
	Allow = "allow"
	Deny  = "deny"
)

// Line is one decision. Empty values are written as "-".
type Line struct {
	Decision string // allow | deny
	Context  string // ssh | sudo
	User     string
	RHost    string // source IP, "" when unknown
	Host     string
	Reason   string
	Request  string // backend request id, "" when the backend was never reached
}

// String renders the key=value pairs in the documented order.
func (l Line) String() string {
	return "decision=" + field(l.Decision) +
		" context=" + field(l.Context) +
		" user=" + field(l.User) +
		" rhost=" + field(l.RHost) +
		" host=" + field(l.Host) +
		" reason=" + field(l.Reason) +
		" request=" + field(l.Request)
}

// field keeps every value a single token so the line always splits on spaces.
func field(s string) string {
	if s == "" {
		return "-"
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, s)
}

// Logger receives one Line per decision.
type Logger interface {
	Log(Line)
}

// WriterLogger writes lines to W. Used by tests and as the fallback when the system log is
// unavailable.
type WriterLogger struct{ W io.Writer }

// Log writes the line followed by a newline.
func (w WriterLogger) Log(l Line) { fmt.Fprintln(w.W, l.String()) }

// Recorder keeps lines in memory, for tests.
type Recorder struct{ Lines []Line }

// Log appends the line.
func (r *Recorder) Log(l Line) { r.Lines = append(r.Lines, l) }

// Last returns the most recent line, or a zero Line.
func (r *Recorder) Last() Line {
	if len(r.Lines) == 0 {
		return Line{}
	}
	return r.Lines[len(r.Lines)-1]
}
