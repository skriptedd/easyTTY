package terminal

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

type bufCloser struct {
	bytes.Buffer
	closed bool
	failAt int // fail writes once the buffer is this long (0 = never, -1 = always)
}

func (b *bufCloser) Write(p []byte) (int, error) {
	if b.failAt < 0 || b.failAt > 0 && b.Len() >= b.failAt {
		return 0, errors.New("disk full")
	}
	return b.Buffer.Write(p)
}
func (b *bufCloser) Close() error { b.closed = true; return nil }

func fakeClock() func() time.Time {
	t := time.Date(2026, 10, 30, 14, 5, 0, 0, time.Local)
	return func() time.Time { t = t.Add(time.Second); return t }
}

func TestSessionLogPlainTextWithTimestamps(t *testing.T) {
	out := &bufCloser{}
	l, err := NewSessionLog(out, LogInfo{Session: "core-sw1", Target: "admin@10.0.0.1:22"}, false, fakeClock())
	if err != nil {
		t.Fatal(err)
	}
	// Colours, a window-title OSC, a charset switch, a typo fixed with backspace,
	// a progress line redrawn via \r, and a final prompt without newline.
	chunks := []string{
		"\x1b]0;admin@sw1\x07\x1b(B\x1b[1;32mcore-sw1#\x1b[0m sh",
		"ow verx\bsion\r\n",
		"Cisco IOS XE\r\n",
		"10%\r50%\r100%\r\n",
		"\x07ünïcødé ok\r\n",
		"core-sw1#",
	}
	for _, c := range chunks {
		if n, err := l.Write([]byte(c)); err != nil || n != len(c) {
			t.Fatalf("write: %d %v", n, err)
		}
	}
	if err := l.Close(); err != nil || !out.closed {
		t.Fatal("close failed")
	}
	_ = l.Close() // idempotent

	got := out.String()
	for _, want := range []string{
		"===== easyTTY · core-sw1 · admin@10.0.0.1:22 · Start 2026-10-30 14:05:01 =====",
		"] core-sw1# show version\r\n",
		"] Cisco IOS XE\r\n",
		"] 100%\r\n",
		"] ünïcødé ok\r\n",
		"] core-sw1#\r\n",
		"===== Ende ",
		"Dauer ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.ContainsAny(got, "\x1b\x07\b") || strings.Contains(got, "10%") {
		t.Errorf("control sequences or overwritten text leaked:\n%q", got)
	}
	if !strings.Contains(got, "[2026-10-30 14:05:") {
		t.Errorf("timestamp missing:\n%s", got)
	}
}

func TestSessionLogStampsWhenLineCompletes(t *testing.T) {
	now := time.Date(2026, 10, 30, 9, 0, 0, 0, time.Local)
	out := &bufCloser{}
	l, _ := NewSessionLog(out, LogInfo{}, false, func() time.Time { return now })
	_, _ = l.Write([]byte("sw1>")) // prompt appears at 09:00:00
	now = now.Add(30 * time.Second)
	_, _ = l.Write([]byte("show version\r\n")) // Enter at 09:00:30
	_ = l.Close()
	if !strings.Contains(out.String(), "[2026-10-30 09:00:30] sw1>show version") {
		t.Fatalf("command must carry the Enter time:\n%s", out.String())
	}
}

func TestSessionLogRawKeepsBytes(t *testing.T) {
	out := &bufCloser{}
	l, _ := NewSessionLog(out, LogInfo{Session: "s", Target: "t"}, true, fakeClock())
	_, _ = l.Write([]byte("\x1b[31mred\x1b[0m\r\n"))
	_ = l.Close()
	if !strings.Contains(out.String(), "\x1b[31mred\x1b[0m\r\n") {
		t.Fatalf("raw bytes changed: %q", out.String())
	}
}

func TestSessionLogWriteErrorSticks(t *testing.T) {
	out := &bufCloser{failAt: 1}
	l, err := NewSessionLog(out, LogInfo{}, false, fakeClock())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Write([]byte("line\n")); err == nil {
		t.Fatal("expected write error")
	}
	if _, err := l.Write([]byte("more")); err == nil {
		t.Fatal("error must stick")
	}
	_ = l.Close()
	if !out.closed {
		t.Fatal("file must still be closed")
	}
	rawOut := &bufCloser{failAt: 1}
	rl, _ := NewSessionLog(rawOut, LogInfo{}, true, nil)
	if _, err := rl.Write([]byte("x")); err == nil {
		t.Fatal("raw write error expected")
	}
	if _, err := NewSessionLog(&bufCloser{failAt: -1}, LogInfo{}, false, nil); err == nil {
		t.Fatal("header write error expected")
	}
}

func TestSafeNameForWindowsPaths(t *testing.T) {
	for in, want := range map[string]string{
		"core-sw1": "core-sw1", "CON": "_CON", "nul.txt": "_nul.txt", "a/b": "a_b", "name. ": "name", "...": "_", "COM10": "COM10",
	} {
		if got := safeName(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestLogSplitsEndlessLinesAndHidesControlStrings(t *testing.T) {
	b := &bufCloser{}
	l, err := NewSessionLog(b, LogInfo{Session: "evil\r\nname", Target: "t"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.SplitN(b.String(), "\r\n", 2)[0], "evil\r") || strings.Count(b.String(), "\n") != 1 {
		t.Fatalf("header is more than one line: %q", b.String())
	}
	l.Write(bytes.Repeat([]byte("x"), 3*maxLogLine))
	if got := len(l.line); got >= maxLogLine {
		t.Errorf("line buffer grew to %d", got)
	}
	l.Write([]byte("a\x1bPgeheim\x1b\\b\n"))
	l.Close()
	if strings.Contains(b.String(), "geheim") {
		t.Error("DCS payload landed in the log")
	}
}
