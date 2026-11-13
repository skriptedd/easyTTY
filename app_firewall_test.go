package main

import (
	"strings"
	"testing"
)

func TestJudgeFirewall(t *testing.T) {
	cases := []struct{ out, level, want string }{
		{"Allow|Private|TCP\r\nAllow|Private|UDP\r\nNET|Private\r\n", "ok", "TCP und UDP"},
		{"Allow|Private|TCP\nAllow|Private|UDP\nNET|Public\n", "warn", "anderes Netzprofil"},
		{"Allow|Any|Any\nNET|Public\n", "ok", "Public"},
		{"Allow|Domain, Private|TCP\nNET|Private\n", "warn", "UDP"},
		{"Block|Private|UDP\nAllow|Private|TCP\nNET|Private\n", "warn", "BLOCKIERT"},
		{"NET|Private\n", "warn", "keine Freigabe"},
	}
	for _, c := range cases {
		got := judgeFirewall(c.out)
		if got.Level != c.level || !strings.Contains(got.Message, c.want) {
			t.Errorf("%q → %+v", c.out, got)
		}
	}
}
