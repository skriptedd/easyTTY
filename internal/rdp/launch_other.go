//go:build !windows

package rdp

import (
	"errors"

	"easytty/internal/store"
)

// Launch: Remote Desktop via mstsc only exists on Windows.
func Launch(store.Session) error {
	return errors.New("Remotedesktop gibt es nur unter Windows")
}
