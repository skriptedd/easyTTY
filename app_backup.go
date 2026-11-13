package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Before an update replaces the exe, the data of the running version is
// copied to <data>\backups\<version>_<date>. A newer version may convert the
// files; with the backup and easyTTY.old.exe the way back stays open.

const (
	backupDir  = "backups"
	backupKeep = 5
)

// backupFiles are the files worth keeping, relative to the data folder.
// Logs, run histories and the WebView cache can be rebuilt or live elsewhere.
var backupFiles = []string{
	"sessions.json",
	"settings.json",
	"known_hosts",
	filepath.Join("automation", "tasks.json"),
}

// backupData copies the data files of version into a new backup folder and
// keeps only the newest backupKeep folders. Missing files are skipped (a
// fresh installation has no tasks yet). Returns the backup folder.
func backupData(dataDir, version string, at time.Time) (string, error) {
	root := filepath.Join(dataDir, backupDir)
	dest := filepath.Join(root, fmt.Sprintf("%s_%s", safeName(version, "unbekannt"), at.Format("2006-01-02_150405")))
	var todo []string
	for _, rel := range backupFiles {
		if _, err := os.Stat(filepath.Join(dataDir, rel)); !errors.Is(err, os.ErrNotExist) {
			todo = append(todo, rel)
		}
	}
	if len(todo) == 0 {
		return "", nil // nothing to keep yet
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	// Mkdir, not MkdirAll: an existing backup of the same second stays untouched.
	if err := os.Mkdir(dest, 0o700); err != nil {
		return "", err
	}
	for _, rel := range todo {
		if err := copyFile(filepath.Join(dataDir, rel), filepath.Join(dest, rel)); err != nil {
			os.RemoveAll(dest) // half a backup is worse than none: it looks complete
			return "", fmt.Errorf("%s: %w", rel, err)
		}
	}
	pruneBackups(root, backupKeep)
	return dest, nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// pruneBackups deletes all but the newest keep backup folders. The names
// sort by date only within one version, so the folder time decides.
func pruneBackups(root string, keep int) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	type dir struct {
		path string
		mod  time.Time
	}
	var dirs []dir
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if info, err := e.Info(); err == nil {
			dirs = append(dirs, dir{filepath.Join(root, e.Name()), info.ModTime()})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].mod.After(dirs[j].mod) })
	for i := keep; i < len(dirs); i++ {
		os.RemoveAll(dirs[i].path)
	}
}
