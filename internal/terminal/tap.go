package terminal

import (
	"errors"
	"io"
)

// ErrTapBusy is returned while another automation already drives the tab.
var ErrTapBusy = errors.New("In diesem Tab läuft schon eine Automation")

// TabStream lets automation drive a visible tab: writes are typed into the
// tab, reads return what the tab receives from now on (the UI keeps showing
// everything). Close detaches it; the tab stays open.
type TabStream struct {
	m  *Manager
	id string
	t  *tab
	pr *io.PipeReader
	pw *io.PipeWriter
}

// Tap attaches a stream to a connected tab (one at a time).
func (m *Manager) Tap(id string) (*TabStream, error) {
	t, err := m.get(id)
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	if !t.tap.CompareAndSwap(nil, pw) {
		return nil, ErrTapBusy
	}
	select {
	case <-t.done: // closed between get and CompareAndSwap
		t.tap.CompareAndSwap(pw, nil)
		return nil, ErrNoTab
	default:
	}
	return &TabStream{m: m, id: id, t: t, pr: pr, pw: pw}, nil
}

func (s *TabStream) Read(p []byte) (int, error)  { return s.pr.Read(p) }
func (s *TabStream) Write(p []byte) (int, error) { return len(p), s.m.Write(s.id, p) }

// Close detaches the stream without closing the tab.
func (s *TabStream) Close() error {
	s.t.tap.CompareAndSwap(s.pw, nil)
	_ = s.pw.Close()
	return s.pr.Close()
}

// feedTap copies output to an attached stream; a slow reader never loses data
// (the pump waits, like it waits for the UI).
func (t *tab) feedTap(p []byte) {
	if w := t.tap.Load(); w != nil {
		_, _ = w.Write(p)
	}
}

// endTap tells an attached stream that the connection is gone.
func (t *tab) endTap() {
	if w := t.tap.Swap(nil); w != nil {
		_ = w.CloseWithError(io.EOF)
	}
}
