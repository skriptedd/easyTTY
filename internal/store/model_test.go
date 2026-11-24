package store

import "testing"

func TestRDPSessionNormalizeValidate(t *testing.T) {
	s := Session{Protocol: ProtocolRDP, Host: " win-srv01 ", User: `CORP\admin`, Auth: AuthKey, KeyPath: "x", SerialPort: "COM1"}.Normalize()
	if s.Port != 3389 || s.Name != "win-srv01" || s.Auth != "" || s.KeyPath != "" || s.SerialPort != "" {
		t.Fatalf("normalize: %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Session{
		{Protocol: ProtocolRDP, Name: "x"},
		{Protocol: ProtocolRDP, Host: "a b"},
		{Protocol: ProtocolRDP, Host: "h", User: "a\nfull address:s:evil"},
	} {
		if err := bad.Normalize().Validate(); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	ssh := Session{Host: "h", RdpDrives: true}.Normalize()
	if ssh.RdpDrives {
		t.Errorf("rdp fields left on ssh: %+v", ssh)
	}
}

func TestLocalSessionNormalizeValidate(t *testing.T) {
	s := Session{Protocol: ProtocolLocal, Host: "h", Port: 22, User: "u", JumpID: "j", SerialPort: "COM1", StartDir: ` C:\Work `}.Normalize()
	if s.Shell != "powershell" || s.Name != "PowerShell" || s.Host != "" || s.Port != 0 || s.User != "" || s.JumpID != "" || s.SerialPort != "" || s.StartDir != `C:\Work` {
		t.Fatalf("normalize: %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if n := (Session{Protocol: ProtocolLocal, Shell: "cmd"}).Normalize().Name; n != "Eingabeaufforderung" {
		t.Errorf("cmd name = %q", n)
	}
	for _, bad := range []Session{
		{Protocol: ProtocolLocal, Shell: "bash"},
		{Protocol: ProtocolLocal, Shell: "pwsh", StartDir: "C:\\x\" -Command evil"},
	} {
		if err := bad.Normalize().Validate(); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if ssh := (Session{Host: "h", Shell: "cmd", StartDir: "C:\\"}).Normalize(); ssh.Shell != "" || ssh.StartDir != "" {
		t.Errorf("local fields left on ssh: %+v", ssh)
	}
}

func TestNormalizeDropsRdpSwitchesOutsideRdp(t *testing.T) {
	for _, p := range []string{ProtocolSSH, ProtocolSerial} {
		s := Session{Protocol: p, Host: "h", SerialPort: "COM1", RdpDrives: true, RdpNoClipboard: true}.Normalize()
		if s.RdpDrives || s.RdpNoClipboard {
			t.Errorf("%s keeps RDP switches: %+v", p, s)
		}
	}
	if s := (Session{Protocol: ProtocolRDP, Host: "h", RdpNoClipboard: true}).Normalize(); !s.RdpNoClipboard {
		t.Error("RDP session lost its clipboard switch")
	}
}
