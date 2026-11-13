//go:build windows

package rdp

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"easytty/internal/store"
)

// Launch starts mstsc for the session with a temporary .rdp file. No password
// is handed over: mstsc asks itself (writing credentials into the Windows
// credential store looked like a credential stealer to virus scanners).
func Launch(s store.Session) error {
	dir := filepath.Join(os.TempDir(), "easyTTY-rdp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	rnd := make([]byte, 8)
	_, _ = rand.Read(rnd)
	file := filepath.Join(dir, hex.EncodeToString(rnd)+".rdp")
	if err := os.WriteFile(file, []byte(File(s)), 0o600); err != nil {
		return err
	}
	cmd := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "mstsc.exe"), file)
	if err := cmd.Start(); err != nil {
		os.Remove(file)
		return fmt.Errorf("Remotedesktop (mstsc) starten: %w", err)
	}
	go func() {
		_ = cmd.Wait()
		os.Remove(file)
	}()
	return nil
}
