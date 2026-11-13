package transfer

import (
	"path/filepath"
	"strings"
	"testing"
)

// A server decides the names: none may lead outside the target folder.
func FuzzSafeLocalPath(f *testing.F) {
	for _, s := range []string{"a/b.txt", "../x", `..\x`, `C:\Windows\x`, "NUL", "a:b", "a/./c", ""} {
		f.Add(s)
	}
	root := filepath.Join(`C:\`, "ziel")
	f.Fuzz(func(t *testing.T, rel string) {
		p, err := safeLocalPath(root, rel)
		if err != nil {
			return
		}
		if p != root && !strings.HasPrefix(p, root+string(filepath.Separator)) {
			t.Fatalf("%q escapes to %q", rel, p)
		}
	})
}
