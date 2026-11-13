package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// A crash of the Go runtime (a panic, or an exception inside Windows code such
// as the RDP control) goes to stderr, ends the process with code 2 and leaves
// no Windows error report. A GUI app has no stderr, so without this file a
// crash would just be a vanished window.

const (
	crashLogName = "crash.log"
	maxCrashLog  = 1 << 20
)

// crashFile stays referenced: its finalizer would close the handle.
var crashFile *os.File

// dataDir returns the folder of all app data and whether it is the portable one.
func dataDir() (dir string, portable bool, err error) {
	if dir, ok := portableDir(); ok {
		return dir, true, nil // everything stays next to the exe
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", false, err
	}
	return filepath.Join(cfg, appName), false, nil
}

// startCrashLog sends crash output to crash.log. Failing here must never stop
// the app, it only means a crash stays unexplained.
func startCrashLog() {
	dir, _, err := dataDir()
	if err != nil {
		return
	}
	f, err := openCrashLog(filepath.Join(dir, crashLogName))
	if err != nil {
		return
	}
	if redirectStderr(f) != nil {
		_ = f.Close()
		return
	}
	crashFile = f
	debug.SetTraceback("all") // the RDP thread is rarely the one that panics first
}

// openCrashLog opens the log for appending and notes the start. A log that
// grew too large moves to crash.log.old.
func openCrashLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxCrashLog {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(f, "=== easyTTY %s gestartet %s\n", appVersion, time.Now().Format(time.RFC3339))
	return f, nil
}
