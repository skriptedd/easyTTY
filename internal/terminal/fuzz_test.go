package terminal

import (
	"strings"
	"testing"
)

func FuzzSplitPasteLines(f *testing.F) {
	f.Add("a\r\nb\nc\r")
	f.Fuzz(func(t *testing.T, text string) {
		for _, l := range SplitPasteLines(text) {
			if strings.ContainsAny(l, "\r\n") {
				t.Fatalf("line break in %q", l)
			}
		}
	})
}

func FuzzSafeName(f *testing.F) {
	f.Add("CON")
	f.Add("a/b:c.")
	f.Fuzz(func(t *testing.T, s string) {
		out := safeName(s)
		if out == "" || strings.ContainsAny(out, `\/:*?"<>|`) || strings.HasSuffix(out, ".") || strings.HasSuffix(out, " ") {
			t.Fatalf("%q -> %q", s, out)
		}
	})
}

func FuzzSessionLog(f *testing.F) {
	f.Add([]byte("x\x1b[31my\x1bP\x1b\\\r\n\x08"))
	f.Fuzz(func(t *testing.T, raw []byte) {
		l, err := NewSessionLog(&bufCloser{}, LogInfo{}, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = l.Write(raw)
		_ = l.Close()
	})
}
