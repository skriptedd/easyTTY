package terminal

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeConn struct {
	r       *io.PipeReader
	w       *io.PipeWriter
	mu      sync.Mutex
	in      bytes.Buffer
	size    [2]int
	breaks  int
	closed  bool
	noBreak bool
}

func newFake() *fakeConn {
	r, w := io.Pipe()
	return &fakeConn{r: r, w: w}
}

func (f *fakeConn) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *fakeConn) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.in.Write(p)
}
func (f *fakeConn) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	f.w.CloseWithError(io.EOF)
	return nil
}
func (f *fakeConn) Resize(c, r int) error { f.size = [2]int{c, r}; return nil }

type breakConn struct{ *fakeConn }

func (b breakConn) SendBreak() error { b.breaks++; return nil }

type recorder struct {
	mu     sync.Mutex
	data   bytes.Buffer
	events int
	states []State
}

func (r *recorder) emit(event string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if event == EventState {
		r.states = append(r.states, payload.(State))
		return
	}
	if event == EventNotify {
		return
	}
	raw, _ := base64.StdEncoding.DecodeString(payload.(string))
	r.data.Write(raw)
	r.events++
}

func (r *recorder) waitState(t *testing.T, state string) State {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		for _, s := range r.states {
			if s.State == state {
				r.mu.Unlock()
				return s
			}
		}
		r.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("state %q not reached", state)
	return State{}
}

type closeRecorder struct{ closed bool }

func (c *closeRecorder) Close() error { c.closed = true; return nil }

func TestManagerStreamsBatchesAndCloses(t *testing.T) {
	rec := &recorder{}
	m := NewManager(rec.emit)
	fc := newFake()
	logPath := filepath.Join(t.TempDir(), "log.txt")
	lf, err := OpenLog(logPath)
	if err != nil {
		t.Fatal(err)
	}
	m.Attach("t1", fc, lf)
	rec.waitState(t, "connected")

	extra := &closeRecorder{}
	if err := m.CloseWith("t1", extra); err != nil {
		t.Fatal(err)
	}
	for range 200 {
		fc.w.Write([]byte("x"))
	}
	if err := m.Write("t1", []byte("ls\n")); err != nil {
		t.Fatal(err)
	}
	if err := m.Resize("t1", 100, 40); err != nil || fc.size != [2]int{100, 40} {
		t.Fatalf("resize: %v %v", err, fc.size)
	}
	_ = m.Resize("t1", 0, 0) // ignored
	if err := m.Break("t1"); err == nil {
		t.Fatal("fake conn without break must error")
	}
	if c, err := m.Conn("t1"); err != nil || c != fc {
		t.Fatal("Conn lookup failed")
	}
	if m.Count() != 1 {
		t.Fatal("count")
	}

	fc.w.Close() // remote EOF
	st := rec.waitState(t, "closed")
	if st.Message != "Verbindung beendet" {
		t.Fatalf("message %q", st.Message)
	}
	rec.mu.Lock()
	got, events := rec.data.String(), rec.events
	rec.mu.Unlock()
	if got != strings.Repeat("x", 200) {
		t.Fatalf("data lost: %d bytes", len(got))
	}
	if events >= 200 {
		t.Fatalf("output was not batched (%d events)", events)
	}
	if fc.in.String() != "ls\n" || !fc.closed || !extra.closed {
		t.Fatal("input/close not handled")
	}
	logged, _ := os.ReadFile(logPath)
	if string(logged) != got {
		t.Fatal("log content mismatch")
	}
	if err := m.Write("t1", []byte("x")); !errors.Is(err, ErrNoTab) {
		t.Fatal("closed tab should be gone")
	}
	if m.Count() != 0 {
		t.Fatal("tab not removed")
	}
}

func TestManagerErrorMessageAndUnknownTab(t *testing.T) {
	rec := &recorder{}
	m := NewManager(rec.emit)
	fc := newFake()
	m.Attach("t", fc, nil)
	fc.w.CloseWithError(errors.New("connection reset"))
	if st := rec.waitState(t, "closed"); st.Message != "connection reset" {
		t.Fatalf("got %q", st.Message)
	}
	for _, err := range []error{
		m.Write("nope", nil), m.Resize("nope", 1, 1), m.Break("nope"), m.Close("nope"), m.CloseWith("nope", nil),
	} {
		if !errors.Is(err, ErrNoTab) {
			t.Fatalf("want ErrNoTab, got %v", err)
		}
	}
	if _, err := m.Conn("nope"); !errors.Is(err, ErrNoTab) {
		t.Fatal("want ErrNoTab")
	}
}

func TestManagerReattachAndCloseAll(t *testing.T) {
	rec := &recorder{}
	m := NewManager(rec.emit)
	first, second := newFake(), breakConn{newFake()}
	m.Attach("t", first, nil)
	m.Attach("t", second, nil)
	if !first.closed {
		t.Fatal("old connection should be closed on re-attach")
	}
	if err := m.Break("t"); err != nil {
		t.Fatal(err)
	}
	m.Attach("u", newFake(), nil)
	if err := m.Close("u"); err != nil {
		t.Fatal(err)
	}
	m.CloseAll()
	deadline := time.Now().Add(2 * time.Second)
	for m.Count() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if m.Count() != 0 {
		t.Fatal("tabs left after CloseAll")
	}
	rec.mu.Lock()
	closed := 0
	for _, s := range rec.states {
		if s.State == "closed" {
			closed++
		}
	}
	rec.mu.Unlock()
	if closed != 2 {
		t.Fatalf("expected 2 closed events (replaced tab is silent), got %d", closed)
	}
}

func (r *recorder) waitBytes(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		got := r.data.Len()
		r.mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("did not receive %d bytes", n)
}

func TestFlowControlPausesUntilAck(t *testing.T) {
	rec := &recorder{}
	m := NewManager(rec.emit)
	m.HighWater, m.AckTimeout = 10, time.Hour
	fc := newFake()
	m.Attach("t", fc, nil)

	fc.w.Write([]byte(strings.Repeat("a", 20)))
	rec.waitBytes(t, 20)
	fc.w.Write([]byte("b")) // pump was already blocked in Read
	rec.waitBytes(t, 21)

	done := make(chan struct{})
	go func() { fc.w.Write([]byte("c")); close(done) }()
	select {
	case <-done:
		t.Fatal("pump should pause while the UI is behind")
	case <-time.After(150 * time.Millisecond):
	}
	if err := m.Ack("t", 21); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pump did not resume after ack")
	}
	rec.waitBytes(t, 22)
	if err := m.Ack("nope", 1); !errors.Is(err, ErrNoTab) {
		t.Fatal("want ErrNoTab")
	}
	_ = m.Ack("t", 1000) // over-ack must not go negative
	m.CloseAll()
}

func TestFlowControlTimeoutNeverStalls(t *testing.T) {
	rec := &recorder{}
	m := NewManager(rec.emit)
	m.HighWater, m.AckTimeout = 5, 30*time.Millisecond
	fc := newFake()
	m.Attach("t", fc, nil)
	for range 5 {
		fc.w.Write([]byte("0123456789"))
	}
	rec.waitBytes(t, 50)
	m.CloseAll()
}

func TestCloseWithAfterShutdownFails(t *testing.T) {
	m := NewManager((&recorder{}).emit)
	fc := newFake()
	m.Attach("t", fc, nil)
	m.mu.Lock()
	tb := m.tabs["t"]
	m.mu.Unlock()
	tb.shutdown()
	if err := m.CloseWith("t", &closeRecorder{}); !errors.Is(err, ErrNoTab) {
		t.Fatalf("closer registered on a closed tab: %v", err)
	}
}

func TestExpandLogPath(t *testing.T) {
	now := time.Date(2026, 10, 29, 14, 5, 9, 0, time.UTC)
	got := ExpandLogPath(`C:\logs\{session}_{host}_{date}_{time}.log`, "core/sw1", "10.0.0.1", now)
	want := `C:\logs\core_sw1_10.0.0.1_2026-10-29_140509.log`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestOpenLogBadPath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	_ = os.WriteFile(file, nil, 0o600)
	if _, err := OpenLog(filepath.Join(file, "sub", "x.log")); err == nil {
		t.Fatal("expected error when parent is a file")
	}
}

func TestManagerTailKeepsRecentOutput(t *testing.T) {
	rec := &recorder{}
	m := NewManager(rec.emit)
	m.HighWater = 0
	fc := newFake()
	m.Attach("t1", fc, nil)
	rec.waitState(t, "connected")
	chunk := bytes.Repeat([]byte("a"), 10*1024)
	for range 20 {
		fc.w.Write(chunk)
	}
	fc.w.Write([]byte("ENDE"))
	var tail []byte
	for range 100 {
		tail, _ = m.Tail("t1")
		if bytes.HasSuffix(tail, []byte("ENDE")) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !bytes.HasSuffix(tail, []byte("ENDE")) || len(tail) > tailKeep {
		t.Fatalf("tail len %d", len(tail))
	}
	if _, err := m.Tail("nope"); err == nil {
		t.Error("unknown tab")
	}
}

func TestTapDrivesVisibleTab(t *testing.T) {
	rec := &recorder{}
	m := NewManager(rec.emit)
	m.HighWater = 0
	fc := newFake()
	m.Attach("t1", fc, nil)
	rec.waitState(t, "connected")
	s, err := m.Tap("t1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Tap("t1"); err != ErrTapBusy {
		t.Errorf("second tap: %v", err)
	}
	if _, err := s.Write([]byte("show clock\r")); err != nil {
		t.Fatal(err)
	}
	fc.mu.Lock()
	typed := fc.in.String()
	fc.mu.Unlock()
	if typed != "show clock\r" {
		t.Errorf("typed %q", typed)
	}
	go fc.w.Write([]byte("12:00 sw1#"))
	buf := make([]byte, 64)
	n, err := s.Read(buf)
	if err != nil || string(buf[:n]) != "12:00 sw1#" {
		t.Fatalf("read %q %v", buf[:n], err)
	}
	s.Close()
	if _, err := m.Conn("t1"); err != nil {
		t.Error("closing the stream must not close the tab")
	}
	s2, err := m.Tap("t1")
	if err != nil {
		t.Fatal("tap again:", err)
	}
	fc.w.Close() // connection ends → stream sees EOF
	if _, err := s2.Read(buf); err == nil {
		t.Error("expected EOF after the connection closed")
	}
}
