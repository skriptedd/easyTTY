package rdp

import (
	"strings"
	"testing"

	"easytty/internal/store"
)

func TestFileFullScreen(t *testing.T) {
	s := store.Session{Protocol: store.ProtocolRDP, Host: "win-srv01", User: `CORP\admin`, RdpDrives: true}.Normalize()
	f := File(s)
	for _, want := range []string{
		"full address:s:win-srv01\r\n", `username:s:CORP\admin` + "\r\n", "screen mode id:i:2\r\n",
		"drivestoredirect:s:*\r\n",
	} {
		if !strings.Contains(f, want) {
			t.Errorf("missing %q in\n%s", want, f)
		}
	}
	if strings.Contains(strings.ToLower(f), "password") {
		t.Error("an .rdp file must never contain a password")
	}
}

func TestFilePortAndOptionalLines(t *testing.T) {
	s := store.Session{Protocol: store.ProtocolRDP, Host: "10.0.0.5", Port: 3390}.Normalize()
	f := File(s)
	if !strings.Contains(f, "full address:s:10.0.0.5:3390\r\n") {
		t.Errorf("missing address in\n%s", f)
	}
	if strings.Contains(f, "username:") || strings.Contains(f, "drivestoredirect") {
		t.Errorf("unexpected lines:\n%s", f)
	}
	v6 := File(store.Session{Protocol: store.ProtocolRDP, Host: "fe80::1", Port: 3390}.Normalize())
	if !strings.Contains(v6, "full address:s:[fe80::1]:3390\r\n") {
		t.Errorf("ipv6: %s", v6)
	}
}

func TestFileClipboardSwitch(t *testing.T) {
	for _, tc := range []struct {
		off  bool
		want string
	}{{false, "redirectclipboard:i:1\r\n"}, {true, "redirectclipboard:i:0\r\n"}} {
		s := store.Session{Protocol: store.ProtocolRDP, Host: "srv", RdpNoClipboard: tc.off}.Normalize()

		if f := File(s); !strings.Contains(f, tc.want) {
			t.Errorf("RdpNoClipboard=%v: missing %q in\n%s", tc.off, tc.want, f)
		}
	}
}
