package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func seedShare(t *testing.T) (*Store, Folder, Folder, Session, Session) {
	t.Helper()
	st, _ := openTemp(t)
	rz, _ := st.SaveFolder(Folder{Name: "RZ"})
	rack, _ := st.SaveFolder(Folder{Name: "Rack 1", ParentID: rz.ID})
	a, _ := st.SaveSession(Session{Name: "core", Host: "10.0.0.1", User: "admin", Auth: AuthKey, KeyPath: `C:\Users\me\.ssh\id`, LogPath: `C:\me\log`, FolderID: rack.ID})
	b, _ := st.SaveSession(Session{Name: "console", Protocol: ProtocolSerial, SerialPort: "COM6", FolderID: rz.ID})
	_ = st.SetPassword(a.ID, "secret")
	_ = st.Touch(a.ID)
	if _, err := st.SaveFolderLayout(rack.ID, &Layout{Template: "2v", Slots: []string{a.ID, b.ID}}); err != nil {
		t.Fatal(err)
	}
	return st, rz, rack, a, b
}

func TestExportStripsPersonalData(t *testing.T) {
	st, rz, _, _, _ := seedShare(t)
	b := st.Export([]string{rz.ID}, nil, false)
	raw, _ := json.Marshal(b)
	for _, leak := range []string{"admin", "secret", `.ssh`, `C:\\me`} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("export leaks %q: %s", leak, raw)
		}
	}
	if b.Format != ExportFormat || len(b.Folders) != 2 || len(b.Sessions) != 2 {
		t.Fatalf("bundle: %+v", b)
	}
	for _, s := range b.Sessions {
		if s.LastUsed != 0 || s.HasPassword || s.Auth == AuthKey {
			t.Fatalf("not stripped: %+v", s)
		}
	}
}

func TestExportSubsetAndLayoutSlots(t *testing.T) {
	st, _, rack, a, _ := seedShare(t)
	b := st.Export([]string{rack.ID}, nil, false)
	if len(b.Folders) != 1 || b.Folders[0].ParentID != "" || len(b.Sessions) != 1 || b.Sessions[0].ID != a.ID {
		t.Fatalf("subset wrong: %+v", b)
	}
	if l := b.Folders[0].Layout; l == nil || l.Slots[0] != a.ID || l.Slots[1] != "" {
		t.Fatalf("layout slots outside the export must be cleared: %+v", l)
	}
	single := st.Export(nil, []string{a.ID}, false)
	if len(single.Folders) != 0 || len(single.Sessions) != 1 || single.Sessions[0].FolderID != "" {
		t.Fatalf("single session export: %+v", single)
	}
}

func TestImportIntoEmptyAndReimport(t *testing.T) {
	src, _, _, _, _ := seedShare(t)
	b := src.Export(nil, nil, true)

	dst, err := Open(filepath.Join(t.TempDir(), "s.json"), xorProtector{})
	if err != nil {
		t.Fatal(err)
	}
	sum, err := dst.ImportBundle(b)
	if err != nil || sum.Folders != 2 || sum.Sessions != 2 {
		t.Fatalf("import: %+v %v", sum, err)
	}
	tree := dst.Tree()
	var rack Folder
	for _, f := range tree.Folders {
		if f.Name == "Rack 1" {
			rack = f
		}
	}
	if rack.Layout == nil || rack.Layout.Slots[0] == "" || rack.Layout.Slots[1] == "" {
		t.Fatalf("layout not remapped: %+v", rack.Layout)
	}
	for _, sid := range rack.Layout.Slots {
		if _, err := dst.Session(sid); err != nil {
			t.Fatalf("layout points to unknown session %s", sid)
		}
	}
	// Importing the same file again changes nothing.
	again, err := dst.ImportBundle(b)
	if err != nil || again.Folders != 0 || again.Sessions != 0 || again.Skipped != 2 {
		t.Fatalf("re-import should skip: %+v %v", again, err)
	}
	if len(dst.Tree().Sessions) != 2 || len(dst.Tree().Folders) != 2 {
		t.Fatal("re-import duplicated data")
	}
}

func TestImportRejectsForeignAndBroken(t *testing.T) {
	st, _ := openTemp(t)
	if _, err := st.ImportBundle(Bundle{Format: "other"}); err == nil {
		t.Fatal("foreign format must fail")
	}
	if _, err := st.ImportBundle(Bundle{Format: ExportFormat, Version: 99}); err == nil {
		t.Fatal("newer version must fail")
	}
	orphan := Bundle{Format: ExportFormat, Version: 1, Folders: []Folder{{ID: "x", Name: "a", ParentID: "missing"}}}
	if _, err := st.ImportBundle(orphan); err == nil {
		t.Fatal("orphan folder must fail")
	}
}

func TestShareableSettings(t *testing.T) {
	s := ShareableSettings(Settings{LogDir: `C:\me`, FirstRunDone: true, NameCase: CaseLower})
	if s.LogDir != "" || s.FirstRunDone || s.NameCase != CaseLower {
		t.Fatalf("got %+v", s)
	}
}

func TestShareableDropsOtherProtocolFields(t *testing.T) {
	ssh := shareable(Session{Name: "Unraid", Protocol: ProtocolSSH, Host: "u", SerialPort: "COM6", Baud: 9600, DataBits: 8, Parity: "none", StopBits: "1"})
	if ssh.SerialPort != "" || ssh.Baud != 0 || ssh.Parity != "" || ssh.StopBits != "" || ssh.Port != 22 {
		t.Errorf("ssh: %+v", ssh)
	}
	if n := (Session{Name: "K", Protocol: ProtocolSerial, SerialPort: "COM6", User: "admin"}).Normalize(); n.User != "admin" {
		t.Errorf("serial login user dropped: %+v", n)
	}
	ser := shareable(Session{Name: "Konsole", Protocol: ProtocolSerial, SerialPort: "COM6", Host: "old", Port: 22, Auth: AuthAuto, KeepAlive: 30})
	if ser.Host != "" || ser.Port != 0 || ser.Auth != "" || ser.KeepAlive != 0 || ser.Baud != 9600 {
		t.Errorf("serial: %+v", ser)
	}
}

func TestImportOwnKeepsUserAndKey(t *testing.T) {
	st, _ := openTemp(t)
	b := Bundle{
		Format:  ExportFormat,
		Version: exportVersion,
		Folders: []Folder{{ID: "f1", Name: "SecureCRT Import"}, {ID: "f2", Name: "RZ", ParentID: "f1"}},
		Sessions: []Session{
			{ID: "s1", Name: "core", Protocol: ProtocolSSH, Host: "10.0.0.1", User: "admin", KeyPath: `C:\k\id`, Auth: AuthKey, FolderID: "f2", HasPassword: true},
		},
	}
	sum, err := st.ImportOwn(b)
	if err != nil || sum.Sessions != 1 || sum.Folders != 2 {
		t.Fatalf("%+v %v", sum, err)
	}
	got := st.Tree().Sessions[0]
	if got.User != "admin" || got.KeyPath != `C:\k\id` || got.Auth != AuthKey || got.HasPassword {
		t.Errorf("own import lost data: %+v", got)
	}
	if sum, _ := st.ImportOwn(b); sum.Sessions != 0 || sum.Skipped != 1 {
		t.Errorf("re-import must skip: %+v", sum)
	}
	if sum, _ := st.ImportBundle(b); sum.Sessions != 0 {
		t.Errorf("share import of the same session must skip too: %+v", sum)
	}
}

func TestImportedRDPSessionCannotReachLocalDrives(t *testing.T) {
	st, _ := openTemp(t)
	bundle := Bundle{Format: ExportFormat, Version: exportVersion, Sessions: []Session{
		{ID: "x", Name: "evil", Protocol: ProtocolRDP, Host: "rdp.example", RdpDrives: true},
	}}
	if _, err := st.ImportBundle(bundle); err != nil {
		t.Fatal(err)
	}
	got := st.Tree().Sessions
	if len(got) != 1 || got[0].RdpDrives {
		t.Fatalf("imported session keeps redirection: %+v", got)
	}
}
