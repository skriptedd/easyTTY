package automation

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func FuzzCleanText(f *testing.F) {
	f.Add([]byte("a\x1b[31mb\x1b]0;t\x07c\r\nd\x08e"))
	f.Add([]byte("\x1b"))
	f.Add([]byte{0xff, 0xfe, 0x1b, '['})
	f.Fuzz(func(t *testing.T, raw []byte) {
		if s := CleanText(raw); strings.Contains(s, "\x1b[") {
			t.Fatalf("escape left in %q", s)
		}
	})
}

func FuzzSplitCommands(f *testing.F) {
	f.Add("show clock\n! comment\n\n# x\nconf t\r\n")
	f.Fuzz(func(t *testing.T, text string) {
		cmds := SplitCommands(text)
		for _, c := range cmds {
			if strings.Contains(c, "\n") {
				t.Fatalf("line break in %q", c)
			}
			if strings.Contains(c, "\r") && CheckCommands(cmds) == nil {
				t.Fatalf("a CR inside %q must be refused", c)
			}
		}
	})
}

func FuzzCleanLabel(f *testing.F) {
	f.Add("Claude\nWann: nie\u202e")
	f.Fuzz(func(t *testing.T, s string) {
		out := CleanLabel(s, 60)
		if HasHiddenChars(out) || utf8.RuneCountInString(out) > 61 { // 60 + "…"
			t.Fatalf("%q -> %q", s, out)
		}
	})
}
