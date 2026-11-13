package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// A share file comes from somewhere else: importing it must never panic, and a
// session that gets in must be valid.
func FuzzImportBundle(f *testing.F) {
	f.Add(`{"format":"easytty-share","version":1,"folders":[{"id":"a","name":"x"},{"id":"b","name":"y","parentId":"a"}],"sessions":[{"name":"s","host":"h","folderId":"b"}]}`)
	f.Add(`{"format":"easytty-share","version":1,"folders":[{"id":"a","parentId":"b"},{"id":"b","parentId":"a"}]}`)
	f.Fuzz(func(t *testing.T, doc string) {
		var b Bundle
		if json.Unmarshal([]byte(doc), &b) != nil {
			return
		}
		st, err := Open(filepath.Join(t.TempDir(), "sessions.json"), xorProtector{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.ImportBundle(b); err != nil {
			return
		}
		for _, s := range st.Tree().Sessions {
			if err := s.Validate(); err != nil {
				t.Fatalf("invalid session stored: %+v: %v", s, err)
			}
		}
	})
}
