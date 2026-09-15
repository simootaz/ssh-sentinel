package auditlog

import (
	"bytes"
	"testing"
)

func TestLineStringMatchesDocumentedFormat(t *testing.T) {
	l := Line{Decision: Deny, Context: "ssh", User: "deploy", RHost: "203.0.113.42", Host: "web-01", Reason: "admin", Request: "5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11"}
	want := "decision=deny context=ssh user=deploy rhost=203.0.113.42 host=web-01 reason=admin request=5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11"
	if got := l.String(); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestLineEmptyFieldsBecomeDash(t *testing.T) {
	l := Line{Decision: Allow, Context: "sudo", User: "deploy", Host: "web-01", Reason: "breakglass"}
	want := "decision=allow context=sudo user=deploy rhost=- host=web-01 reason=breakglass request=-"
	if got := l.String(); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestLineKeepsOneTokenPerValue(t *testing.T) {
	l := Line{Decision: Deny, Context: "ssh", User: "bad user\n", RHost: "1.2.3.4", Host: "h", Reason: "error", Request: "a\tb"}
	want := "decision=deny context=ssh user=bad_user_ rhost=1.2.3.4 host=h reason=error request=a_b"
	if got := l.String(); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestWriterLoggerAndRecorder(t *testing.T) {
	var buf bytes.Buffer
	WriterLogger{W: &buf}.Log(Line{Decision: Allow, Context: "ssh", User: "u", Host: "h", Reason: "cache"})
	if got := buf.String(); got != "decision=allow context=ssh user=u rhost=- host=h reason=cache request=-\n" {
		t.Errorf("unexpected output %q", got)
	}

	var r Recorder
	if r.Last() != (Line{}) {
		t.Error("empty recorder should return a zero line")
	}
	r.Log(Line{Decision: Deny})
	r.Log(Line{Decision: Allow})
	if len(r.Lines) != 2 || r.Last().Decision != Allow {
		t.Errorf("recorder kept %+v", r.Lines)
	}
}
