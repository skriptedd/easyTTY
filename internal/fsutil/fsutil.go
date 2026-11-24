// Package fsutil holds the small file and id helpers every service needs, so
// services do not have to depend on each other for them.
package fsutil

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// WriteAtomic writes data to path via a temp file, keeping one .bak copy.
func WriteAtomic(path string, data []byte) error { return write(path, data, true) }

// Restore puts data (a good backup) back in place of a damaged file. The
// damaged content must not become the new .bak, so no backup is made.
func Restore(path string, data []byte) error { return write(path, data, false) }

func write(path string, data []byte, backup bool) error {
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
	if old, err := os.ReadFile(path); backup && err == nil && len(old) > 0 {
		writeBackup(path+".bak", old) // best effort
	}
	return renameRetry(tmp, path)
}

// How long a rename or read waits for someone else to let go of the file.
// "Access denied" is also what a real lack of rights looks like, so it is
// given less time than a sharing violation, which is always temporary.
const (
	retryFirst      = 5 * time.Millisecond
	retryMaxWait    = 200 * time.Millisecond
	retrySharing    = 2 * time.Second
	retryPermission = 500 * time.Millisecond
)

// renameRetry renames, retrying while Windows refuses because someone has
// the target open (another read of the file, a virus scanner, a sync tool).
// Replacing our own file, "access denied" means exactly that, so it gets as
// long as a sharing violation.
func renameRetry(from, to string) error {
	return retry(retrySharing, func() error { return os.Rename(from, to) })
}

// ReadFile reads a file, retrying while it is briefly locked (see renameRetry).
func ReadFile(path string) ([]byte, error) {
	var b []byte
	err := retry(retryPermission, func() (err error) {
		b, err = os.ReadFile(path)
		return err
	})
	return b, err
}

// retry runs op until it works, fails for good, or the time is up:
// retrySharing for a sharing violation, denied for "access denied".
func retry(denied time.Duration, op func() error) error {
	start := time.Now()
	wait := retryFirst
	for {
		err := op()
		if err == nil || !Transient(err) {
			return err
		}
		limit := retrySharing
		if !errors.Is(err, errSharingViolation) {
			limit = denied
		}
		if time.Since(start) > limit {
			return err
		}
		time.Sleep(wait)
		wait = min(wait*2, retryMaxWait)
	}
}

// errSharingViolation is ERROR_SHARING_VIOLATION on Windows.
const errSharingViolation = syscall.Errno(32)

// Transient reports a Windows "file is in use" error that goes away by itself.
func Transient(err error) bool {
	return errors.Is(err, fs.ErrPermission) || errors.Is(err, errSharingViolation)
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
