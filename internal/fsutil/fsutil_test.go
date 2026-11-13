package fsutil

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestWriteAtomicKeepsABackupAndNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "a.json")
	for _, content := range []string{"one", "two"} {
		if err := WriteAtomic(p, []byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := os.ReadFile(p); string(got) != "two" {
		t.Fatalf("file: %q", got)
	}
	if got, _ := os.ReadFile(p + ".bak"); string(got) != "one" {
		t.Fatalf("backup: %q", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 2 {
		t.Fatalf("left over: %v", entries)
	}
}

func TestWriteAtomicWithManyWriters(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.json")
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = WriteAtomic(p, []byte("same"))
		}()
	}
	wg.Wait()
	if got, err := os.ReadFile(p); err != nil || string(got) != "same" {
		t.Fatalf("%q, %v", got, err)
	}
}

func TestNewIDIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := NewID()
		if len(id) != 16 || seen[id] {
			t.Fatalf("id %q", id)
		}
		seen[id] = true
	}
}
