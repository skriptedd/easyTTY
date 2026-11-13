package conn

import "testing"

func TestParseTarget(t *testing.T) {
	ok := map[string]Target{
		"router1":               {Host: "router1", Port: 22},
		"admin@10.0.0.1":        {User: "admin", Host: "10.0.0.1", Port: 22},
		"admin@10.0.0.1:2222":   {User: "admin", Host: "10.0.0.1", Port: 2222},
		"ssh://root@srv:23":     {User: "root", Host: "srv", Port: 23},
		"[::1]:2200":            {Host: "::1", Port: 2200},
		"user@[fe80::1]":        {User: "user", Host: "fe80::1", Port: 22},
		"fe80::1":               {Host: "fe80::1", Port: 22},
		"a@b@host":              {User: "a@b", Host: "host", Port: 22},
		"  admin@sw1 -p 8022  ": {User: "admin", Host: "sw1", Port: 8022},
	}
	for in, want := range ok {
		got, err := ParseTarget(in)
		if err != nil || got != want {
			t.Errorf("%q: got %+v, %v; want %+v", in, got, err, want)
		}
	}
	bad := []string{"", "user@", "host:0", "host:99999", "host:abc", "[::1", "[::1]x", "a b", "host -p x", "[::1]:"}
	for _, in := range bad {
		if _, err := ParseTarget(in); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
}

func TestSerialMode(t *testing.T) {
	m, err := SerialConfig{Port: "COM1", Baud: 9600, DataBits: 8, Parity: "even", StopBits: "2"}.SerialMode()
	if err != nil || m.BaudRate != 9600 || m.DataBits != 8 {
		t.Fatalf("got %+v %v", m, err)
	}
	bad := []SerialConfig{
		{Baud: 9600, DataBits: 8, Parity: "x", StopBits: "1"},
		{Baud: 9600, DataBits: 8, Parity: "none", StopBits: "3"},
		{Baud: 0, DataBits: 8, Parity: "none", StopBits: "1"},
		{Baud: 9600, DataBits: 4, Parity: "none", StopBits: "1"},
	}
	for _, c := range bad {
		if _, err := c.SerialMode(); err == nil {
			t.Errorf("%+v: expected error", c)
		}
	}
}

func TestExpandHome(t *testing.T) {
	if got := expandHome("~/.ssh/id", `C:\home`); got != `C:\home\.ssh\id` && got != "C:\\home/.ssh/id" {
		t.Errorf("got %q", got)
	}
	if got := expandHome(`D:\keys\id`, `C:\home`); got != `D:\keys\id` {
		t.Errorf("got %q", got)
	}
}
