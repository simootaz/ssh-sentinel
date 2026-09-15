//go:build windows

package watch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode/utf16"
)

// WevtutilSource reads new event ID 4 records from a channel by running wevtutil.
type WevtutilSource struct {
	Channel string
	// Lookback, when set, makes the first call return the logins of that window instead of
	// only priming the record id. Used by "watch --once".
	Lookback time.Duration

	last   uint64
	primed bool
}

// NewSystemSource returns the wevtutil source for channel.
func NewSystemSource(channel string, lookback time.Duration) Source {
	return &WevtutilSource{Channel: channel, Lookback: lookback}
}

// Next returns the accepted logins recorded since the previous call.
func (s *WevtutilSource) Next(ctx context.Context) ([]Event, error) {
	if !s.primed {
		s.primed = true
		if s.Lookback > 0 {
			q := fmt.Sprintf("*[System[(EventID=4) and TimeCreated[timediff(@SystemTime) <= %d]]]", s.Lookback.Milliseconds())
			raw, err := s.query(ctx, q)
			if err != nil {
				return nil, err
			}
			s.advance(raw)
			return ToEvents(raw), nil
		}
		raw, err := s.query(ctx, "*[System[EventID=4]]", "/c:1", "/rd:true")
		if err != nil {
			return nil, err
		}
		s.advance(raw)
		return nil, nil
	}
	raw, err := s.query(ctx, fmt.Sprintf("*[System[(EventID=4) and (EventRecordID > %d)]]", s.last))
	if err != nil {
		return nil, err
	}
	s.advance(raw)
	return ToEvents(raw), nil
}

func (s *WevtutilSource) advance(raw []RawEvent) {
	for _, r := range raw {
		if r.RecordID > s.last {
			s.last = r.RecordID
		}
	}
}

func (s *WevtutilSource) query(ctx context.Context, q string, extra ...string) ([]RawEvent, error) {
	args := append([]string{"qe", s.Channel, "/q:" + q, "/f:xml"}, extra...)
	out, err := exec.CommandContext(ctx, "wevtutil", args...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("wevtutil: %s", strings.TrimSpace(decodeOutput(ee.Stderr)))
		}
		return nil, fmt.Errorf("wevtutil: %w", err)
	}
	return ParseWevtutilXML([]byte(decodeOutput(out)))
}

// decodeOutput turns wevtutil's output into a Go string. Depending on the console the tool
// prints UTF-16 (with a byte order mark) or the ANSI code page; both are handled.
func decodeOutput(b []byte) string {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		b = b[2:]
		u := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
		}
		return string(utf16.Decode(u))
	}
	return string(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")))
}
