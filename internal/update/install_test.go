package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOverwriteExeKeepsTheFileAndSavesTheOldVersion(t *testing.T) {
	dir := t.TempDir()
	exe, fresh := filepath.Join(dir, "easyTTY.exe"), filepath.Join(dir, "easyTTY.new.exe")
	write(t, exe, "old version, longer than the new one")
	write(t, fresh, "new")
	write(t, OldName(exe), "leftover of an earlier update")
	before, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}

	err = OverwriteExe(exe, fresh)

	if err != nil {
		t.Fatal(err)
	}
	if read(t, exe) != "new" || read(t, OldName(exe)) != "old version, longer than the new one" {
		t.Errorf("exe %q, old %q", read(t, exe), read(t, OldName(exe)))
	}
	after, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	// Same file, not a new one under the old name: shortcuts and pins stay.
	if !os.SameFile(before, after) {
		t.Error("exe was replaced by another file instead of being overwritten")
	}
	if _, err := os.Stat(OldName(exe) + ".tmp"); !os.IsNotExist(err) {
		t.Error("temporary copy left behind")
	}
}

func TestOverwriteExeLeavesExeAloneWhenNewFileIsMissing(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "easyTTY.exe")
	write(t, exe, "old")

	err := OverwriteExe(exe, filepath.Join(dir, "missing.exe"))

	if err == nil {
		t.Fatal("update without new file succeeded")
	}
	if read(t, exe) != "old" {
		t.Errorf("exe not restored: %q", read(t, exe))
	}
}

func TestOverwriteExeFailsWithoutTarget(t *testing.T) {
	dir := t.TempDir()
	fresh := filepath.Join(dir, "easyTTY.new.exe")
	write(t, fresh, "new")
	old := unlockWait
	unlockWait = 0
	defer func() { unlockWait = old }()

	err := OverwriteExe(filepath.Join(dir, "easyTTY.exe"), fresh)

	if err == nil {
		t.Fatal("update of a missing exe succeeded")
	}
}

func TestNames(t *testing.T) {
	exe := filepath.Join("C:", "Programs", "easyTTY", "easyTTY.exe")
	if got := filepath.Base(NewName(exe)); got != "easyTTY.new.exe" {
		t.Errorf("NewName = %s", got)
	}
	if got := filepath.Base(OldName(exe)); got != "easyTTY.old.exe" {
		t.Errorf("OldName = %s", got)
	}
}
