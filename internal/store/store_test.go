package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type xorProtector struct{}

func (xorProtector) Protect(p []byte) ([]byte, error) {
	out := make([]byte, len(p))
	for i, b := range p {
		out[i] = b ^ 0x5a
	}
	return out, nil
}
func (x xorProtector) Unprotect(c []byte) ([]byte, error) { return x.Protect(c) }

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sessions.json")
	st, err := Open(path, xorProtector{})
	if err != nil {
		t.Fatal(err)
	}
	return st, path
}

func TestSaveSessionAppliesDefaultsAndPersists(t *testing.T) {
	st, path := openTemp(t)

	got, err := st.SaveSession(Session{Host: " router1 ", User: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == "" || got.Port != 22 || got.Auth != AuthAuto || got.Name != "router1" || got.Protocol != ProtocolSSH {
		t.Fatalf("defaults not applied: %+v", got)
	}

	reopened, err := Open(path, xorProtector{})
	if err != nil {
		t.Fatal(err)
	}
	tree := reopened.Tree()
	if len(tree.Sessions) != 1 || tree.Sessions[0].Host != "router1" {
		t.Fatalf("not persisted: %+v", tree)
	}
}

func TestSaveSessionRejectsInvalid(t *testing.T) {
	st, _ := openTemp(t)
	cases := []Session{
		{Protocol: ProtocolSSH},
		{Host: "a b"},
		{Host: "x", Port: 70000},
		{Host: "x", Auth: AuthKey},
		{Host: "x", Auth: "magic"},
		{Protocol: ProtocolSerial},
		{Protocol: ProtocolSerial, SerialPort: "COM1", Parity: "weird"},
		{Protocol: ProtocolSerial, SerialPort: "COM1", StopBits: "3"},
		{Protocol: ProtocolSerial, SerialPort: "COM1", DataBits: 9},
		{Protocol: "telnet", Host: "x"},
		{Host: "x", KeepAlive: -1},
		{Host: "x", FolderID: "missing"},
	}
	for i, c := range cases {
		if _, err := st.SaveSession(c); err == nil {
			t.Errorf("case %d: expected error for %+v", i, c)
		}
	}
	if n := len(st.Tree().Sessions); n != 0 {
		t.Fatalf("invalid sessions stored: %d", n)
	}
}

func TestSerialDefaults(t *testing.T) {
	st, _ := openTemp(t)
	got, err := st.SaveSession(Session{Protocol: ProtocolSerial, SerialPort: "COM3"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Baud != 9600 || got.DataBits != 8 || got.Parity != "none" || got.StopBits != "1" || got.Name != "COM3" {
		t.Fatalf("serial defaults wrong: %+v", got)
	}
}

func TestUpdateKeepsLastUsedAndUnknownIDFails(t *testing.T) {
	st, _ := openTemp(t)
	s, _ := st.SaveSession(Session{Host: "h"})
	if err := st.Touch(s.ID); err != nil {
		t.Fatal(err)
	}
	touched, _ := st.Session(s.ID)
	if touched.LastUsed == 0 {
		t.Fatal("touch did not set LastUsed")
	}
	touched.Name = "renamed"
	touched.LastUsed = 0
	upd, err := st.SaveSession(touched)
	if err != nil {
		t.Fatal(err)
	}
	if upd.LastUsed == 0 || upd.Name != "renamed" {
		t.Fatalf("update lost data: %+v", upd)
	}
	if _, err := st.SaveSession(Session{ID: "nope", Host: "h"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := st.Touch("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestFoldersMoveAndCycleProtection(t *testing.T) {
	st, _ := openTemp(t)
	a, _ := st.SaveFolder(Folder{Name: "A"})
	b, _ := st.SaveFolder(Folder{Name: "B", ParentID: a.ID})
	c, _ := st.SaveFolder(Folder{Name: "C", ParentID: b.ID})

	if err := st.MoveFolder(a.ID, c.ID); err == nil {
		t.Fatal("moving folder into its descendant must fail")
	}
	if err := st.MoveFolder(a.ID, a.ID); err == nil {
		t.Fatal("moving folder into itself must fail")
	}
	if err := st.MoveFolder(c.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveFolder(Folder{Name: "  "}); err == nil {
		t.Fatal("empty folder name must fail")
	}
	if _, err := st.SaveFolder(Folder{Name: "x", ParentID: "missing"}); err == nil {
		t.Fatal("missing parent must fail")
	}
	if err := st.MoveFolder("missing", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestFolderLayout(t *testing.T) {
	st, path := openTemp(t)
	f, _ := st.SaveFolder(Folder{Name: "Rack 1"})
	a, _ := st.SaveSession(Session{Host: "sw1", FolderID: f.ID})
	b, _ := st.SaveSession(Session{Host: "sw2", FolderID: f.ID})

	got, err := st.SaveFolderLayout(f.ID, &Layout{Template: "3m", Slots: []string{a.ID, "", b.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Layout == nil || got.Layout.Template != "3m" || len(got.Layout.Slots) != 3 || got.Layout.Slots[2] != b.ID {
		t.Fatalf("layout not stored: %+v", got.Layout)
	}
	if l, _ := st.SaveFolderLayout(f.ID, &Layout{Template: "4", Slots: []string{a.ID}}); len(l.Layout.Slots) != 4 {
		t.Fatal("slots must be padded to the tile count")
	}

	// A rename (UI sends the folder without layout) keeps it.
	if _, err := st.SaveFolder(Folder{ID: f.ID, Name: "Rack A"}); err != nil {
		t.Fatal(err)
	}
	reopened, _ := Open(path, xorProtector{})
	if fl := reopened.Tree().Folders[0]; fl.Name != "Rack A" || fl.Layout == nil || fl.Layout.Template != "4" {
		t.Fatalf("layout lost on rename/reload: %+v", fl)
	}

	bad := []*Layout{
		{Template: "9x9"},
		{Template: "2v", Slots: []string{a.ID, b.ID, a.ID}},
		{Template: "1", Slots: []string{"missing"}},
	}
	for _, l := range bad {
		if _, err := st.SaveFolderLayout(f.ID, l); err == nil {
			t.Errorf("expected error for %+v", l)
		}
	}
	if _, err := st.SaveFolderLayout("nope", nil); !errors.Is(err, ErrNotFound) {
		t.Fatal("want ErrNotFound")
	}
	if cleared, _ := st.SaveFolderLayout(f.ID, nil); cleared.Layout != nil {
		t.Fatal("nil must remove the layout")
	}
	if nf, _ := st.SaveFolder(Folder{Name: "new", Layout: &Layout{Template: "1"}}); nf.Layout != nil {
		t.Fatal("new folders must not take a layout via SaveFolder")
	}
}

func TestDeleteFolderReparentsChildren(t *testing.T) {
	st, _ := openTemp(t)
	a, _ := st.SaveFolder(Folder{Name: "A"})
	b, _ := st.SaveFolder(Folder{Name: "B", ParentID: a.ID})
	s, _ := st.SaveSession(Session{Host: "h", FolderID: b.ID})

	if err := st.DeleteFolder(b.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Session(s.ID)
	if got.FolderID != a.ID {
		t.Fatalf("session should move to parent A, got %q", got.FolderID)
	}
	if err := st.DeleteFolder(b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestMoveDuplicateDeleteSession(t *testing.T) {
	st, _ := openTemp(t)
	f, _ := st.SaveFolder(Folder{Name: "Core"})
	s, _ := st.SaveSession(Session{Host: "sw1"})
	_ = st.SetPassword(s.ID, "secret")

	if err := st.MoveSession(s.ID, f.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.MoveSession(s.ID, "missing"); err == nil {
		t.Fatal("move into missing folder must fail")
	}
	dup, err := st.DuplicateSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dup.ID == s.ID || dup.FolderID != f.ID || !strings.HasSuffix(dup.Name, "(Kopie)") {
		t.Fatalf("bad duplicate: %+v", dup)
	}
	if pw, _ := st.Password(dup.ID); pw != "" {
		t.Fatal("duplicate must not copy the password")
	}
	if err := st.DeleteSession(s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Session(s.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("session still present")
	}
	if err := st.DeleteSession(s.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("double delete should be ErrNotFound")
	}
	if _, err := st.DuplicateSession("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatal("duplicate unknown should be ErrNotFound")
	}
}

func TestPasswordsAreEncryptedAndHiddenFromTree(t *testing.T) {
	st, path := openTemp(t)
	s, _ := st.SaveSession(Session{Host: "h"})
	if err := st.SetPassword(s.ID, "hunter2"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if bytes.Contains(raw, []byte("hunter2")) {
		t.Fatal("password stored in plain text")
	}
	if !st.Tree().Sessions[0].HasPassword {
		t.Fatal("HasPassword should be true")
	}
	pw, err := st.Password(s.ID)
	if err != nil || pw != "hunter2" {
		t.Fatalf("got %q, %v", pw, err)
	}
	if err := st.SetPassword(s.ID, ""); err != nil {
		t.Fatal(err)
	}
	if pw, _ := st.Password(s.ID); pw != "" {
		t.Fatal("password not cleared")
	}
	if err := st.SetPassword("nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatal("want ErrNotFound")
	}
}

func TestPasswordWithoutProtectorFails(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "s.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := st.SaveSession(Session{Host: "h"})
	if err := st.SetPassword(s.ID, "x"); err == nil {
		t.Fatal("expected error without protector")
	}
}

func TestImportCreatesFolderAndSkipsInvalid(t *testing.T) {
	st, _ := openTemp(t)
	n, err := st.Import("PuTTY Import", []Session{{Host: "a"}, {Host: ""}, {Host: "b", Port: 2222}})
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	n, _ = st.Import("PuTTY Import", []Session{{Host: "c"}, {Host: "a"}, {Host: "c"}})
	tree := st.Tree()
	if n != 1 || len(tree.Folders) != 1 || len(tree.Sessions) != 3 {
		t.Fatalf("second import should reuse folder: %+v", tree)
	}
	if n, _ := st.Import("Empty", []Session{{}}); n != 0 || len(st.Tree().Folders) != 1 {
		t.Fatal("empty import must not create a folder")
	}
}

func TestOpenCorruptFileFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	_ = os.WriteFile(path, []byte("{not json"), 0o600)
	if _, err := Open(path, nil); err == nil {
		t.Fatal("expected error for corrupt file")
	}
}

func TestWriteKeepsBackup(t *testing.T) {
	st, path := openTemp(t)
	_, _ = st.SaveSession(Session{Host: "first"})
	_, _ = st.SaveSession(Session{Host: "second"})
	bak, err := os.ReadFile(path + ".bak")
	if err != nil || !bytes.Contains(bak, []byte("first")) || bytes.Contains(bak, []byte("second")) {
		t.Fatalf("backup should hold previous version: %s %v", bak, err)
	}
}

func TestSettingsRoundTripAndSanitize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := LoadSettings(path)
	if err != nil || !reflect.DeepEqual(s, DefaultSettings()) {
		t.Fatalf("missing file should give defaults: %+v %v", s, err)
	}
	saved, err := SaveSettings(path, Settings{FontSize: 99, Theme: "neon", Scrollback: -5, CopyOnSelect: false})
	if err != nil {
		t.Fatal(err)
	}
	d := DefaultSettings()
	if saved.FontSize != d.FontSize || saved.Theme != d.Theme || saved.Scrollback != d.Scrollback || saved.FontFamily != d.FontFamily {
		t.Fatalf("not sanitized: %+v", saved)
	}
	if !DefaultSettings().SerialWake {
		t.Fatal("serial wake-up Enter must be on by default")
	}
	if saved.LogEnabled || saved.FirstRunDone {
		t.Fatal("explicit false must be kept")
	}
	if clamped := (Settings{PasteLineDelay: 99999}).Sanitize(); clamped.PasteLineDelay != 2000 {
		t.Fatalf("paste delay not clamped: %d", clamped.PasteLineDelay)
	}
	if clamped := (Settings{PasteLineDelay: -5}).Sanitize(); clamped.PasteLineDelay != 0 {
		t.Fatal("negative paste delay must become 0")
	}
	// Older settings files without the logging keys get logging on by default.
	_ = os.WriteFile(path, []byte(`{"fontSize":12,"logDir":"  D:\\Logs  "}`), 0o600)
	old, _ := LoadSettings(path)
	if !old.LogEnabled || old.FirstRunDone || old.FontSize != 12 || old.LogDir != `D:\Logs` {
		t.Fatalf("old file defaults wrong: %+v", old)
	}
	_, _ = SaveSettings(path, saved)
	loaded, _ := LoadSettings(path)
	if !reflect.DeepEqual(loaded, saved) {
		t.Fatalf("round trip mismatch: %+v vs %+v", loaded, saved)
	}
	_ = os.WriteFile(path, []byte("garbage"), 0o600)
	_ = os.Remove(path + ".bak")
	if _, err := LoadSettings(path); err == nil {
		t.Fatal("expected error for corrupt settings without a backup")
	}
}

func TestDPAPIRoundTrip(t *testing.T) {
	p := DefaultProtector()
	if runtime.GOOS != "windows" {
		if p != nil {
			t.Fatal("non-windows should have no protector")
		}
		return
	}
	for _, in := range [][]byte{[]byte("pässwört-123"), {}} {
		c, err := p.Protect(in)
		if err != nil {
			t.Fatal(err)
		}
		out, err := p.Unprotect(c)
		if err != nil || !bytes.Equal(out, in) {
			t.Fatalf("round trip failed: %q %v", out, err)
		}
	}
	if _, err := p.Unprotect([]byte("garbage")); err == nil {
		t.Fatal("garbage should not decrypt")
	}
}

func TestDeleteSessionsAndFolderWithSessions(t *testing.T) {
	st, _ := openTemp(t)
	top, _ := st.SaveFolder(Folder{Name: "RZ"})
	sub, _ := st.SaveFolder(Folder{Name: "Nord", ParentID: top.ID})
	other, _ := st.SaveFolder(Folder{Name: "Lab"})
	_, _ = st.SaveSession(Session{Name: "a", Host: "a", FolderID: top.ID})
	b, _ := st.SaveSession(Session{Name: "b", Host: "b", FolderID: sub.ID})
	c, _ := st.SaveSession(Session{Name: "c", Host: "c", FolderID: other.ID})
	d, _ := st.SaveSession(Session{Name: "d", Host: "d"})
	_ = st.SetPassword(b.ID, "geheim")
	if _, err := st.SaveFolderLayout(other.ID, &Layout{Template: "2v", Slots: []string{c.ID, b.ID}}); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteSessions([]string{d.ID, "gibtsnicht"}); err == nil {
		t.Fatal("unknown id must fail and change nothing")
	}
	if len(st.Tree().Sessions) != 4 {
		t.Fatal("nothing may be deleted on error")
	}

	if err := st.DeleteFolderWithSessions(top.ID); err != nil {
		t.Fatal(err)
	}
	tree := st.Tree()
	var names []string
	for _, x := range tree.Sessions {
		names = append(names, x.Name)
	}
	if strings.Join(names, ",") != "c,d" {
		t.Errorf("sessions left: %v (a and b inside RZ/Nord must be gone)", names)
	}
	if len(tree.Folders) != 1 || tree.Folders[0].Name != "Lab" {
		t.Errorf("folders left: %+v", tree.Folders)
	}
	if pw, _ := st.Password(b.ID); pw != "" {
		t.Error("password of a deleted session kept")
	}
	if l := tree.Folders[0].Layout; l == nil || l.Slots[1] != "" || l.Slots[0] != c.ID {
		t.Errorf("layout slot of a deleted session kept: %+v", l)
	}
	if err := st.DeleteSessions([]string{c.ID, d.ID}); err != nil || len(st.Tree().Sessions) != 0 {
		t.Errorf("multi delete: %v %d", err, len(st.Tree().Sessions))
	}
}

func TestPatchSessionsFolderAndGlobal(t *testing.T) {
	st, _ := openTemp(t)
	rz, _ := st.SaveFolder(Folder{Name: "RZ"})
	nord, _ := st.SaveFolder(Folder{Name: "Nord", ParentID: rz.ID})
	a, _ := st.SaveSession(Session{Name: "a", Host: "a", User: "old", FolderID: rz.ID})
	b, _ := st.SaveSession(Session{Name: "b", Host: "b", FolderID: nord.ID})
	con, _ := st.SaveSession(Session{Name: "con", Protocol: ProtocolSerial, SerialPort: "COM6", FolderID: nord.ID})
	top, _ := st.SaveSession(Session{Name: "top", Host: "t"})

	user, auth, color := "admin", AuthPassword, "#4fb3ff"
	n, err := st.PatchSessions(rz.ID, false, SessionPatch{User: &user})
	if err != nil || n != 1 {
		t.Fatalf("only RZ itself: %d %v", n, err)
	}
	pw := "geheim"
	n, err = st.PatchSessions(rz.ID, true, SessionPatch{Auth: &auth, Color: &color, Password: &pw})
	if err != nil || n != 3 {
		t.Fatalf("RZ with subfolders: %d %v", n, err)
	}
	get := func(id string) Session { s, _ := st.Session(id); return s }
	if got := get(a.ID); got.User != "admin" || got.Auth != AuthPassword || got.Color != color || !got.HasPassword {
		t.Errorf("a: %+v", got)
	}
	if got := get(con.ID); got.Auth != "" || got.Color != color || !got.HasPassword {
		t.Errorf("serial must not get SSH auth: %+v", got)
	}
	if got := get(top.ID); got.Color != "" || got.HasPassword {
		t.Errorf("top level outside RZ changed: %+v", got)
	}

	empty := ""
	if n, err = st.PatchSessions("", true, SessionPatch{Password: &empty}); err != nil || n != 4 {
		t.Fatalf("global: %d %v", n, err)
	}
	if get(b.ID).HasPassword {
		t.Error("global password removal missed b")
	}
	bad := "key" // key auth without key path is invalid → nothing may change
	if _, err := st.PatchSessions("", true, SessionPatch{Auth: &bad, Color: &empty}); err == nil {
		t.Error("invalid patch accepted")
	}
	if get(a.ID).Color != color {
		t.Error("a failed patch changed data")
	}
	if _, err := st.PatchSessions("", true, SessionPatch{}); err == nil {
		t.Error("empty patch accepted")
	}
}

func TestNewerSessionFileIsNotOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := os.WriteFile(path, []byte(`{"version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, xorProtector{}); err == nil {
		t.Fatal("a newer file must be refused, saving would lose its data")
	}
}

func TestDamagedSettingsFallBackToBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	good := DefaultSettings()
	good.FontSize = 20
	if _, err := SaveSettings(path, good); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveSettings(path, DefaultSettings()); err != nil { // good is now in .bak
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{kaputt"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(path)
	if err != nil || got.FontSize != 20 {
		t.Fatalf("got %d, %v", got.FontSize, err)
	}
}

func TestKeepAliveIsCapped(t *testing.T) {
	if got := (Session{KeepAlive: 1 << 40}).Normalize().KeepAlive; got != maxKeepAlive {
		t.Errorf("huge: %d", got)
	}
}
