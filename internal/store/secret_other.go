//go:build !windows

package store

// DefaultProtector returns nil on non-Windows platforms: passwords are not stored.
func DefaultProtector() Protector { return nil }
