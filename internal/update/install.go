package update

import (
	"fmt"
	"os"
	"strings"
)

// OldName is where the running exe is parked while the new one takes its
// place (Windows lets a running program be renamed, not overwritten).
func OldName(exe string) string {
	return strings.TrimSuffix(exe, ".exe") + ".old.exe"
}

// Swap puts newFile in place of exe and keeps the previous exe as OldName.
// If the second step fails, the previous exe is moved back.
func Swap(exe, newFile string) error {
	old := OldName(exe)
	if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("alte Version lässt sich nicht entfernen: %w", err)
	}
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("easyTTY.exe lässt sich nicht ersetzen: %w", err)
	}
	if err := os.Rename(newFile, exe); err != nil {
		if back := os.Rename(old, exe); back != nil {
			return fmt.Errorf("Update fehlgeschlagen (%v), alte Version liegt unter %s", err, old)
		}
		return fmt.Errorf("neue Version lässt sich nicht einsetzen: %w", err)
	}
	return nil
}
