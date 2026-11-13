// Package fsutil holds the small file and id helpers every service needs, so
// services do not have to depend on each other for them.
package fsutil

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
)

// WriteAtomic writes data to path via a temp file, keeping one .bak copy.
func WriteAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// A unique temp name: two writers at once must not share one file.
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // gone after the rename; cleans up after an error
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if old, err := os.ReadFile(path); err == nil && len(old) > 0 {
		writeBackup(path+".bak", old) // best effort
	}
	return os.Rename(tmp, path)
}

// writeBackup replaces bak in one step, so a crash in the middle leaves the
// previous backup instead of half a file.
func writeBackup(bak string, data []byte) {
	f, err := os.CreateTemp(filepath.Dir(bak), filepath.Base(bak)+".*.tmp")
	if err != nil {
		return
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, werr := f.Write(data)
	if cerr := f.Close(); werr != nil || cerr != nil {
		return
	}
	_ = os.Rename(tmp, bak)
}

// NewID returns a random 16-hex id.
func NewID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return hex.EncodeToString(b)
}
