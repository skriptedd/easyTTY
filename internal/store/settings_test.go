package store

import (
	"strings"
	"testing"
)

func TestSanitizeCleansSnippets(t *testing.T) {
	in := Settings{Snippets: []Snippet{
		{Name: "  Uhrzeit  ", Text: "show clock\r\n"},
		{Name: "", Text: "show ip int brief\nshow vlan"},
		{Name: "leer", Text: "  \n "},
		{Name: "Esc\x1b[201~", Text: "conf t\x03\x1b\n\tend"},
		{Name: strings.Repeat("x", 100), Text: "a"},
		{Name: "zu lang", Text: strings.Repeat("a", maxSnippetBytes+1)},
	}}

	got := in.Sanitize().Snippets

	want := []Snippet{
		{Name: "Uhrzeit", Text: "show clock"},
		{Name: "show ip int brief", Text: "show ip int brief\nshow vlan"},
		{Name: "Esc[201~", Text: "conf t\n\tend"},
		{Name: strings.Repeat("x", maxSnippetName), Text: "a"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d snippets: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("snippet %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestSanitizeCapsSnippetCount(t *testing.T) {
	many := make([]Snippet, maxSnippets+10)
	for i := range many {
		many[i] = Snippet{Name: "n", Text: "show clock"}
	}

	if got := (Settings{Snippets: many}).Sanitize().Snippets; len(got) != maxSnippets {
		t.Errorf("kept %d snippets, want %d", len(got), maxSnippets)
	}
}

func TestMergeSnippetsKeepsLocalAndAddsNew(t *testing.T) {
	local := []Snippet{{Name: "a", Text: "show a"}}
	shared := []Snippet{{Name: "a", Text: "show a"}, {Name: "b", Text: "show b"}}

	got := MergeSnippets(local, shared)

	if len(got) != 2 || got[0] != local[0] || got[1] != shared[1] {
		t.Errorf("merged = %v", got)
	}
}

func TestSanitizeKeepsSaveOutput(t *testing.T) {
	in := Settings{Snippets: []Snippet{{Name: "a", Text: "show version", SaveOutput: true}}}

	if got := in.Sanitize().Snippets; len(got) != 1 || !got[0].SaveOutput {
		t.Errorf("SaveOutput lost: %+v", got)
	}
}
