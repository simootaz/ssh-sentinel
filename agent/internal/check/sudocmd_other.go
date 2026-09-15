//go:build !linux

package check

// SudoCommand is Linux only: there is no /proc to read on the other targets.
func SudoCommand() *string { return nil }
