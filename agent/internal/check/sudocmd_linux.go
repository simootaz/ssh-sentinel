//go:build linux

package check

import (
	"fmt"
	"os"
	"strings"
)

// maxCommandLen keeps a huge command line from bloating the request; the app only shows the
// start of it anyway.
const maxCommandLen = 4096

// SudoCommand returns the command line of the parent process. Under pam_exec the parent is
// sudo itself, so this is the "sudo ..." invocation being approved. Best effort: nil whenever
// it cannot be read.
func SudoCommand() *string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", os.Getppid()))
	if err != nil {
		return nil
	}
	parts := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
	cmd := strings.TrimSpace(strings.Join(parts, " "))
	if cmd == "" {
		return nil
	}
	if len(cmd) > maxCommandLen {
		cmd = cmd[:maxCommandLen]
	}
	return &cmd
}
