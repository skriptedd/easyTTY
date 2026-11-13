package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCrashLogAppendsStartLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", crashLogName)
	for range 2 {
		f, err := openCrashLog(path)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "gestartet"); n != 2 {
		t.Fatalf("want 2 start lines, got %d: %q", n, raw)
	}
}

func TestOpenCrashLogMovesLargeLogAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), crashLogName)
	if err := os.WriteFile(path, make([]byte, maxCrashLog+1), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := openCrashLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if fi, err := os.Stat(path + ".old"); err != nil || fi.Size() != maxCrashLog+1 {
		t.Fatalf("old log not kept: %v %v", fi, err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Size() > 200 {
		t.Fatalf("new log not fresh: %v %v", fi, err)
	}
}
