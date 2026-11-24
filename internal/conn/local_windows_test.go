//go:build windows

package conn

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// A real cmd.exe in a pseudo console: the typed command's output arrives,
// and "exit" ends the stream so the tab notices.
func TestOpenLocalRunsShell(t *testing.T) {
	c, err := OpenLocal(LocalConfig{Shell: ShellCmd, Home: t.TempDir(), Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, c)
		got <- buf.String()
	}()
	if err := c.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	// The %…% part vanishes in the output, so only the echo itself matches.
	if _, err := c.Write([]byte("echo easytty-%COMPUTERNAME:~0,0%ok\r")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := c.Write([]byte("exit\r")); err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-got:
		if !strings.Contains(out, "easytty-ok") {
			t.Errorf("output lacks the echo: %q", out)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("stream did not end after exit")
	}
}

func TestCloseEndsShell(t *testing.T) {
	c, err := OpenLocal(LocalConfig{Shell: ShellCmd, Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	select {
	case <-c.exited:
	case <-time.After(10 * time.Second):
		t.Fatal("shell still running after Close")
	}
}
