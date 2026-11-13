package fwserver

import (
	"io"
	"sync"
	"time"
)

// progressEvery limits "läuft" events to one per second per transfer.
const progressEvery = time.Second

// progress counts the bytes of one transfer and reports them while it runs,
// so the UI can show how far a device is without the server log open.
type progress struct {
	mu   sync.Mutex
	ev   Event
	log  func(Event)
	last time.Time
}

func newProgress(ev Event, log func(Event)) *progress {
	return &progress{ev: ev, log: log, last: time.Now()}
}

func (p *progress) add(n int64) {
	if n <= 0 {
		return
	}
	p.mu.Lock()
	p.ev.Done += n
	report := time.Since(p.last) >= progressEvery
	snap := p.ev
	if report {
		p.last = time.Now()
	}
	p.mu.Unlock()
	if report {
		p.log(snap)
	}
}

func (p *progress) done() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ev.Done
}

// progressWriter reports everything written through it.
type progressWriter struct {
	w io.Writer
	p *progress
}

func (pw progressWriter) Write(b []byte) (int, error) {
	n, err := pw.w.Write(b)
	pw.p.add(int64(n))
	return n, err
}
