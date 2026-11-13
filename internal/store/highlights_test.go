package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanHighlights(t *testing.T) {
	in := []Highlight{
		{Name: "  VLAN  ", Kind: "words", Pattern: " vlan10, vlan20 ", Color: "yellow"},
		{Name: "leer", Pattern: "   "},
		{Name: "zu lang", Pattern: strings.Repeat("a", maxHighlightPattern+1)},
		{Name: "Farbe falsch", Kind: "regex", Pattern: "x+", Color: "pink", Off: true},
		{Name: "Art falsch", Kind: "glob", Pattern: "x", Color: "red"},
	}

	got := cleanHighlights(in)

	want := []Highlight{
		{Name: "VLAN", Kind: "words", Pattern: "vlan10, vlan20", Color: "yellow"},
		{Name: "Farbe falsch", Kind: "regex", Pattern: "x+", Color: "red", Off: true},
		{Name: "Art falsch", Kind: "words", Pattern: "x", Color: "red"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rule %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestHighlightDefaultsAndEmptyList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := LoadSettings(path) // no file yet
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Highlights) != len(DefaultHighlights()) {
		t.Fatalf("fresh settings have %d rules", len(s.Highlights))
	}

	s.Highlights = []Highlight{} // the user deleted every rule
	if _, err := SaveSettings(path, s); err != nil {
		t.Fatal(err)
	}
	again, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Highlights == nil || len(again.Highlights) != 0 {
		t.Errorf("deleted rules came back: %+v", again.Highlights)
	}
}

func TestOldSettingsGetDefaultHighlights(t *testing.T) {
	var s = DefaultSettings()
	if err := json.Unmarshal([]byte(`{"fontSize":14}`), &s); err != nil {
		t.Fatal(err)
	}

	if got := s.Sanitize().Highlights; len(got) == 0 {
		t.Error("settings file without highlights got none")
	}
}
