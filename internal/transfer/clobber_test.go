package transfer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Two download jobs planned before either finished must both keep their file.
func TestDownloadsPlannedTogetherNeverOverwrite(t *testing.T) {
	c := memClient(t)
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a", "sw.cfg"), []byte("AAA"))
	writeFile(t, filepath.Join(src, "b", "sw.cfg"), []byte("BBB"))
	if err := Upload(context.Background(), c, Progress{}, []string{filepath.Join(src, "a")}, "/", OnExistOverwrite, func(Progress) {}); err != nil {
		t.Fatal(err)
	}
	if err := Upload(context.Background(), c, Progress{}, []string{filepath.Join(src, "b")}, "/", OnExistOverwrite, func(Progress) {}); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	ia, _, err := planDownload(context.Background(), c, []string{"/a/sw.cfg"}, out)
	if err != nil {
		t.Fatal(err)
	}
	ib, _, err := planDownload(context.Background(), c, []string{"/b/sw.cfg"}, out)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range append(ia, ib...) {
		if err := downloadFile(context.Background(), c, it, &tracker{report: func(Progress) {}}); err != nil {
			t.Fatal(err)
		}
	}
	ents, _ := os.ReadDir(out)
	if len(ents) != 2 {
		b, _ := os.ReadFile(filepath.Join(out, "sw.cfg"))
		t.Fatalf("expected 2 files, got %d; sw.cfg=%q (job A's download was overwritten)", len(ents), b)
	}
	a, _ := os.ReadFile(filepath.Join(out, "sw.cfg"))
	b, _ := os.ReadFile(filepath.Join(out, "sw (1).cfg"))
	if string(a) != "AAA" || string(b) != "BBB" {
		t.Fatalf("sw.cfg=%q, sw (1).cfg=%q", a, b)
	}
}

// A file named like an existing remote folder must not push the folder aside.
func TestUploadFileNeverReplacesRemoteFolder(t *testing.T) {
	c := memClient(t)
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "foo", "keep.txt"), []byte("dir content"))
	if err := Upload(context.Background(), c, Progress{}, []string{filepath.Join(src, "foo")}, "/", OnExistOverwrite, func(Progress) {}); err != nil {
		t.Fatal(err)
	}
	src2 := t.TempDir()
	writeFile(t, filepath.Join(src2, "foo"), []byte("file"))
	err := Upload(context.Background(), c, Progress{}, []string{filepath.Join(src2, "foo")}, "/", OnExistOverwrite, func(Progress) {})
	fi, serr := c.Stat("/foo")
	_, berr := c.Stat("/foo.easytty-old/keep.txt")
	if err == nil || serr != nil || !fi.IsDir() || berr == nil {
		t.Fatalf("upload err=%v, /foo dir=%v (%v), backup left=%v", err, fi != nil && fi.IsDir(), serr, berr == nil)
	}
}
