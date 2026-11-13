package securecrt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easytty/internal/store"
)

const iniSSH = "\xef\xbb\xbfS:\"Protocol Name\"=SSH2\r\n" +
	"S:\"Hostname\"=10.0.0.1\r\n" +
	"S:\"Username\"=admin\r\n" +
	"D:\"[SSH2] Port\"=00000016\r\n" +
	"S:\"Password V2\"=02:abcdef\r\n" +
	"B:\"Keyword Set\"=00000004\r\n" +
	" 00 01 02 03\r\n" +
	"S:\"Identity Filename V2\"=\r\n"

const iniSerial = "S:\"Protocol Name\"=Serial\r\n" +
	"S:\"Com Port\"=COM6\r\n" +
	"D:\"Baud Rate\"=0001c200\r\n" +
	"D:\"Data Bits\"=00000008\r\n" +
	"D:\"Parity\"=00000002\r\n" +
	"D:\"Stop Bits\"=00000002\r\n"

const iniTelnet = "S:\"Protocol Name\"=Telnet\r\nS:\"Hostname\"=old-router\r\n"

const iniRDP = "S:\"Protocol Name\"=RDP\r\nS:\"Hostname\"=win-srv01\r\nS:\"Username\"=CORP\\admin\r\n"

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSessionFolder(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Default.ini", iniSSH)
	write(t, dir, "__FolderData__.ini", "S:\"Folder List\"=RZ\r\n")
	write(t, dir, "RZ/core-sw1.ini", iniSSH)
	write(t, dir, "RZ/Nord/console.ini", iniSerial)
	write(t, dir, "RZ/Nord/__FolderData__.ini", "")
	write(t, dir, "Alt/telnet-router.ini", iniTelnet)
	write(t, dir, "Windows/srv01.ini", iniRDP)

	r, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Sessions) != 3 || len(r.Skipped) != 1 || !strings.Contains(r.Skipped[0], "Telnet") {
		t.Fatalf("sessions %+v skipped %v", r.Sessions, r.Skipped)
	}
	byName := map[string]store.Session{}
	for _, s := range r.Sessions {
		byName[s.Name] = s
	}
	ssh := byName["core-sw1"]
	if ssh.Protocol != store.ProtocolSSH || ssh.Host != "10.0.0.1" || ssh.User != "admin" || ssh.Port != 22 || ssh.KeyPath != "" {
		t.Errorf("ssh: %+v", ssh)
	}
	ser := byName["console"]
	if ser.Protocol != store.ProtocolSerial || ser.SerialPort != "COM6" || ser.Baud != 115200 || ser.Parity != "even" || ser.StopBits != "2" {
		t.Errorf("serial: %+v", ser)
	}
	if w := byName["srv01"]; w.Protocol != store.ProtocolRDP || w.Host != "win-srv01" || w.User != `CORP\admin` || w.Normalize().Port != 3389 {
		t.Errorf("rdp: %+v", w)
	}
	path := folderPath(r, ser.FolderID)
	if path != "RZ/Nord" || folderPath(r, ssh.FolderID) != "RZ" {
		t.Errorf("folders: %q %q (%+v)", path, folderPath(r, ssh.FolderID), r.Folders)
	}
	for _, s := range r.Sessions {
		if err := s.Normalize().Validate(); err != nil {
			t.Errorf("%s not valid for easyTTY: %v", s.Name, err)
		}
	}
}

func folderPath(r Result, id string) string {
	byID := map[string]store.Folder{}
	for _, f := range r.Folders {
		byID[f.ID] = f
	}
	var parts []string
	for f, ok := byID[id]; ok; f, ok = byID[f.ParentID] {
		parts = append([]string{f.Name}, parts...)
	}
	return strings.Join(parts, "/")
}

const xmlExport = `<?xml version="1.0" encoding="UTF-8"?>
<VanDyke version="3.0">
  <key name="Sessions">
    <key name="Default">
      <string name="Protocol Name">SSH2</string>
    </key>
    <key name="Kunden">
      <key name="Firma A">
        <key name="fw-01">
          <string name="Protocol Name">SSH2</string>
          <string name="Hostname">fw01.example.net</string>
          <string name="Username">root</string>
          <dword name="[SSH2] Port">2222</dword>
          <string name="Identity Filename V2">C:\keys\id_ed25519::rawkey</string>
        </key>
      </key>
    </key>
    <key name="lab-console">
      <string name="Protocol Name">Serial</string>
      <string name="Com Port">COM3</string>
      <dword name="Baud Rate">9600</dword>
      <dword name="Data Bits">8</dword>
      <dword name="Parity">0</dword>
      <dword name="Stop Bits">0</dword>
    </key>
  </key>
</VanDyke>`

func TestLoadXMLExport(t *testing.T) {
	p := filepath.Join(t.TempDir(), "SecureCRT.xml")
	if err := os.WriteFile(p, []byte(xmlExport), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Sessions) != 2 {
		t.Fatalf("sessions: %+v", r.Sessions)
	}
	fw := r.Sessions[0]
	if fw.Name != "fw-01" || fw.Port != 2222 || fw.User != "root" || fw.KeyPath != `C:\keys\id_ed25519` || fw.Auth != store.AuthKey {
		t.Errorf("fw: %+v", fw)
	}
	if folderPath(r, fw.FolderID) != "Kunden/Firma A" {
		t.Errorf("folder: %q", folderPath(r, fw.FolderID))
	}
	con := r.Sessions[1]
	if con.SerialPort != "COM3" || con.Baud != 9600 || con.Parity != "none" || con.StopBits != "1" || con.FolderID != "" {
		t.Errorf("console: %+v", con)
	}
}

func TestLoadRejectsOtherFiles(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.xml")
	_ = os.WriteFile(p, []byte(`<foo><bar/></foo>`), 0o644)
	if _, err := Load(p); err == nil {
		t.Error("non-SecureCRT XML accepted")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "fehlt")); err == nil {
		t.Error("missing path accepted")
	}
}

func TestNetworkKeyPathsAreDropped(t *testing.T) {
	for p, want := range map[string]bool{
		`\evil.example\share\key`:  true,
		`//evil.example/share/key`: true,
		`C:\keys\id_ed25519`:       false,
		`~/.ssh/id_ed25519`:        false,
	} {
		if got := isNetworkPath(p); got != want {
			t.Errorf("isNetworkPath(%q) = %v, want %v", p, got, want)
		}
	}
}
