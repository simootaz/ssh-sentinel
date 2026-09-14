//go:build windows

package whitelist

import "os"

// checkPrivate is a no-op on Windows: the break-glass file is not used there (watch is
// notify-only) and NTFS permissions are managed by the installer.
func checkPrivate(os.FileInfo) error { return nil }
