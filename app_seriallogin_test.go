package main

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"easytty/internal/store"
)

func TestSerialLoginPrompts(t *testing.T) {
	users := []string{"Username:", "Username: ", "User Name:", "login:", "switch login: ", "LOGIN:"}
	for _, u := range users {
		if !userPrompt.MatchString(u) {
			t.Errorf("user prompt %q not recognised", u)
		}
	}
	for _, p := range []string{"Password:", "Password: ", "password:"} {
		if !passPrompt.MatchString(p) {
			t.Errorf("password prompt %q not recognised", p)
		}
	}
	for _, no := range []string{"LAB-2960CX-8TC-L>", "Last login: Mon Nov  2", "% Login invalid", "Password changed successfully", "sw1#"} {
		if userPrompt.MatchString(no) || passPrompt.MatchString(no) {
			t.Errorf("%q must not count as a prompt", no)
		}
	}
}

// fakeConsole simulates a Cisco console line: "local" asks user + password,
// "line" asks only a password, "none" has no login. After login: "sw1>".
type fakeConsole struct {
	toDev   *io.PipeWriter
	fromDev *io.PipeReader
	in      *io.PipeReader
	out     *io.PipeWriter

	mu    sync.Mutex
	lines []string // what the "user" typed, line by line
}

func newFakeConsole(mode, user, pw string) *fakeConsole {
	c := &fakeConsole{}
	c.in, c.toDev = io.Pipe()
	c.fromDev, c.out = io.Pipe()
	go c.serve(mode, user, pw)
	return c
}

func (c *fakeConsole) Read(p []byte) (int, error)  { return c.fromDev.Read(p) }
func (c *fakeConsole) Write(p []byte) (int, error) { return c.toDev.Write(p) }
func (c *fakeConsole) Close()                      { c.toDev.Close(); c.out.Close() }

func (c *fakeConsole) readLine() (string, bool) {
	var line []byte
	b := make([]byte, 1)
	for {
		if _, err := c.in.Read(b); err != nil {
			return "", false
		}
		if b[0] == '\r' {
			c.mu.Lock()
			c.lines = append(c.lines, string(line))
			c.mu.Unlock()
			return string(line), true
		}
		line = append(line, b[0])
	}
}

func (c *fakeConsole) typed() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.lines...)
}

func (c *fakeConsole) serve(mode, user, pw string) {
	say := func(s string) { _, _ = io.WriteString(c.out, s) }
	if _, ok := c.readLine(); !ok { // wake-up Enter
		return
	}
	if mode == "none" {
		say("\r\nsw1>")
		return
	}
	say("\r\n\r\nUser Access Verification\r\n\r\n")
	for {
		u := ""
		if mode == "local" {
			say("Username: ")
			var ok bool
			if u, ok = c.readLine(); !ok {
				return
			}
			say("\r\n")
		}
		say("Password: ")
		p, ok := c.readLine()
		if !ok {
			return
		}
		if (mode == "line" || u == user) && p == pw {
			say("\r\n\r\nsw1>")
			c.readLine() // keep the line open
			return
		}
		say("\r\n% Login invalid\r\n\r\n")
	}
}

func runLogin(t *testing.T, c *fakeConsole, user, pw string, window time.Duration) loginResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), window)
	defer cancel()
	return consoleLogin(ctx, newChanConn(closer{c}), user, pw, true)
}

func TestConsoleLoginLocal(t *testing.T) {
	c := newFakeConsole("local", "admin", "geheim")
	defer c.Close()
	if r := runLogin(t, c, "admin", "geheim", 5*time.Second); r != loginOK {
		t.Fatalf("result %v", r)
	}
	if got := strings.Join(c.typed(), "|"); got != "|admin|geheim" {
		t.Errorf("typed %q", got)
	}
}

func TestConsoleLoginRejectedTypesPasswordOnce(t *testing.T) {
	c := newFakeConsole("local", "admin", "geheim")
	defer c.Close()
	if r := runLogin(t, c, "admin", "falsch", 5*time.Second); r != loginRejected {
		t.Fatalf("result %v", r)
	}
	if got := strings.Join(c.typed(), "|"); got != "|admin|falsch" {
		t.Errorf("password must be sent once only, typed %q", got)
	}
}

func TestConsoleLoginLinePasswordWithStoredUser(t *testing.T) {
	c := newFakeConsole("line", "", "cisco")
	defer c.Close()
	if r := runLogin(t, c, "admin", "cisco", 5*time.Second); r != loginOK {
		t.Fatalf("result %v", r)
	}
	if got := strings.Join(c.typed(), "|"); got != "|cisco" {
		t.Errorf("typed %q (no user name expected)", got)
	}
}

func TestConsoleLoginNoLoginConsole(t *testing.T) {
	c := newFakeConsole("none", "", "")
	defer c.Close()
	if r := runLogin(t, c, "admin", "geheim", 5*time.Second); r != loginNone {
		t.Fatalf("result %v", r)
	}
	if got := strings.Join(c.typed(), "|"); got != "" {
		t.Errorf("nothing but the wake-up Enter may be typed, got %q", got)
	}
}

type closer struct{ *fakeConsole }

func (c closer) Close() error { c.fakeConsole.Close(); return nil }

// After the login the commands must see the console output (no second reader).
func TestLoginThenCommandsShareReader(t *testing.T) {
	c := newFakeConsole("local", "admin", "geheim")
	defer c.Close()
	conn, err := loginIfNeeded(context.Background(), closer{c}, store.Session{Name: "sw", Protocol: store.ProtocolSerial, User: "admin"}, "geheim")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("show clock\r")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50 && len(c.typed()) < 4; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if got := strings.Join(c.typed(), "|"); !strings.HasSuffix(got, "admin|geheim|show clock") {
		t.Errorf("typed %q", got)
	}
	if _, err := loginIfNeeded(context.Background(), closer{newFakeConsole("local", "admin", "geheim")}, store.Session{Name: "sw", Protocol: store.ProtocolSerial, User: "admin"}, "falsch"); err == nil {
		t.Error("wrong password must fail the run")
	}
}
