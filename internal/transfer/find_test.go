package transfer

import (
	"context"
	"sort"
	"testing"

	"github.com/pkg/sftp"
)

func findTree(t *testing.T) *sftp.Client {
	t.Helper()
	c := memClient(t)
	for _, d := range []string{"/srv", "/srv/logs", "/srv/logs/old", "/srv/app"} {
		if err := c.Mkdir(d); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"/srv/readme.txt", "/srv/logs/app.log", "/srv/logs/old/app.log.1", "/srv/app/App.LOG", "/srv/app/config.yml"} {
		putRemote(t, c, f, "x")
	}
	return c
}

func foundPaths(r FindResult) []string {
	out := make([]string, 0, len(r.Entries))
	for _, e := range r.Entries {
		out = append(out, e.Path)
	}
	sort.Strings(out)
	return out
}

func TestFindSearchesSubfoldersCaseInsensitive(t *testing.T) {
	c := findTree(t)
	r, err := Find(context.Background(), c, "/srv", "*.log")
	if err != nil {
		t.Fatal(err)
	}
	got := foundPaths(r)
	want := []string{"/srv/app/App.LOG", "/srv/logs/app.log"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v", got, want)
	}
	if r.Stopped != "" || r.Dirs != 4 || r.Dir != "/srv" {
		t.Fatalf("result %+v", r)
	}
}

func TestFindWordsMustAllMatch(t *testing.T) {
	c := findTree(t)
	r, err := Find(context.Background(), c, "/srv", "app log")
	if err != nil {
		t.Fatal(err)
	}
	if got := foundPaths(r); len(got) != 3 { // app.log, app.log.1, App.LOG
		t.Fatalf("got %v", got)
	}
	r, _ = Find(context.Background(), c, "/srv", "logs")
	if got := foundPaths(r); len(got) != 1 || got[0] != "/srv/logs" || !r.Entries[0].IsDir {
		t.Fatalf("folders are found too, got %v", got)
	}
}

func TestFindNearestFirst(t *testing.T) {
	c := findTree(t)
	r, _ := Find(context.Background(), c, "/srv", "app")
	if len(r.Entries) == 0 || r.Entries[0].Path != "/srv/app" {
		t.Fatalf("top level hit should come first: %v", foundPaths(r))
	}
}

func TestFindStopsWhenCancelled(t *testing.T) {
	c := findTree(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := Find(ctx, c, "/srv", "app")
	if err != nil || r.Stopped != StopCancelled || len(r.Entries) != 0 {
		t.Fatalf("got %+v, %v", r, err)
	}
}

func TestFindRejectsEmptyQueryAndMissingDir(t *testing.T) {
	c := findTree(t)
	if _, err := Find(context.Background(), c, "/srv", "   "); err == nil {
		t.Fatal("empty query accepted")
	}
	if _, err := Find(context.Background(), c, "/nope", "x"); err == nil {
		t.Fatal("missing folder accepted")
	}
}

func TestMatcherWildcards(t *testing.T) {
	cases := []struct {
		q, name string
		want    bool
	}{
		{"*.log", "a.log", true},
		{"*.log", "a.log.1", false},
		{"a?c", "ABC", true},
		{"a?c", "abbc", false},
		{"[x]", "file[x].txt", true}, // regexp characters are plain text
		{"(1)*", "(1) copy", true},
	}
	for _, tc := range cases {
		m, err := NewMatcher(tc.q)
		if err != nil {
			t.Fatal(err)
		}
		if got := m(tc.name); got != tc.want {
			t.Errorf("%q on %q: got %v", tc.q, tc.name, got)
		}
	}
}
