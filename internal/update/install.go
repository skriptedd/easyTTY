package update

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Installing a new exe: the running program cannot overwrite itself, so it
// downloads the new version next to itself and quits; the new version then
// runs once as a helper (see FinishUpdate in the app) and writes itself into
// the old file. The file easyTTY.exe is never renamed or deleted: a taskbar
// pin or shortcut that points at it keeps working (Windows drops pins whose
// target is renamed away).

const unlockPoll = 200 * time.Millisecond

// unlockWait is how long the helper waits for the old process to quit.
var unlockWait = 60 * time.Second

// OldName is where the previous version is kept for going back.
func OldName(exe string) string {
	return strings.TrimSuffix(exe, ".exe") + ".old.exe"
}

// NewName is where the downloaded version waits until the old one has quit.
func NewName(exe string) string {
	return strings.TrimSuffix(exe, ".exe") + ".new.exe"
}

// OverwriteExe copies the previous exe to OldName(exe), then writes src into
// exe in place. It waits until exe can be opened for writing (the old process
// has quit). If writing fails, the previous content is put back.
func OverwriteExe(exe, src string) error {
	deadline := time.Now().Add(unlockWait)
	var target *os.File
	for {
		f, err := os.OpenFile(exe, os.O_RDWR, 0)
		if err == nil {
			target = f
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("easyTTY.exe ist noch in Benutzung: %w", err)
		}
		time.Sleep(unlockPoll)
	}
	defer target.Close()
	if err := copyInto(OldName(exe), target); err != nil {
		return fmt.Errorf("alte Version nicht gesichert, nichts geändert: %w", err)
	}
	if err := writeFrom(target, src); err != nil {
		if back := writeFrom(target, OldName(exe)); back != nil {
			return fmt.Errorf("Update fehlgeschlagen (%v), die vorige Version liegt unter %s", err, OldName(exe))
		}
		return fmt.Errorf("Update fehlgeschlagen, vorige Version bleibt: %w", err)
	}
	return nil
}

// copyInto writes the content of src (read from the start) to a new file dst.
func copyInto(dst string, src *os.File) error {
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, src)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// writeFrom replaces the content of the open file f with the file at src.
func writeFrom(f *os.File, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.Copy(f, in); err != nil {
		return err
	}
	return f.Sync()
}
