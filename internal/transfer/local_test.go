package transfer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestListLocal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "b.txt"), []byte("hello"))
	writeFile(t, filepath.Join(dir, "A.cfg"), []byte("x"))
	_ = os.Mkdir(filepath.Join(dir, "zz-dir"), 0o755)

	l, err := ListLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 3 || !l.Entries[0].IsDir || l.Entries[1].Name != "A.cfg" || l.Entries[2].Size != 5 {
		t.Fatalf("unexpected listing: %+v", l.Entries)
	}
	if l.Entries[2].Path != filepath.Join(dir, "b.txt") {
		t.Fatalf("path %q", l.Entries[2].Path)
	}
	missing := filepath.Join(dir, "missing")
	_, err = ListLocal(missing)
	if err == nil {
		t.Fatal("expected error for missing dir")
	}
	if strings.Count(err.Error(), missing) != 1 {
		t.Fatalf("path should appear once: %v", err)
	}
}

func TestLocalRootAndParent(t *testing.T) {
	root, err := ListLocal(LocalRoot)
	if err != nil || len(root.Entries) == 0 || !root.Entries[0].IsDir {
		t.Fatalf("drive list: %+v %v", root, err)
	}
	if LocalParent(LocalRoot) != LocalRoot {
		t.Fatal("root has no parent")
	}
	dir := t.TempDir()
	if LocalParent(filepath.Join(dir, "x")) != dir {
		t.Fatal("parent of child dir")
	}
	if runtime.GOOS == "windows" && LocalParent(`C:\`) != LocalRoot {
		t.Fatal(`parent of C:\ must be the drive list`)
	}
}
