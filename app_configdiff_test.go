package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDevice(t *testing.T) {
	for name, want := range map[string]string{
		"sw1_2026-11-11_182548.cfg":         "sw1",
		"Test_Switch_2026-11-11_182548.cfg": "Test_Switch",
		"sw1_Check_2026-11-11_183346.txt":   "sw1_Check",
		"sw1_2026-11-11_182548 (1).cfg":     "sw1",
		"sw1-confg":                         "sw1-confg",
		"notes.txt":                         "notes",
		"_2026-11-11_182548.cfg":            "_2026-11-11_182548",
	} {
		if got := configDevice(name); got != want {
			t.Errorf("configDevice(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestReadConfigLinesIgnoresVolatileLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sw1.cfg")
	text := "Building configuration...\n\nCurrent configuration : 4242 bytes\n!\n! Last configuration change at 18:00:00 MEZ\nhostname sw1\nntp clock-period 36028797\nend\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	kept, err := readConfigLines(dir, path, true)
	all, err2 := readConfigLines(dir, path, false)

	if err != nil || err2 != nil {
		t.Fatal(err, err2)
	}
	if len(kept) != 4 || kept[2] != "hostname sw1" || len(all) != 8 {
		t.Errorf("kept %q, all %d lines", kept, len(all))
	}
}

func TestReadConfigLinesStaysInConfigFolder(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := readConfigLines(dir, outside, false); err == nil {
		t.Error("file outside the config folder was read")
	}
	if _, err := readConfigLines(dir, filepath.Join(dir, "..", filepath.Base(filepath.Dir(outside)), "secret.txt"), false); err == nil {
		t.Error("path with .. was read")
	}
}
