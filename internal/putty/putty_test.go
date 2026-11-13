package putty

import (
	"strings"
	"testing"

	"easytty/internal/store"
)

func TestConvertSSH(t *testing.T) {
	s, ok := Convert("Core%20Switch%2F1", Values{
		"HostName": "admin@10.1.1.1", "PortNumber": uint64(2222), "Protocol": "ssh",
		"PublicKeyFile": `C:\keys\id_ed25519`, "PingIntervalSecs": uint64(30),
	})
	if !ok || s.Name != "Core Switch/1" || s.Host != "10.1.1.1" || s.User != "admin" || s.Port != 2222 ||
		s.Auth != store.AuthKey || s.KeyPath != `C:\keys\id_ed25519` || s.KeepAlive != 30 {
		t.Fatalf("got %+v", s)
	}
	s, _ = Convert("x", Values{"HostName": "h", "UserName": "root", "PublicKeyFile": `C:\k.PPK`})
	if s.User != "root" || s.Auth != store.AuthAuto || !strings.Contains(s.Notes, ".ppk") {
		t.Fatalf("ppk handling: %+v", s)
	}
}

func TestConvertSerial(t *testing.T) {
	s, ok := Convert("console", Values{
		"Protocol": "serial", "SerialLine": "COM4", "SerialSpeed": uint64(115200),
		"SerialDataBits": uint64(8), "SerialParity": uint64(2), "SerialStopHalfbits": uint64(4),
	})
	if !ok || s.Protocol != store.ProtocolSerial || s.SerialPort != "COM4" || s.Baud != 115200 || s.Parity != "even" || s.StopBits != "2" {
		t.Fatalf("got %+v", s)
	}
}

func TestConvertSkips(t *testing.T) {
	for name, v := range map[string]Values{
		"Default%20Settings": {"HostName": "h"},
		"telnet":             {"Protocol": "telnet", "HostName": "h"},
		"nohost":             {"Protocol": "ssh"},
	} {
		if _, ok := Convert(name, v); ok {
			t.Errorf("%s should be skipped", name)
		}
	}
	if DecodeName("bad%zz") != "bad%zz" {
		t.Error("invalid escapes should be kept")
	}
}

func TestLoadSessionsDoesNotFail(t *testing.T) {
	if _, _, err := LoadSessions(); err != nil {
		t.Fatal(err)
	}
}
