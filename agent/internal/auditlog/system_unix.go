//go:build !windows

package auditlog

import (
	"log/syslog"
	"os"
)

type syslogLogger struct{ w *syslog.Writer }

// NewSystem returns the syslog logger (facility auth, tag ssh-sentinel). syslog adds the
// "ssh-sentinel[pid]:" prefix itself. If syslog is unavailable, lines go to stderr.
func NewSystem() Logger {
	w, err := syslog.New(syslog.LOG_AUTH|syslog.LOG_INFO, "ssh-sentinel")
	if err != nil {
		return WriterLogger{W: os.Stderr}
	}
	return syslogLogger{w: w}
}

func (s syslogLogger) Log(l Line) {
	if l.Decision == Deny {
		_ = s.w.Warning(l.String())
		return
	}
	_ = s.w.Info(l.String())
}
