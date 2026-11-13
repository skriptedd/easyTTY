package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"easytty/internal/textdiff"
)

const (
	maxConfigFiles    = 500
	maxDiffFileBytes  = 5 << 20
	configDateSuffixN = len("_2006-01-02_150405")
)

// ConfigFile is one saved config or command output in the config folder.
type ConfigFile struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"` // unix ms
	// Device is the name without date and extension ("sw1" for
	// "sw1_2026-11-11_182548.cfg"): files of one device compare well.
	Device string `json:"device"`
}

// ConfigDiff is the comparison of two files.
type ConfigDiff struct {
	Lines   []textdiff.Line `json:"lines"`
	Removed int             `json:"removed"`
	Added   int             `json:"added"`
}

// volatileLine matches lines that change with every "show running-config"
// without a real change: byte count, timestamps, NTP drift.
var volatileLine = regexp.MustCompile(`^(Building configuration|Current configuration ?:|! Last configuration change|! NVRAM config last updated|! No configuration change since|ntp clock-period |! easyTTY – )`)

// ConfigFiles lists the files of the config folder, newest first.
func (a *App) ConfigFiles() ([]ConfigFile, error) {
	dir, err := a.ConfigDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := []ConfigFile{}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, ConfigFile{Name: e.Name(), Path: filepath.Join(dir, e.Name()), Size: info.Size(),
			ModTime: info.ModTime().UnixMilli(), Device: configDevice(e.Name())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime > out[j].ModTime })
	if len(out) > maxConfigFiles {
		out = out[:maxConfigFiles]
	}
	return out, nil
}

// configDevice cuts "_<date>_<time>", a " (n)" counter and the extension off a file name.
func configDevice(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if i := strings.LastIndex(base, " ("); i > 0 && strings.HasSuffix(base, ")") {
		base = base[:i]
	}
	if n := len(base) - configDateSuffixN; n > 0 && dateSuffix.MatchString(base[n:]) {
		return base[:n]
	}
	return base
}

var dateSuffix = regexp.MustCompile(`^_\d{4}-\d{2}-\d{2}_\d{6}$`)

// DiffConfigs compares two files of the config folder line by line.
// ignoreVolatile leaves out lines that differ in every backup (timestamps …).
func (a *App) DiffConfigs(oldPath, newPath string, ignoreVolatile bool) (ConfigDiff, error) {
	dir, err := a.ConfigDir()
	if err != nil {
		return ConfigDiff{}, err
	}
	oldLines, err := readConfigLines(dir, oldPath, ignoreVolatile)
	if err != nil {
		return ConfigDiff{}, err
	}
	newLines, err := readConfigLines(dir, newPath, ignoreVolatile)
	if err != nil {
		return ConfigDiff{}, err
	}
	lines := textdiff.Diff(oldLines, newLines)
	removed, added := textdiff.Changed(lines)
	return ConfigDiff{Lines: lines, Removed: removed, Added: added}, nil
}

func readConfigLines(dir, path string, ignoreVolatile bool) ([]string, error) {
	if !insideDir(dir, path) {
		return nil, errors.New("Datei liegt nicht im Config-Ordner")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxDiffFileBytes {
		return nil, fmt.Errorf("%s ist zu groß zum Vergleichen (über %d MB)", filepath.Base(path), maxDiffFileBytes>>20)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := textdiff.Lines(string(raw))
	if !ignoreVolatile {
		return lines, nil
	}
	kept := lines[:0:0]
	for _, l := range lines {
		if !volatileLine.MatchString(l) {
			kept = append(kept, l)
		}
	}
	return kept, nil
}
