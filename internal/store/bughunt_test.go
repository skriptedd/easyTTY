package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A jump host may not be changed so that sessions using it break.
func TestJumpHostChangeKeepsUsersSaveable(t *testing.T) {
	st, _ := openTemp(t)
	b, err := st.SaveSession(Session{Name: "b", Host: "b.example"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.SaveSession(Session{Name: "a", Host: "a.example", JumpID: b.ID})
	if err != nil {
		t.Fatal(err)
	}
	serial := b
	serial.Protocol, serial.SerialPort = ProtocolSerial, "COM3"
	if _, err := st.SaveSession(serial); err == nil || !strings.Contains(err.Error(), "„a“") {
		t.Fatalf("jump host turned serial although „a“ uses it: %v", err)
	}
	a.Name = "a2"
	if _, err := st.SaveSession(a); err != nil {
		t.Fatalf("user of the jump host no longer saveable: %v", err)
	}
}

func TestJumpChainOfUserMayNotGrowBeyondMax(t *testing.T) {
	st, _ := openTemp(t)
	mk := func(name, jump string) Session {
		s, err := st.SaveSession(Session{Name: name, Host: name + ".x", JumpID: jump})
		if err != nil {
			t.Fatal(name, err)
		}
		return s
	}
	e := mk("e", "")
	d := mk("d", e.ID)
	b := mk("b", "")
	a := mk("a", b.ID)
	mk("c", a.ID)   // c -> a -> b
	b.JumpID = d.ID // would make c -> a -> b -> d -> e
	if _, err := st.SaveSession(b); err == nil {
		t.Fatal("chain of „c“ grew beyond the limit")
	}
}

func TestReimportFindsTrimmedFolder(t *testing.T) {
	st, _ := openTemp(t)
	b := Bundle{Format: ExportFormat, Version: 1,
		Folders:  []Folder{{ID: "f1", Name: " Lab "}, {ID: "f2", Name: "  "}},
		Sessions: []Session{{ID: "s1", Name: "x", Host: "h", FolderID: "f1"}, {ID: "s2", Name: "y", Host: "h2", FolderID: "f2"}}}
	for range 2 {
		if _, err := st.ImportBundle(b); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(st.Tree().Folders); n != 2 {
		t.Fatalf("re-import created %d folders, want 2", n)
	}
}

// After falling back to the .bak, the next save must not copy the damaged
// file over the good backup.
func TestRecoveryKeepsGoodBackup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sessions.json")
	st, err := Open(p, xorProtector{})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"one", "two"} {
		if _, err := st.SaveSession(Session{Name: n, Host: n + ".h"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(p, []byte("{garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	st2, err := Open(p, xorProtector{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st2.Touch(st2.Tree().Sessions[0].ID); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(p + ".bak")
	if err != nil || !json.Valid(bak) {
		t.Fatalf(".bak damaged after recovery: %q (%v)", bak, err)
	}
}
