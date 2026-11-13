package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeData(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBackupDataCopiesTheImportantFiles(t *testing.T) {
	dir := t.TempDir()
	writeData(t, dir, "sessions.json", "S")
	writeData(t, dir, "settings.json", "E")
	writeData(t, dir, "known_hosts", "K")
	writeData(t, dir, filepath.Join("automation", "tasks.json"), "T")
	writeData(t, dir, filepath.Join("automation", "runs", "big.json"), "R") // history: not needed
	at := time.Date(2026, 11, 13, 9, 30, 0, 0, time.Local)

	dest, err := backupData(dir, "0.2.19", at)

	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dest) != "0.2.19_2026-11-13_093000" {
		t.Errorf("folder %s", dest)
	}
	for rel, want := range map[string]string{"sessions.json": "S", "settings.json": "E", "known_hosts": "K", filepath.Join("automation", "tasks.json"): "T"} {
		if got, err := os.ReadFile(filepath.Join(dest, rel)); err != nil || string(got) != want {
			t.Errorf("%s = %q, %v", rel, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "automation", "runs")); !os.IsNotExist(err) {
		t.Error("run history was copied")
	}
}

func TestBackupDataWithoutDataDoesNothing(t *testing.T) {
	dir := t.TempDir()

	dest, err := backupData(dir, "0.2.19", time.Now())

	if err != nil || dest != "" {
		t.Errorf("dest %q, err %v", dest, err)
	}
	if _, err := os.Stat(filepath.Join(dir, backupDir)); err == nil {
		if entries, _ := os.ReadDir(filepath.Join(dir, backupDir)); len(entries) != 0 {
			t.Errorf("empty backup left: %v", entries)
		}
	}
}

func TestBackupDataKeepsTheNewestFive(t *testing.T) {
	dir := t.TempDir()
	writeData(t, dir, "sessions.json", "S")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	var paths []string
	for i := range 7 {
		p, err := backupData(dir, "0.2.19", base.Add(time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		// Folder times must differ even on fast file systems.
		mod := base.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	pruneBackups(filepath.Join(dir, backupDir), backupKeep)

	entries, _ := os.ReadDir(filepath.Join(dir, backupDir))
	if len(entries) != backupKeep {
		t.Fatalf("%d backups left, want %d", len(entries), backupKeep)
	}
	for _, old := range paths[:2] {
		if _, err := os.Stat(old); !os.IsNotExist(err) {
			t.Errorf("old backup %s kept", filepath.Base(old))
		}
	}
}

func TestBackupDataNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	writeData(t, dir, "sessions.json", "S")
	at := time.Date(2026, 11, 13, 9, 30, 0, 0, time.Local)
	if _, err := backupData(dir, "0.2.19", at); err != nil {
		t.Fatal(err)
	}

	_, err := backupData(dir, "0.2.19", at) // same second: the folder exists

	if err == nil {
		t.Error("existing backup overwritten")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, backupDir, "0.2.19_2026-11-13_093000", "sessions.json")); string(got) != "S" {
		t.Errorf("first backup damaged: %q", got)
	}
}
