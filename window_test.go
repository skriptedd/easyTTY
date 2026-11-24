package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFitWindowKeepsWindowsReachable(t *testing.T) {
	work := rect{0, 0, 1920, 1040}
	cases := []struct {
		name string
		win  rect
		want rect
		max  bool
	}{
		{"fits", rect{100, 100, 1500, 980}, rect{100, 100, 1500, 980}, false},
		{"title bar above screen", rect{200, -300, 1600, 580}, rect{200, 0, 1600, 880}, false},
		{"hanging off the right", rect{1800, 100, 3200, 980}, rect{520, 100, 1920, 980}, false},
		{"taller than screen", rect{100, 50, 1500, 1250}, rect{100, 0, 1500, 1040}, false},
		{"bigger than screen", rect{-8, -8, 1928, 1048}, rect{-8, -8, 1928, 1048}, true},
	}
	for _, c := range cases {
		got, max := fitWindow(c.win, work)
		if got != c.want || max != c.max {
			t.Errorf("%s: fitWindow = %v, %v; want %v, %v", c.name, got, max, c.want, c.max)
		}
	}
}

func TestCentredShrinksToWorkArea(t *testing.T) {
	work := rect{1920, 0, 3200, 1000} // a second, smaller monitor
	got := centred(rect{-5000, -5000, -3600, -4120}, work)
	if want := (rect{1920, 60, 3200, 940}); got != want {
		t.Fatalf("centred = %v, want %v", got, want)
	}
}

func TestWindowStateFile(t *testing.T) {
	dir := t.TempDir()
	if _, ok := loadWindowState(dir); ok {
		t.Fatal("no file must mean no state")
	}
	want := windowState{X: 10, Y: 20, W: 1400, H: 880, Max: true}
	if err := saveWindowState(dir, want); err != nil {
		t.Fatal(err)
	}
	if got, ok := loadWindowState(dir); !ok || got != want {
		t.Fatalf("got %+v, %v", got, ok)
	}
	// A destroyed window reports 0×0: that must not replace the good state.
	if err := saveWindowState(dir, windowState{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := loadWindowState(dir); got != want {
		t.Fatalf("empty state overwrote the file: %+v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, windowFile), []byte(`{"w":5,"h":5}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadWindowState(dir); ok {
		t.Fatal("a tiny window is a broken file")
	}
}
