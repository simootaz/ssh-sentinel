package watch

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// acceptedRE matches the sshd success line, the same on Windows and Linux:
// "Accepted publickey for deploy from 203.0.113.42 port 51234 ssh2: ...". The IP is a run of
// non-space characters so IPv6 works too.
var acceptedRE = regexp.MustCompile(`Accepted (\S+) for (\S+) from (\S+) port \d+`)

// ParseAccepted extracts the user, source IP and method from an sshd message.
func ParseAccepted(message string) (username, sourceIP, method string, ok bool) {
	m := acceptedRE.FindStringSubmatch(message)
	if m == nil {
		return "", "", "", false
	}
	return m[2], m[3], m[1], true
}

// RawEvent is one <Event> as printed by "wevtutil qe ... /f:xml": its record id, timestamp
// and every EventData value, whatever the Data Name attribute says.
type RawEvent struct {
	RecordID uint64
	Time     time.Time
	Data     []string
}

type xmlEvents struct {
	Events []struct {
		System struct {
			EventRecordID uint64 `xml:"EventRecordID"`
			TimeCreated   struct {
				SystemTime string `xml:"SystemTime,attr"`
			} `xml:"TimeCreated"`
		} `xml:"System"`
		EventData struct {
			Data []struct {
				Name  string `xml:"Name,attr"`
				Value string `xml:",chardata"`
			} `xml:"Data"`
		} `xml:"EventData"`
	} `xml:"Event"`
}

// ParseWevtutilXML decodes wevtutil's output. The tool prints <Event> elements back to back
// with no root element, so the data is wrapped in one before decoding.
func ParseWevtutilXML(data []byte) ([]RawEvent, error) {
	data = bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
	if len(data) == 0 {
		return nil, nil
	}
	wrapped := make([]byte, 0, len(data)+17)
	wrapped = append(wrapped, "<Events>"...)
	wrapped = append(wrapped, data...)
	wrapped = append(wrapped, "</Events>"...)

	var doc xmlEvents
	if err := xml.Unmarshal(wrapped, &doc); err != nil {
		return nil, fmt.Errorf("parse event xml: %w", err)
	}
	out := make([]RawEvent, 0, len(doc.Events))
	for _, e := range doc.Events {
		raw := RawEvent{RecordID: e.System.EventRecordID}
		if t, err := time.Parse(time.RFC3339Nano, e.System.TimeCreated.SystemTime); err == nil {
			raw.Time = t
		}
		for _, d := range e.EventData.Data {
			raw.Data = append(raw.Data, strings.TrimSpace(d.Value))
		}
		out = append(out, raw)
	}
	return out, nil
}

// ToEvents keeps the raw events that carry an accepted login and fills in the fields.
func ToEvents(raw []RawEvent) []Event {
	var events []Event
	for _, r := range raw {
		for _, d := range r.Data {
			user, ip, method, ok := ParseAccepted(d)
			if !ok {
				continue
			}
			events = append(events, Event{RecordID: r.RecordID, Time: r.Time, Username: user, SourceIP: ip, Method: method})
			break
		}
	}
	return events
}
