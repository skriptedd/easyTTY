package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// slowDevice mimics a Cisco console: a small input buffer that overflows
// (bytes are lost) and a parser that needs time per line before echoing it.
type slowDevice struct {
	r        *io.PipeReader
	w        *io.PipeWriter
	mu       sync.Mutex
	pending  []byte
	capacity int
	received []string
	dropped  int
	sig      chan struct{}
	stop     chan struct{}
}

func newSlowDevice(capacity int, perLine time.Duration) *slowDevice {
	r, w := io.Pipe()
	d := &slowDevice{r: r, w: w, capacity: capacity, sig: make(chan struct{}, 1), stop: make(chan struct{})}
	go d.run(perLine)
	return d
}

func (d *slowDevice) Read(p []byte) (int, error) { return d.r.Read(p) }
func (d *slowDevice) Write(p []byte) (int, error) {
	d.mu.Lock()
	for _, b := range p {
		if len(d.pending) >= d.capacity {
			d.dropped++
			continue
		}
		d.pending = append(d.pending, b)
	}
	d.mu.Unlock()
	select {
	case d.sig <- struct{}{}:
	default:
	}
	return len(p), nil
}
func (d *slowDevice) Close() error {
	select {
	case <-d.stop:
	default:
		close(d.stop)
	}
	return d.w.Close()
}
func (d *slowDevice) Resize(int, int) error { return nil }

func (d *slowDevice) run(perLine time.Duration) {
	for {
		select {
		case <-d.stop:
			return
		case <-d.sig:
		case <-time.After(5 * time.Millisecond):
		}
		for {
			d.mu.Lock()
			i := strings.IndexByte(string(d.pending), '\r')
			if i < 0 {
				d.mu.Unlock()
				break
			}
			line := string(d.pending[:i])
			d.pending = d.pending[i+1:]
			d.mu.Unlock()
			time.Sleep(perLine) // parsing the command
			d.mu.Lock()
			d.received = append(d.received, line)
			d.mu.Unlock()
			if _, err := d.w.Write([]byte(line + "\r\nsw1(config)#")); err != nil {
				return
			}
		}
	}
}

func runningConfig(n int) string {
	var sb strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&sb, "interface GigabitEthernet1/0/%d\r\n description Uplink zu Verteiler %d\r\n switchport mode access\r\n!\r\n", i, i)
	}
	return sb.String()
}

func TestPasteDeliversRunningConfigIntact(t *testing.T) {
	cfg := runningConfig(40) // 160 lines, ~7 KB
	lines := SplitPasteLines(cfg)

	// Sanity check: sending everything at once overruns the device (the PuTTY problem).
	naive := newSlowDevice(256, time.Millisecond)
	_, _ = naive.Write([]byte(strings.ReplaceAll(cfg, "\r\n", "\r")))
	if naive.dropped == 0 {
		t.Fatal("fake device should drop data on a burst")
	}
	naive.Close()

	rec := &recorder{}
	m := NewManager(rec.emit)
	dev := newSlowDevice(256, time.Millisecond)
	m.Attach("sw", dev, nil)
	defer m.CloseAll()

	var progress []PasteProgress
	err := m.Paste(context.Background(), "sw", cfg, PasteOptions{}, func(p PasteProgress) { progress = append(progress, p) })
	if err != nil {
		t.Fatal(err)
	}
	dev.mu.Lock()
	defer dev.mu.Unlock()
	if dev.dropped != 0 {
		t.Fatalf("paced paste lost %d bytes", dev.dropped)
	}
	if len(dev.received) != len(lines) {
		t.Fatalf("got %d lines, want %d", len(dev.received), len(lines))
	}
	for i := range lines {
		if dev.received[i] != lines[i] {
			t.Fatalf("line %d: got %q want %q", i, dev.received[i], lines[i])
		}
	}
	final := progress[len(progress)-1]
	if final.State != "done" || final.Done != len(lines) || final.Total != len(lines) || final.Timeouts != 0 {
		t.Fatalf("bad final progress: %+v", final)
	}
}

func TestPasteWithoutEchoFallsBackToPacing(t *testing.T) {
	m := NewManager((&recorder{}).emit)
	fc := newFake() // swallows input, never echoes
	m.Attach("t", fc, nil)
	defer m.CloseAll()
	var final PasteProgress
	start := time.Now()
	err := m.Paste(context.Background(), "t", "a\nb\nc\nd\ne\n", PasteOptions{EchoTimeout: 40 * time.Millisecond, LineDelay: time.Millisecond},
		func(p PasteProgress) { final = p })
	if err != nil {
		t.Fatal(err)
	}
	if final.State != "done" || final.Timeouts != 5 || fc.in.String() != "a\rb\rc\rd\re\r" {
		t.Fatalf("progress %+v, sent %q", final, fc.in.String())
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("no-echo paste took too long")
	}
}

func TestPasteCancelBusyAndErrors(t *testing.T) {
	m := NewManager((&recorder{}).emit)
	fc := newFake()
	m.Attach("t", fc, nil)
	defer m.CloseAll()

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	result := make(chan PasteProgress, 1)
	go func() {
		var last PasteProgress
		_ = m.Paste(ctx, "t", strings.Repeat("x\n", 50), PasteOptions{EchoTimeout: time.Second}, func(p PasteProgress) {
			last = p
			if p.State == "running" && p.Done == 0 {
				close(started)
			}
		})
		result <- last
	}()
	<-started
	if err := m.Paste(context.Background(), "t", "y", PasteOptions{}, func(PasteProgress) {}); !errors.Is(err, ErrPasteBusy) {
		t.Fatalf("want ErrPasteBusy, got %v", err)
	}
	cancel()
	select {
	case p := <-result:
		if p.State != "cancelled" || p.Done >= 50 {
			t.Fatalf("got %+v", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not stop the paste")
	}
	if err := m.Paste(context.Background(), "nope", "x", PasteOptions{}, func(PasteProgress) {}); !errors.Is(err, ErrNoTab) {
		t.Fatal("want ErrNoTab")
	}

	// Connection drops mid-paste.
	fc2 := newFake()
	m.Attach("u", fc2, nil)
	go func() { time.Sleep(50 * time.Millisecond); fc2.Close() }()
	var last PasteProgress
	_ = m.Paste(context.Background(), "u", strings.Repeat("z\n", 20), PasteOptions{EchoTimeout: time.Second}, func(p PasteProgress) { last = p })
	if last.State != "error" {
		t.Fatalf("expected error state after disconnect, got %+v", last)
	}
}

func TestSplitPasteLinesAndEchoTail(t *testing.T) {
	got := SplitPasteLines("a\r\nb\rc\n\nd\n")
	want := []string{"a", "b", "c", "", "d"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
	if SplitPasteLines("") != nil || SplitPasteLines("\n") != nil {
		t.Fatal("empty paste must yield no lines")
	}
	tails := map[string]string{
		"interface Gi1/0/1": "/0/1",
		" description x   ": "on x",
		"!":                 "!",
		"   ":               "",
		"ab\tc":             "abc",
		"exit-äöü":          "-äöü",
	}
	for in, want := range tails {
		if got := echoTail(in); got != want {
			t.Errorf("echoTail(%q) = %q, want %q", in, got, want)
		}
	}
}

type noteLog struct {
	mu    sync.Mutex
	notes []string
}

func (n *noteLog) Write(p []byte) (int, error) { return len(p), nil }
func (n *noteLog) Close() error                { return nil }
func (n *noteLog) Note(s string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.notes = append(n.notes, s)
}

func TestPasteAbortIsLoggedWithLine(t *testing.T) {
	m := NewManager((&recorder{}).emit)
	fc := newFake() // never echoes: each line waits for the timeout
	logs := &noteLog{}
	m.Attach("t", fc, logs)
	defer m.CloseAll()

	ctx, cancel := context.WithCancel(context.Background())
	var last PasteProgress
	go func() { time.Sleep(120 * time.Millisecond); cancel() }()
	_ = m.Paste(ctx, "t", "interface Gi1/0/1\n description up\n shutdown\n", PasteOptions{EchoTimeout: 100 * time.Millisecond},
		func(p PasteProgress) { last = p })

	if last.State != "cancelled" || last.StopLine < 2 || last.StopText == "" || len(last.Misses) == 0 {
		t.Fatalf("progress should name the stop line and misses: %+v", last)
	}
	logs.mu.Lock()
	defer logs.mu.Unlock()
	if len(logs.notes) != 2 || !strings.Contains(logs.notes[0], "gestartet: 3 Zeilen") {
		t.Fatalf("notes: %q", logs.notes)
	}
	end := logs.notes[1]
	for _, want := range []string{"ABGEBROCHEN", fmt.Sprintf("ab Zeile %d", last.StopLine), last.StopText, "ohne Echo"} {
		if !strings.Contains(end, want) {
			t.Errorf("log line misses %q: %s", want, end)
		}
	}
}

func TestSessionLogNote(t *testing.T) {
	out := &bufCloser{}
	l, _ := NewSessionLog(out, LogInfo{}, false, fakeClock())
	_, _ = l.Write([]byte("partial"))
	l.Note("Einfügen fertig")
	_, _ = l.Write([]byte(" line\r\n"))
	_ = l.Close()
	l.Note("after close is ignored")
	got := out.String()
	if !strings.Contains(got, ">>> easyTTY: Einfügen fertig") || !strings.Contains(got, "] partial line\r\n") || strings.Contains(got, "after close") {
		t.Fatalf("got:\n%s", got)
	}
}
