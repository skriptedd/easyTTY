package store

import "testing"

func TestPatchUserKeepsConsoleLine(t *testing.T) {
	cases := []struct {
		name, protocol, old, patch, want string
	}{
		{"line stays", ProtocolSSH, "ops:6", "admin", "admin:6"},
		{"empty protocol is SSH", "", "ops:6", "admin", "admin:6"},
		{"named port stays", ProtocolSSH, "ops:port12", "admin", "admin:port12"},
		{"no line", ProtocolSSH, "ops", "admin", "admin"},
		{"new line wins", ProtocolSSH, "ops:6", "admin:7", "admin:7"},
		{"empty clears the login", ProtocolSSH, "ops:6", "", ""},
		{"rdp is left alone", ProtocolRDP, "ops:6", "admin", "admin"},
		{"serial is left alone", ProtocolSerial, "ops:6", "admin", "admin"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Arrange
			s := Session{Protocol: c.protocol, User: c.old}
			user := c.patch

			// Act
			got := SessionPatch{User: &user}.apply(s).User

			// Assert
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
