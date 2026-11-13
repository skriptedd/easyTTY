package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"easytty/internal/update"
)

// An update ends in two steps: the old easyTTY downloads easyTTY.new.exe,
// quits and starts it as "easyTTY.new.exe --finish-update <easyTTY.exe>".
// That helper waits until the old process is gone, writes itself into
// easyTTY.exe (same file, so taskbar pins and shortcuts stay) and starts it.

const finishUpdateFlag = "--finish-update"

const (
	leftoverTries = 25
	leftoverPause = 200 * time.Millisecond
)

// finishUpdateTarget returns the exe to update when easyTTY runs as helper.
func finishUpdateTarget() (string, bool) {
	if len(os.Args) != 3 || os.Args[1] != finishUpdateFlag {
		return "", false
	}
	return os.Args[2], true
}

// runFinishUpdate writes the running helper into target and starts target.
// Whatever happens, target is started afterwards: either the new or the
// unchanged old version.
func runFinishUpdate(target string) int {
	self, err := os.Executable()
	if err == nil {
		err = checkUpdatePair(self, target)
	}
	if err != nil {
		logUpdateError(err)
		return 1 // never touch or start a file the update did not name
	}
	failed := update.OverwriteExe(target, self)
	if failed != nil {
		logUpdateError(failed) // the old version is still in place
	}
	cmd := exec.Command(target)
	cmd.Dir = filepath.Dir(target)
	if err := cmd.Start(); err != nil {
		logUpdateError(err)
		return 1
	}
	_ = cmd.Process.Release()
	if failed != nil {
		return 1
	}
	return 0
}

// checkUpdatePair only accepts the pair the update itself creates: the
// helper must be NewName(target) right next to target.
func checkUpdatePair(self, target string) error {
	if !filepath.IsAbs(target) || !strings.EqualFold(filepath.Ext(target), ".exe") {
		return fmt.Errorf("Update-Ziel ungültig: %q", target)
	}
	if !samePath(self, update.NewName(target)) {
		return fmt.Errorf("Update-Helfer %q gehört nicht zu %q", self, target)
	}
	return nil
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// removeUpdateLeftovers deletes easyTTY.new.exe after a finished update. The
// helper may still be exiting, so it tries for a few seconds in the background.
func removeUpdateLeftovers() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	fresh := update.NewName(exe)
	if _, err := os.Stat(fresh); err != nil {
		return
	}
	go func() {
		for i := 0; i < leftoverTries; i++ {
			if err := os.Remove(fresh); err == nil || errors.Is(err, os.ErrNotExist) {
				return
			}
			time.Sleep(leftoverPause)
		}
	}()
}

// logUpdateError notes a failed update in the crash log of the data folder,
// the helper has no window to show it in.
func logUpdateError(err error) {
	dir, _, derr := dataDir()
	if derr != nil {
		return
	}
	f, ferr := os.OpenFile(filepath.Join(dir, crashLogName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if ferr != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s update: %v\n", time.Now().Format(time.RFC3339), err)
}
