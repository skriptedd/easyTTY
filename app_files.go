package main

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// The file list ("Logs & Dateien", Alt+L): session logs, saved configs and
// snippet output in one place, newest first, so nothing has to be looked
// for in Explorer.

const (
	maxListedFiles = 3000
	maxFileDepth   = 4          // Logs\<Tag>\… or Configs\Ausgaben\…
	outputSubdir   = "Ausgaben" // snippet output, apart from the configs
)

// File kinds in the list.
const (
	kindLog    = "log"
	kindConfig = "config"
	kindOutput = "output"
)

// ListedFile is one row of the file list.
type ListedFile struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Folder string `json:"folder"` // relative to its root folder ("" = directly in it)
	Kind   string `json:"kind"`
	Size   int64  `json:"size"`
	Time   int64  `json:"time"` // modified, Unix ms
}

// FileList returns logs, configs and snippet output, newest first.
func (a *App) FileList() ([]ListedFile, error) {
	s := a.GetSettings()
	out := []ListedFile{}
	seen := map[string]bool{}
	add := func(f ListedFile) {
		key := strings.ToLower(filepath.Clean(f.Path))
		if !seen[key] {
			seen[key] = true
			out = append(out, f)
		}
	}
	// Each folder is walked for at most maxListedFiles files: a folder set to
	// C:\ or a whole profile must not keep the list from opening.
	walkFiles(s.ConfigDir, func(f ListedFile) {
		f.Kind = kindConfig
		if first, _, _ := strings.Cut(f.Folder, string(filepath.Separator)); strings.EqualFold(first, outputSubdir) {
			f.Kind = kindOutput
		}
		add(f)
	})
	walkFiles(s.LogDir, func(f ListedFile) { f.Kind = kindLog; add(f) })
	for _, p := range a.ownLogs() { // logs with their own path (session setting)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			add(ListedFile{Path: p, Name: fi.Name(), Folder: filepath.Dir(p), Kind: kindLog, Size: fi.Size(), Time: fi.ModTime().UnixMilli()})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time > out[j].Time })
	if len(out) > maxListedFiles {
		out = out[:maxListedFiles]
	}
	return out, nil
}

// walkFiles calls fn for every regular file below root (hidden ones and
// deep trees left out). A missing root is no error: nothing was saved yet.
func walkFiles(root string, fn func(ListedFile)) {
	if root == "" {
		return
	}
	found := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if found >= maxListedFiles {
			return fs.SkipAll
		}
		if err != nil {
			return nil // unreadable folder: skip it, list the rest
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if strings.Count(rel, string(filepath.Separator)) >= maxFileDepth-1 {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		folder := filepath.Dir(rel)
		if folder == "." {
			folder = ""
		}
		found++
		fn(ListedFile{Path: p, Name: d.Name(), Folder: folder, Size: info.Size(), Time: info.ModTime().UnixMilli()})
		return nil
	})
}

// OpenFile opens a listed file with the program Windows uses for it. Only
// files in the log or config folder or a log of this run, so the UI cannot
// be used to start anything else.
func (a *App) OpenFile(path string) error {
	if !a.listable(path) {
		return errors.New("Datei liegt nicht im Log- oder Config-Ordner")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return errors.New("keine Datei")
	}
	if !openable(path) {
		// Explorer would run it (a .cmd as log path, an .exe in a chosen folder).
		return errors.New("Nur Text-, Log- und Config-Dateien werden geöffnet – „Ordner“ zeigt sie im Explorer")
	}
	return exec.Command(explorerExe(), path).Start()
}

// openableExt are the file types OpenFile hands to Explorer: text only.
var openableExt = map[string]bool{".log": true, ".txt": true, ".cfg": true, ".conf": true, ".json": true, ".csv": true, ".diff": true}

func openable(path string) bool { return openableExt[strings.ToLower(filepath.Ext(path))] }

func (a *App) listable(path string) bool {
	s := a.GetSettings()
	if insideDir(s.LogDir, path) || insideDir(s.ConfigDir, path) {
		return true
	}
	for _, p := range a.ownLogs() {
		if samePath(p, path) {
			return true
		}
	}
	return false
}

// rememberLog notes a log file of this run (it may lie outside the log folder).
func (a *App) rememberLog(path string) {
	if path == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.logFiles == nil {
		a.logFiles = map[string]bool{}
	}
	a.logFiles[path] = true
}

func (a *App) ownLogs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.logFiles))
	for p := range a.logFiles {
		out = append(out, p)
	}
	return out
}
