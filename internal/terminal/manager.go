// Package terminal pumps connection output to the UI and routes input back.
package terminal

import (
	"encoding/base64"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

const (
	readBufSize       = 64 * 1024
	batchDelay        = 4 * time.Millisecond
	batchMaxBytes     = 256 * 1024
	defaultHighWater  = 2 * 1024 * 1024
	defaultAckTimeout = 2 * time.Second
)

// Event names emitted to the UI.
const (
	EventState      = "term:state"
	EventDataPrefix = "term:data:"
	EventNotify     = "notify"
)

// Notice is the payload of EventNotify (shown as a toast).
type Notice struct {
	Kind    string `json:"kind"` // info | ok | err
	Message string `json:"message"`
}

// ErrNoTab is returned for unknown tab ids.
var ErrNoTab = errors.New("Tab nicht verbunden")

// Conn is a bidirectional terminal stream.
type Conn interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
}

// Breaker is implemented by connections that can send a break signal.
type Breaker interface {
	SendBreak() error
}

// Emitter sends an event to the UI.
type Emitter func(event string, payload any)

// State is the payload of EventState.
type State struct {
	ID      string `json:"id"`
	State   string `json:"state"` // connecting | connected | closed | error
	Message string `json:"message"`
}

type tab struct {
	id      string
	conn    Conn
	log     io.WriteCloser
	mu      sync.Mutex
	closers []io.Closer
	closed  bool
	once    sync.Once
	done    chan struct{}
	unacked atomic.Int64              // bytes sent to the UI but not yet rendered
	ackSig  chan struct{}             // signalled on Ack
	watch   atomic.Pointer[echoWatch] // set while a paced paste runs
	notes   Noter                     // session log events (nil = not logging)

	tailMu sync.Mutex
	tail   []byte // last output (read by automation / MCP)

	tap atomic.Pointer[io.PipeWriter] // automation driving this tab (see tap.go)
}

// tailKeep is how much recent output each tab keeps for Tail.
const tailKeep = 64 * 1024

func (t *tab) addTail(p []byte) {
	t.tailMu.Lock()
	t.tail = append(t.tail, p...)
	if len(t.tail) > 2*tailKeep {
		t.tail = append([]byte(nil), t.tail[len(t.tail)-tailKeep:]...)
	}
	t.tailMu.Unlock()
}

// Tail returns up to the last 64 KB a tab received (raw terminal bytes).
func (m *Manager) Tail(id string) ([]byte, error) {
	t, err := m.get(id)
	if err != nil {
		return nil, err
	}
	t.tailMu.Lock()
	defer t.tailMu.Unlock()
	from := max(0, len(t.tail)-tailKeep)
	return append([]byte(nil), t.tail[from:]...), nil
}

func (t *tab) shutdown() {
	t.once.Do(func() {
		t.conn.Close()
		t.mu.Lock()
		t.closed = true
		closers := t.closers
		t.closers = nil
		t.mu.Unlock()
		for _, c := range closers {
			c.Close()
		}
		t.endTap() // unblocks a pump writing to a reader that stopped reading
		close(t.done)
	})
}

// Manager owns all live terminal tabs.
type Manager struct {
	mu   sync.Mutex
	tabs map[string]*tab
	emit Emitter

	// Flow control: stop reading from the connection while more than
	// HighWater bytes await rendering in the UI (the remote side is then
	// throttled by SSH/serial flow control). If the UI stops acking for
	// AckTimeout, the window resets so a lost UI can never stall a session.
	HighWater  int64
	AckTimeout time.Duration
}

// NewManager creates a manager that reports through emit.
func NewManager(emit Emitter) *Manager {
	return &Manager{tabs: map[string]*tab{}, emit: emit, HighWater: defaultHighWater, AckTimeout: defaultAckTimeout}
}

// Attach registers c under id and starts streaming its output.
// log (optional) receives a raw copy of the output and is closed with the tab.
func (m *Manager) Attach(id string, c Conn, log io.WriteCloser) {
	t := &tab{id: id, conn: c, log: log, done: make(chan struct{}), ackSig: make(chan struct{}, 1)}
	if n, ok := log.(Noter); ok {
		t.notes = n
	}
	m.mu.Lock()
	old := m.tabs[id]
	m.tabs[id] = t
	m.mu.Unlock()
	if old != nil {
		old.shutdown()
		<-old.done
	}
	m.emit(EventState, State{ID: id, State: "connected"})
	go m.pump(t)
}

func (m *Manager) pump(t *tab) {
	event := EventDataPrefix + t.id
	b := newBatcher(batchDelay, batchMaxBytes, func(p []byte) {
		t.unacked.Add(int64(len(p)))
		m.emit(event, base64.StdEncoding.EncodeToString(p))
	})
	buf := make([]byte, readBufSize)
	var readErr error
	for {
		m.waitForUI(t)
		n, err := t.conn.Read(buf)
		if n > 0 {
			b.Write(buf[:n])
			t.addTail(buf[:n])
			t.feedTap(buf[:n])
			if w := t.watch.Load(); w != nil {
				w.feed(buf[:n])
			}
			if t.log != nil {
				if _, lerr := t.log.Write(buf[:n]); lerr != nil {
					t.log.Close()
					t.log = nil // stop logging, keep the session alive
					m.emit(EventNotify, Notice{Kind: "err", Message: "Session-Log abgebrochen: " + lerr.Error()})
				}
			}
		}
		if err != nil {
			readErr = err
			break
		}
	}
	b.Close()
	t.endTap()
	if t.log != nil {
		t.log.Close()
	}

	m.mu.Lock()
	current := m.tabs[t.id] == t
	if current {
		delete(m.tabs, t.id)
	}
	m.mu.Unlock()
	t.shutdown()
	if current {
		msg := "Verbindung beendet"
		if !errors.Is(readErr, io.EOF) && readErr != nil {
			msg = readErr.Error()
		}
		m.emit(EventState, State{ID: t.id, State: "closed", Message: msg})
	}
}

func (m *Manager) waitForUI(t *tab) {
	if m.HighWater <= 0 {
		return
	}
	for t.unacked.Load() > m.HighWater {
		select {
		case <-t.ackSig:
		case <-t.done:
			return
		case <-time.After(m.AckTimeout):
			t.unacked.Store(0)
			return
		}
	}
}

// Ack reports that the UI has rendered n bytes of a tab's output.
func (m *Manager) Ack(id string, n int) error {
	t, err := m.get(id)
	if err != nil {
		return err
	}
	if t.unacked.Add(-int64(n)) < 0 {
		t.unacked.Store(0)
	}
	select {
	case t.ackSig <- struct{}{}:
	default:
	}
	return nil
}

func (m *Manager) get(id string) (*tab, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tabs[id]
	if !ok {
		return nil, ErrNoTab
	}
	return t, nil
}

// Write sends user input to the tab.
func (m *Manager) Write(id string, p []byte) error {
	t, err := m.get(id)
	if err != nil {
		return err
	}
	_, err = t.conn.Write(p)
	return err
}

// Resize forwards a terminal size change.
func (m *Manager) Resize(id string, cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	t, err := m.get(id)
	if err != nil {
		return err
	}
	return t.conn.Resize(cols, rows)
}

// Break sends a break signal if the connection supports it.
func (m *Manager) Break(id string) error {
	t, err := m.get(id)
	if err != nil {
		return err
	}
	b, ok := t.conn.(Breaker)
	if !ok {
		return errors.New("Break wird von dieser Verbindung nicht unterstützt")
	}
	return b.SendBreak()
}

// Conn returns the live connection for id.
func (m *Manager) Conn(id string) (Conn, error) {
	t, err := m.get(id)
	if err != nil {
		return nil, err
	}
	return t.conn, nil
}

// CloseWith registers c to be closed together with the tab.
func (m *Manager) CloseWith(id string, c io.Closer) error {
	t, err := m.get(id)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return ErrNoTab
	}
	t.closers = append(t.closers, c)
	return nil
}

// Close disconnects a tab; the pump then emits the closed state.
func (m *Manager) Close(id string) error {
	t, err := m.get(id)
	if err != nil {
		return err
	}
	t.shutdown()
	return nil
}

// CloseAll disconnects every tab and waits for them to finish.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*tab, 0, len(m.tabs))
	for _, t := range m.tabs {
		all = append(all, t)
	}
	m.mu.Unlock()
	for _, t := range all {
		t.shutdown()
		<-t.done
	}
}

// Count returns the number of live tabs.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tabs)
}

// note writes an easyTTY event into the tab's session log, if it has one.
func (t *tab) note(text string) {
	if t.notes != nil {
		t.notes.Note(text)
	}
}
