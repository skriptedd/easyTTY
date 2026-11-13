package textdiff

import (
	"fmt"
	"strings"
	"testing"
)

// render shows a diff as "-old", "+new", " same" lines.
func render(ls []Line) string {
	var b strings.Builder
	for _, l := range ls {
		b.WriteString(l.Kind + l.Text + "\n")
	}
	return b.String()
}

func TestDiffMarksChangedLines(t *testing.T) {
	old := Lines("hostname sw1\ninterface Gi1/0/1\n description alt\n!\nend\n")
	new := Lines("hostname sw1\ninterface Gi1/0/1\n description neu\n switchport mode access\n!\nend\n")

	got := Diff(old, new)

	want := " hostname sw1\n interface Gi1/0/1\n- description alt\n+ description neu\n+ switchport mode access\n !\n end\n"
	if render(got) != want {
		t.Errorf("got\n%s\nwant\n%s", render(got), want)
	}
	if rm, add := Changed(got); rm != 1 || add != 2 {
		t.Errorf("Changed = %d, %d", rm, add)
	}
}

func TestDiffKeepsLineNumbers(t *testing.T) {
	got := Diff([]string{"a", "b", "c"}, []string{"a", "x", "c", "d"})

	want := []Line{
		{Kind: Same, Text: "a", OldNo: 1, NewNo: 1},
		{Kind: Removed, Text: "b", OldNo: 2},
		{Kind: Added, Text: "x", NewNo: 2},
		{Kind: Same, Text: "c", OldNo: 3, NewNo: 3},
		{Kind: Added, Text: "d", NewNo: 4},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestDiffEdgeCases(t *testing.T) {
	cases := []struct {
		name     string
		old, new []string
		want     string
	}{
		{"both empty", nil, nil, ""},
		{"equal", []string{"a", "b"}, []string{"a", "b"}, " a\n b\n"},
		{"all new", nil, []string{"a"}, "+a\n"},
		{"all gone", []string{"a"}, nil, "-a\n"},
		{"moved line", []string{"a", "b", "c"}, []string{"b", "c", "a"}, "-a\n b\n c\n+a\n"},
		{"repeated lines", []string{"!", "x", "!"}, []string{"!", "!"}, " !\n-x\n !\n"},
	}
	for _, c := range cases {
		if got := render(Diff(c.old, c.new)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDiffTooDifferentStillListsEverything(t *testing.T) {
	old, new := make([]string, 3000), make([]string, 3000)
	for i := range old {
		old[i], new[i] = fmt.Sprint("alt ", i), fmt.Sprint("neu ", i)
	}

	got := Diff(old, new)

	if rm, add := Changed(got); rm != 3000 || add != 3000 || len(got) != 6000 {
		t.Errorf("removed %d, added %d, lines %d", rm, add, len(got))
	}
}

func TestLinesHandlesLineEndings(t *testing.T) {
	if got := Lines("a\r\nb\rc\n"); strings.Join(got, "|") != "a|b|c" {
		t.Errorf("got %q", got)
	}
	if got := Lines(""); got != nil {
		t.Errorf("empty text gives %q", got)
	}
}
