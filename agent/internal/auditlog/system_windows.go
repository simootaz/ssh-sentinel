//go:build windows

package auditlog

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	advapi32                  = syscall.NewLazyDLL("advapi32.dll")
	procRegisterEventSourceW  = advapi32.NewProc("RegisterEventSourceW")
	procReportEventW          = advapi32.NewProc("ReportEventW")
	procDeregisterEventSource = advapi32.NewProc("DeregisterEventSource")
)

const (
	eventlogWarning     = 0x0002
	eventlogInformation = 0x0004
	// eventID is arbitrary but fixed, so operators can filter on it.
	eventID = 100
)

type eventLogger struct{ h uintptr }

// NewSystem returns a logger writing to the Application event log under the source
// "ssh-sentinel", through advapi32 directly so the binary keeps zero dependencies. If the
// source cannot be opened, lines go to stderr.
func NewSystem() Logger {
	name, err := syscall.UTF16PtrFromString("ssh-sentinel")
	if err != nil {
		return WriterLogger{W: os.Stderr}
	}
	h, _, _ := procRegisterEventSourceW.Call(0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return WriterLogger{W: os.Stderr}
	}
	return &eventLogger{h: h}
}

func (e *eventLogger) Log(l Line) {
	msg, err := syscall.UTF16PtrFromString(l.String())
	if err != nil {
		return
	}
	typ := uintptr(eventlogInformation)
	if l.Decision == Deny {
		typ = eventlogWarning
	}
	strs := []*uint16{msg}
	procReportEventW.Call(e.h, typ, 0, eventID, 0, 1, 0, uintptr(unsafe.Pointer(&strs[0])), 0)
}

// Close releases the event source handle.
func (e *eventLogger) Close() {
	procDeregisterEventSource.Call(e.h)
}
