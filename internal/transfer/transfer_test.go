package transfer

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/pkg/sftp"
)

type rwc struct {
	io.Reader
	io.WriteCloser
}

func memClient(t *testing.T) *sftp.Client {
	t.Helper()
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	srv := sftp.NewRequestServer(rwc{sr, sw}, sftp.InMemHandler())
	go srv.Serve()
	c, err := sftp.NewClientPipe(cr, cw, sftp.UseConcurrentWrites(true), sftp.UseConcurrentReads(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sw.Close(); cw.Close(); c.Close() })
	return c
}

type progressLog struct {
	mu sync.Mutex
	ps []Progress
}

func (l *progressLog) add(p Progress) { l.mu.Lock(); l.ps = append(l.ps, p); l.mu.Unlock() }
func (l *progressLog) last() Progress {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ps[len(l.ps)-1]
}

func writeFile(t *testing.T, p string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUploadBrowseDownloadRoundTrip(t *testing.T) {
	c := memClient(t)
	local := t.TempDir()
	big := make([]byte, 3<<20) // multi-packet → concurrent pipeline
	_, _ = rand.Read(big)
	writeFile(t, filepath.Join(local, "configs", "sw1.cfg"), []byte("hostname sw1\n"))
	writeFile(t, filepath.Join(local, "configs", "sub", "big.bin"), big)
	writeFile(t, filepath.Join(local, "Readme.txt"), []byte("hi"))

	if err := Mkdir(c, "/data"); err != nil {
		t.Fatal(err)
	}
	var up progressLog
	err := Upload(context.Background(), c, Progress{JobID: "j1", TabID: "t1"},
		[]string{filepath.Join(local, "configs"), filepath.Join(local, "Readme.txt")}, "/data", OnExistOverwrite, up.add)
	if err != nil {
		t.Fatal(err)
	}
	final := up.last()
	if final.State != StateDone || final.Files != 3 || final.FilesDone != 3 || final.Done != final.Total || final.Kind != "upload" || final.JobID != "j1" {
		t.Fatalf("bad final progress: %+v", final)
	}

	l, err := List(c, "/data")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 2 || !l.Entries[0].IsDir || l.Entries[0].Name != "configs" || l.Entries[1].Name != "Readme.txt" {
		t.Fatalf("listing not sorted dirs-first: %+v", l.Entries)
	}

	out := t.TempDir()
	var down progressLog
	if err := Download(context.Background(), c, Progress{JobID: "j2"}, []string{"/data/configs"}, out, down.add); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "configs", "sub", "big.bin"))
	if err != nil || !bytes.Equal(got, big) {
		t.Fatalf("big file corrupted: %v", err)
	}
	if d := down.last(); d.State != StateDone || d.Files != 2 || d.Kind != "download" {
		t.Fatalf("bad download progress: %+v", d)
	}
	if _, err := os.Stat(filepath.Join(out, "configs", "sub", "big.bin.part")); !os.IsNotExist(err) {
		t.Fatal(".part file left behind")
	}

	if err := Rename(c, "/data/Readme.txt", "/data/README"); err != nil {
		t.Fatal(err)
	}
	if err := Remove(c, []string{"/data/configs", "/data/README"}); err != nil {
		t.Fatal(err)
	}
	if l, _ := List(c, "/data"); len(l.Entries) != 0 {
		t.Fatalf("remove failed: %+v", l.Entries)
	}
}

func TestCancelledUploadReportsCancelled(t *testing.T) {
	c := memClient(t)
	local := t.TempDir()
	writeFile(t, filepath.Join(local, "a.bin"), make([]byte, 1<<20))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var log progressLog
	err := Upload(ctx, c, Progress{}, []string{filepath.Join(local, "a.bin")}, "/", OnExistOverwrite, log.add)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if log.last().State != StateCancelled {
		t.Fatalf("state %q", log.last().State)
	}
}

func TestErrorsAreReported(t *testing.T) {
	c := memClient(t)
	var log progressLog
	if err := Upload(context.Background(), c, Progress{}, []string{filepath.Join(t.TempDir(), "missing")}, "/", OnExistOverwrite, log.add); err == nil {
		t.Fatal("expected error for missing local file")
	}
	if log.last().State != StateError {
		t.Fatal("error not reported")
	}
	if err := Download(context.Background(), c, Progress{}, []string{"/nope"}, t.TempDir(), log.add); err == nil {
		t.Fatal("expected error for missing remote file")
	}
	local := t.TempDir()
	writeFile(t, filepath.Join(local, "f"), []byte("x"))
	if err := Upload(context.Background(), c, Progress{}, []string{filepath.Join(local, "f")}, "/missing-dir", OnExistOverwrite, log.add); err == nil {
		t.Fatal("expected error for missing remote dir")
	}
	if _, err := List(c, "/nope"); err == nil {
		t.Fatal("expected list error")
	}
	if err := Mkdir(c, "/a/b/c"); err == nil {
		t.Fatal("expected mkdir error without parent")
	}
	if err := Rename(c, "/x", "/y"); err == nil {
		t.Fatal("expected rename error")
	}
	for _, p := range []string{"", "/", "."} {
		if err := Remove(c, []string{p}); err == nil {
			t.Fatalf("removing %q must be refused", p)
		}
	}
	if err := Remove(c, []string{"/ghost"}); err == nil {
		t.Fatal("expected error removing missing path")
	}
}

func TestDownloadNeverOverwritesLocalFiles(t *testing.T) {
	c := memClient(t)
	local := t.TempDir()
	writeFile(t, filepath.Join(local, "sw1.cfg"), []byte("new"))
	if err := Upload(context.Background(), c, Progress{}, []string{filepath.Join(local, "sw1.cfg")}, "/", OnExistOverwrite, func(Progress) {}); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	writeFile(t, filepath.Join(out, "sw1.cfg"), []byte("mine"))
	writeFile(t, filepath.Join(out, "sw1 (1).cfg"), []byte("mine too"))
	var log progressLog
	if err := Download(context.Background(), c, Progress{}, []string{"/sw1.cfg"}, out, log.add); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "sw1.cfg")); string(b) != "mine" {
		t.Fatal("existing file was overwritten")
	}
	if b, _ := os.ReadFile(filepath.Join(out, "sw1 (2).cfg")); string(b) != "new" {
		t.Fatalf("download should land in sw1 (2).cfg, got %q", b)
	}
	if log.last().Dest != out {
		t.Fatal("dest not reported")
	}
}

func TestRelUnderAndRemoveGuards(t *testing.T) {
	cases := []struct {
		root, p, rel string
		ok           bool
	}{
		{"/a", "/a", "", true}, {"/a", "/a/b/c", "b/c", true}, {"/", "/x", "x", true},
		{"/a", "/ab", "", false}, {"/a/b", "/a", "", false},
	}
	for _, c := range cases {
		rel, ok := relUnder(c.root, c.p)
		if rel != c.rel || ok != c.ok {
			t.Errorf("relUnder(%q,%q) = %q,%v", c.root, c.p, rel, ok)
		}
	}
	c := memClient(t)
	for _, p := range []string{"//", "/.", "/a/..", ".."} {
		if err := Remove(c, []string{p}); err == nil {
			t.Errorf("removing %q must be refused", p)
		}
	}
}

func TestSafeLocalPath(t *testing.T) {
	dir := t.TempDir()
	if p, err := safeLocalPath(dir, "/sub/file"); err != nil || p != filepath.Join(dir, "sub", "file") {
		t.Fatalf("got %q %v", p, err)
	}
	for _, rel := range []string{"/../evil", "../../x"} {
		if _, err := safeLocalPath(dir, rel); err == nil {
			t.Fatalf("%q should be rejected", rel)
		}
	}
	if runtime.GOOS == "windows" {
		if _, err := safeLocalPath(dir, `/..\..\evil`); err == nil {
			t.Fatal("backslash traversal should be rejected")
		}
	}
}

func put(t *testing.T, c *sftp.Client, p string, data []byte) {
	t.Helper()
	f, err := c.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func TestUploadReplacesRemoteFileOnlyWhenComplete(t *testing.T) {
	c := memClient(t)
	put(t, c, "/app.conf", []byte("old"))
	local := filepath.Join(t.TempDir(), "app.conf")
	writeFile(t, local, []byte("new content"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var log progressLog
	_ = Upload(ctx, c, Progress{}, []string{local}, "/", OnExistOverwrite, log.add)
	f, err := c.Open("/app.conf")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(f)
	f.Close()
	if string(got) != "old" {
		t.Fatalf("cancelled upload damaged the file: %q", got)
	}

	_ = Upload(context.Background(), c, Progress{}, []string{local}, "/", OnExistOverwrite, log.add)
	f, err = c.Open("/app.conf")
	if err != nil {
		t.Fatal(err)
	}
	got, _ = io.ReadAll(f)
	f.Close()
	if string(got) != "new content" {
		t.Fatalf("finished upload: %q (%+v)", got, log.last())
	}
	if _, err := c.Stat("/app.conf.easytty-part"); err == nil {
		t.Error("temp file left behind")
	}
}

func TestDownloadKeepsNamesThatDifferOnlyInCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows folds case")
	}
	c := memClient(t)
	if err := c.Mkdir("/d"); err != nil {
		t.Fatal(err)
	}
	put(t, c, "/d/Readme.txt", []byte("one"))
	put(t, c, "/d/readme.txt", []byte("two"))
	dir := t.TempDir()
	var log progressLog
	_ = Download(context.Background(), c, Progress{}, []string{"/d"}, dir, log.add)
	if p := log.last(); p.Error != "" {
		t.Fatal(p.Error)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "d"))
	if len(entries) != 2 {
		t.Fatalf("got %d files, want 2", len(entries))
	}
}

func TestDownloadLeavesUsersPartFileAlone(t *testing.T) {
	c := memClient(t)
	put(t, c, "/a.bin", []byte("data"))
	dir := t.TempDir()
	part := filepath.Join(dir, "a.bin.part")
	writeFile(t, part, []byte("mine"))
	var log progressLog
	_ = Download(context.Background(), c, Progress{}, []string{"/a.bin"}, dir, log.add)
	if got, _ := os.ReadFile(part); string(got) != "mine" {
		t.Fatalf("user file changed: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.bin")); string(got) != "data" {
		t.Fatalf("download: %q", got)
	}
}

func TestUploadOnExistingNames(t *testing.T) {
	cases := []struct {
		onExist string
		want    map[string]string // remote path -> content
		skipped int
	}{
		{OnExistOverwrite, map[string]string{"/sw1.cfg": "new", "/notes.txt": "fresh"}, 0},
		{OnExistRename, map[string]string{"/sw1.cfg": "old", "/sw1 (1).cfg": "new", "/notes.txt": "fresh"}, 0},
		{OnExistSkip, map[string]string{"/sw1.cfg": "old", "/notes.txt": "fresh"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.onExist, func(t *testing.T) {
			c := memClient(t)
			putRemote(t, c, "/sw1.cfg", "old")
			local := t.TempDir()
			writeFile(t, filepath.Join(local, "sw1.cfg"), []byte("new"))
			writeFile(t, filepath.Join(local, "notes.txt"), []byte("fresh"))
			var log progressLog

			err := Upload(context.Background(), c, Progress{}, []string{filepath.Join(local, "sw1.cfg"), filepath.Join(local, "notes.txt")}, "/", tc.onExist, log.add)

			if err != nil {
				t.Fatal(err)
			}
			for p, want := range tc.want {
				if got := getRemote(t, c, p); got != want {
					t.Errorf("%s = %q, want %q", p, got, want)
				}
			}
			if got := log.last().Skipped; got != tc.skipped {
				t.Errorf("skipped = %d, want %d", got, tc.skipped)
			}
		})
	}
}

func TestUploadRenamesFolderAndKeepsItsContent(t *testing.T) {
	c := memClient(t)
	if err := c.MkdirAll("/configs"); err != nil {
		t.Fatal(err)
	}
	local := t.TempDir()
	writeFile(t, filepath.Join(local, "configs", "a", "sw2.cfg"), []byte("x"))

	err := Upload(context.Background(), c, Progress{}, []string{filepath.Join(local, "configs")}, "/", OnExistRename, func(Progress) {})

	if err != nil {
		t.Fatal(err)
	}
	if got := getRemote(t, c, "/configs (1)/a/sw2.cfg"); got != "x" {
		t.Errorf("content %q", got)
	}
}

func TestUploadRejectsUnknownChoice(t *testing.T) {
	c := memClient(t)
	local := t.TempDir()
	writeFile(t, filepath.Join(local, "f"), []byte("x"))

	if err := Upload(context.Background(), c, Progress{}, []string{filepath.Join(local, "f")}, "/", "delete-all", func(Progress) {}); err == nil {
		t.Error("unknown choice accepted")
	}
}

func TestFreeRemoteName(t *testing.T) {
	taken := map[string]bool{"a.txt": true, "a (1).txt": true, ".profile": true, "dir": true}
	exists := func(n string) bool { return taken[n] }
	for in, want := range map[string]string{"a.txt": "a (2).txt", ".profile": ".profile (1)", "dir": "dir (1)"} {
		if got := freeRemoteName(in, exists); got != want {
			t.Errorf("freeRemoteName(%q) = %q, want %q", in, got, want)
		}
	}
}

func putRemote(t *testing.T, c *sftp.Client, p, content string) {
	t.Helper()
	f, err := c.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func getRemote(t *testing.T, c *sftp.Client, p string) string {
	t.Helper()
	f, err := c.Open(p)
	if err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
