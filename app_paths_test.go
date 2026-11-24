package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreDirKeepsFoldersMovable(t *testing.T) {
	data := filepath.Join(t.TempDir(), "easyTTY-data")
	def := filepath.Join(data, "Logs")
	other := filepath.Join(t.TempDir(), "Elsewhere")
	cases := []struct {
		name     string
		portable bool
		dir      string
		want     string
	}{
		{"default is empty", true, def, ""},
		{"default other case", true, filepath.Join(data, "LOGS"), ""},
		{"empty stays empty", true, "  ", ""},
		{"inside data is relative", true, filepath.Join(data, "Logs", "Nord"), filepath.Join("Logs", "Nord")},
		{"outside stays absolute", true, other, other},
		{"installed keeps absolute", false, filepath.Join(data, "Mine"), filepath.Join(data, "Mine")},
	}
	for _, c := range cases {
		if got := storeDir(data, c.portable, c.dir, def); got != c.want {
			t.Errorf("%s: storeDir(%q) = %q, want %q", c.name, c.dir, got, c.want)
		}
	}
}

func TestResolveDirFollowsMovedApp(t *testing.T) {
	data := filepath.Join(t.TempDir(), "new", "easyTTY-data")
	def := filepath.Join(data, "Logs")
	old := filepath.Join(t.TempDir(), "old", "easyTTY-data", "Logs")
	other := filepath.Join(t.TempDir(), "Elsewhere")
	cases := []struct {
		name     string
		portable bool
		stored   string
		want     string
	}{
		{"empty is default", true, "", def},
		{"relative joins data", true, filepath.Join("Logs", "Nord"), filepath.Join(data, "Logs", "Nord")},
		{"old absolute data path is rebased", true, old, def},
		{"old data folder itself", true, filepath.Dir(old), data},
		{"own data path stays", true, filepath.Join(data, "X"), filepath.Join(data, "X")},
		{"unrelated folder stays", true, other, other},
		{"installed never rebases", false, old, old},
	}
	for _, c := range cases {
		if got := resolveDir(data, c.portable, c.stored, def); got != c.want {
			t.Errorf("%s: resolveDir(%q) = %q, want %q", c.name, c.stored, got, c.want)
		}
	}
}

func TestResolveDirKeepsExistingForeignDataFolder(t *testing.T) {
	data := filepath.Join(t.TempDir(), "easyTTY-data")
	chosen := filepath.Join(t.TempDir(), "Backup", "easyTTY-data", "Logs")
	if err := os.MkdirAll(chosen, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := resolveDir(data, true, chosen, filepath.Join(data, "Logs")); got != chosen {
		t.Fatalf("an existing folder chosen on purpose was rebased to %q", got)
	}
}

func TestDirsRoundTrip(t *testing.T) {
	data := filepath.Join(t.TempDir(), "easyTTY-data")
	def := filepath.Join(data, "Logs")
	for _, dir := range []string{def, filepath.Join(data, "A", "B"), filepath.Join(t.TempDir(), "C")} {
		if got := resolveDir(data, true, storeDir(data, true, dir, def), def); got != dir {
			t.Errorf("round trip of %q gave %q", dir, got)
		}
	}
}
