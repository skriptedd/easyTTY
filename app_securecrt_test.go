package main

import (
	"os"
	"path/filepath"
	"testing"

	"easytty/internal/store"
)

type plainProtector struct{}

func (plainProtector) Protect(b []byte) ([]byte, error)   { return b, nil }
func (plainProtector) Unprotect(b []byte) ([]byte, error) { return b, nil }

func TestImportSecureCRTIntoOwnFolder(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "sessions.json"), plainProtector{})
	if err != nil {
		t.Fatal(err)
	}
	a := &App{store: st, settingsPath: filepath.Join(dir, "settings.json"), home: dir}

	src := filepath.Join(dir, "Sessions")
	files := map[string]string{
		"RZ/core sw1.ini":  "S:\"Protocol Name\"=SSH2\r\nS:\"Hostname\"=10.0.0.1\r\nS:\"Username\"=admin\r\nD:\"[SSH2] Port\"=00000016\r\n",
		"lab console.ini":  "S:\"Protocol Name\"=Serial\r\nS:\"Com Port\"=COM6\r\nD:\"Baud Rate\"=00002580\r\n",
		"alt/telnet-r.ini": "S:\"Protocol Name\"=Telnet\r\nS:\"Hostname\"=r1\r\n",
	}
	for rel, content := range files {
		p := filepath.Join(src, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	r, err := a.importSecureCRT(src)
	if err != nil || r.Imported != 2 || len(r.Skipped) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	tree := st.Tree()
	folders := map[string]store.Folder{}
	for _, f := range tree.Folders {
		folders[f.ID] = f
	}
	for _, s := range tree.Sessions {
		f := folders[s.FolderID]
		top := f
		for top.ParentID != "" {
			top = folders[top.ParentID]
		}
		if top.Name != "SecureCRT Import" {
			t.Errorf("%s not under SecureCRT Import (folder %q)", s.Name, f.Name)
		}
		if s.Color == "" {
			t.Errorf("%s got no colour", s.Name)
		}
		if s.Protocol == store.ProtocolSSH && (s.User != "admin" || f.Name != "RZ") {
			t.Errorf("ssh session: %+v in %q", s, f.Name)
		}
	}

	again, err := a.importSecureCRT(src)
	if err != nil || again.Imported != 0 || again.Existing != 2 {
		t.Errorf("second import must not duplicate: %+v %v", again, err)
	}
}
