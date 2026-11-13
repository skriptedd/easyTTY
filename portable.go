package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// Portable mode: a marker file next to the executable keeps every byte of
// state (sessions, settings, known_hosts, logs, WebView cache) in a folder
// beside it – nothing in the user profile, no installer, no admin rights.
const (
	portableMarker  = "easyTTY.portable"
	portableDataDir = "easyTTY-data"
)

// portableDir returns the data folder when running portable.
func portableDir() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(dir, portableMarker)); err != nil {
		return "", false
	}
	return filepath.Join(dir, portableDataDir), true
}

// webviewDir is where the WebView keeps its cache in portable mode. Next to
// the data as usual, but not inside a OneDrive folder: OneDrive refuses the
// open cache databases (cache.db, declarative_performance_observer.db) with
// "item name or type not allowed". The cache can always be rebuilt, so it
// then goes to %LOCALAPPDATA%; sessions and settings stay in the folder.
func webviewDir(dataDir string) string {
	if !insideOneDrive(dataDir) {
		return filepath.Join(dataDir, "webview")
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return filepath.Join(dataDir, "webview")
	}
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(dataDir))))
	return filepath.Join(base, "easyTTY", "portable-webview", hex.EncodeToString(sum[:6]))
}

// insideOneDrive reports whether path lies in a folder OneDrive syncs.
func insideOneDrive(path string) bool {
	p := strings.ToLower(filepath.Clean(path)) + string(filepath.Separator)
	for _, env := range []string{"OneDrive", "OneDriveCommercial", "OneDriveConsumer"} {
		root := os.Getenv(env)
		if root == "" {
			continue
		}
		if strings.HasPrefix(p, strings.ToLower(filepath.Clean(root))+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
