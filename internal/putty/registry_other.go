//go:build !windows

package putty

import "easytty/internal/store"

// LoadSessions is only supported on Windows.
func LoadSessions() ([]store.Session, int, error) { return nil, 0, nil }
