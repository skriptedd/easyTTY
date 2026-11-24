package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeAt(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func TestFileListFindsLogsConfigsAndOutput(t *testing.T) {
	data := t.TempDir()
	a := &App{dir: data, portable: true, settingsPath: filepath.Join(data, "settings.json")}
	now := time.Now()
	logFile := filepath.Join(data, "Logs", "2026-11-24", "sw1_101500.log")
	cfg := filepath.Join(data, "Configs", "sw1_2026-11-24_101600.cfg")
	out := filepath.Join(data, "Configs", outputSubdir, "sw1_clock_2026-11-24_101700.txt")
	writeAt(t, logFile, now.Add(-3*time.Minute))
	writeAt(t, cfg, now.Add(-2*time.Minute))
	writeAt(t, out, now.Add(-time.Minute))
	writeAt(t, filepath.Join(data, "Logs", ".hidden"), now)
	own := filepath.Join(t.TempDir(), "eigen.log") // a session with its own log path
	writeAt(t, own, now.Add(-4*time.Minute))
	a.rememberLog(own)

	files, err := a.FileList()
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ path, kind string }{{out, kindOutput}, {cfg, kindConfig}, {logFile, kindLog}, {own, kindLog}}
	if len(files) != len(want) {
		t.Fatalf("got %d files: %+v", len(files), files)
	}
	for i, w := range want {
		if files[i].Path != w.path || files[i].Kind != w.kind {
			t.Errorf("file %d = %s (%s), want %s (%s)", i, files[i].Path, files[i].Kind, w.path, w.kind)
		}
	}
	if files[2].Folder != "2026-11-24" {
		t.Errorf("log folder = %q", files[2].Folder)
	}
	for _, p := range []string{logFile, cfg, out, own} {
		if !a.listable(p) {
			t.Errorf("%s must be openable", p)
		}
	}
	if a.listable(filepath.Join(t.TempDir(), "evil.exe")) {
		t.Error("a file outside the folders must not be openable")
	}
	// Inside the folders, only text is handed to Explorer (it would run a .cmd).
	cmd := filepath.Join(data, "Logs", "x.cmd")
	writeAt(t, cmd, now)
	if err := a.OpenFile(cmd); err == nil {
		t.Error("a .cmd in the log folder must not be opened")
	}
	for _, p := range []string{"a.log", "b.TXT", "c.cfg"} {
		if !openable(p) {
			t.Errorf("%s must be openable", p)
		}
	}
}
