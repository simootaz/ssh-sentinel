package watch

import (
	"testing"
	"time"
)

func TestParseAccepted(t *testing.T) {
	cases := []struct {
		line             string
		user, ip, method string
		ok               bool
	}{
		{"Accepted publickey for deploy from 203.0.113.42 port 51234 ssh2: ED25519 SHA256:abc", "deploy", "203.0.113.42", "publickey", true},
		{"sshd: Accepted password for alice from 2001:db8::1 port 2222 ssh2", "alice", "2001:db8::1", "password", true},
		{"Accepted keyboard-interactive/pam for bob from 10.0.0.5 port 1 ssh2", "bob", "10.0.0.5", "keyboard-interactive/pam", true},
		{"Failed password for invalid user root from 203.0.113.42 port 1 ssh2", "", "", "", false},
		{"Disconnected from user deploy 203.0.113.42 port 51234", "", "", "", false},
		{"", "", "", "", false},
	}
	for _, tc := range cases {
		user, ip, method, ok := ParseAccepted(tc.line)
		if ok != tc.ok || user != tc.user || ip != tc.ip || method != tc.method {
			t.Errorf("ParseAccepted(%q) = %q %q %q %v, want %q %q %q %v", tc.line, user, ip, method, ok, tc.user, tc.ip, tc.method, tc.ok)
		}
	}
}

// sampleXML is two events the way "wevtutil qe OpenSSH/Operational /f:xml" prints them:
// one element per line, single-quoted attributes, no root element.
const sampleXML = `<Event xmlns='http://schemas.microsoft.com/win/2004/08/events/event'><System><Provider Name='OpenSSH' Guid='{c4b43d1a-7b8e-4b5a-8a0f-3b5b8c1c2d3e}'/><EventID>4</EventID><Version>0</Version><Level>4</Level><Task>0</Task><Opcode>0</Opcode><Keywords>0x8000000000000000</Keywords><TimeCreated SystemTime='2026-09-14T20:11:40.1234567Z'/><EventRecordID>1201</EventRecordID><Correlation/><Execution ProcessID='1234' ThreadID='5678'/><Channel>OpenSSH/Operational</Channel><Computer>win-01</Computer><Security UserID='S-1-5-18'/></System><EventData><Data Name='process'>sshd</Data><Data Name='payload'>Accepted publickey for deploy from 203.0.113.42 port 51234 ssh2: ED25519 SHA256:abc</Data></EventData></Event>
<Event xmlns='http://schemas.microsoft.com/win/2004/08/events/event'><System><Provider Name='OpenSSH'/><EventID>4</EventID><TimeCreated SystemTime='2026-09-14T20:12:01.0000000Z'/><EventRecordID>1202</EventRecordID><Channel>OpenSSH/Operational</Channel><Computer>win-01</Computer></System><EventData><Data Name='process'>sshd</Data><Data Name='payload'>Disconnected from user deploy 203.0.113.42 port 51234</Data></EventData></Event>
`

func TestParseWevtutilXML(t *testing.T) {
	raw, err := ParseWevtutilXML([]byte(sampleXML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(raw) != 2 {
		t.Fatalf("got %d events, want 2", len(raw))
	}
	if raw[0].RecordID != 1201 || raw[1].RecordID != 1202 {
		t.Errorf("record ids = %d, %d", raw[0].RecordID, raw[1].RecordID)
	}
	want := time.Date(2026, 9, 14, 20, 11, 40, 123456700, time.UTC)
	if !raw[0].Time.Equal(want) {
		t.Errorf("time = %v, want %v", raw[0].Time, want)
	}
	if len(raw[0].Data) != 2 || raw[0].Data[0] != "sshd" {
		t.Errorf("data = %q", raw[0].Data)
	}

	if empty, err := ParseWevtutilXML([]byte("  \r\n")); err != nil || len(empty) != 0 {
		t.Errorf("empty input: got %v, %v", empty, err)
	}
	if bom, err := ParseWevtutilXML([]byte("\xef\xbb\xbf" + sampleXML)); err != nil || len(bom) != 2 {
		t.Errorf("input with a byte order mark: got %d events, %v", len(bom), err)
	}
	if _, err := ParseWevtutilXML([]byte("<Event><System>")); err == nil {
		t.Error("truncated xml must return an error")
	}
}

func TestToEvents(t *testing.T) {
	raw, err := ParseWevtutilXML([]byte(sampleXML))
	if err != nil {
		t.Fatal(err)
	}
	events := ToEvents(raw)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (the disconnect must be dropped)", len(events))
	}
	ev := events[0]
	if ev.RecordID != 1201 || ev.Username != "deploy" || ev.SourceIP != "203.0.113.42" || ev.Method != "publickey" || ev.Time.IsZero() {
		t.Errorf("event = %+v", ev)
	}
	if ToEvents(nil) != nil {
		t.Error("no raw events must give no events")
	}
}
