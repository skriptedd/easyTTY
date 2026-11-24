package main

import (
	"os"
	"path/filepath"
	"strings"

	"easytty/internal/store"
)

// Folders from the settings (logs, configs) are kept so they move with the
// app: the default is stored as "", and in portable mode a folder inside the
// data folder is stored relative to it. A portable copy that was moved or
// copied still finds paths an older version stored absolutely (see
// resolveDir), instead of creating the old folders again.

// storeDir turns a folder from the UI into what settings.json keeps.
func storeDir(dataDir string, portable bool, dir, def string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" || samePath(dir, def) {
		return ""
	}
	if portable && filepath.IsAbs(dir) && insideDir(dataDir, dir) {
		if rel, err := filepath.Rel(dataDir, dir); err == nil {
			return rel
		}
	}
	return dir
}

// resolveDir turns a stored folder back into an absolute path.
func resolveDir(dataDir string, portable bool, stored, def string) string {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return def
	}
	if !filepath.IsAbs(stored) {
		return filepath.Join(dataDir, stored)
	}
	// Only a folder that is gone: one chosen on purpose that still exists stays.
	if portable && !samePath(stored, dataDir) && !insideDir(dataDir, stored) && !dirExists(stored) {
		if rest, ok := underDataDir(stored); ok {
			return filepath.Join(append([]string{dataDir}, rest...)...)
		}
	}
	return stored
}

// underDataDir returns the part of path below a portable data folder
// ("…\easyTTY-data\Logs" → ["Logs"]); such a path belongs to a copy of the
// app that is somewhere else now.
func underDataDir(path string) ([]string, bool) {
	parts := strings.FieldsFunc(filepath.Clean(path), func(r rune) bool { return r == '\\' || r == '/' })
	for i := len(parts) - 1; i >= 0; i-- {
		if strings.EqualFold(parts[i], portableDataDir) {
			return parts[i+1:], true
		}
	}
	return nil, false
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// withDirs fills in the folders of settings read from disk.
func (a *App) withDirs(s store.Settings) store.Settings {
	s.LogDir = resolveDir(a.dir, a.portable, s.LogDir, a.defaultLogDir())
	s.ConfigDir = resolveDir(a.dir, a.portable, s.ConfigDir, a.defaultConfigDir())
	return s
}

// storableDirs is the reverse of withDirs, used before writing settings.
func (a *App) storableDirs(s store.Settings) store.Settings {
	s.LogDir = storeDir(a.dir, a.portable, s.LogDir, a.defaultLogDir())
	s.ConfigDir = storeDir(a.dir, a.portable, s.ConfigDir, a.defaultConfigDir())
	return s
}
