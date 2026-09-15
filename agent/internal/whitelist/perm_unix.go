//go:build !windows

package whitelist

import (
	"os"
	"syscall"
)

// checkPrivate refuses a file that group or others can write, or that belongs to neither root
// nor the current user. Under pam_exec the agent runs as root, so the file must be root's.
func checkPrivate(info os.FileInfo) error {
	if info.Mode().Perm()&0o022 != 0 {
		return ErrInsecure
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		if st.Uid != 0 && int(st.Uid) != os.Geteuid() {
			return ErrInsecure
		}
	}
	return nil
}
