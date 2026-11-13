package terminal

import (
	"sync"
	"time"
)

// batcher coalesces many small writes into fewer flushes so a flood of
// output (e.g. `cat` of a big file) becomes a few large UI events.
type batcher struct {
	mu      sync.Mutex
	buf     []byte
	signal  chan struct{}
	stopped chan struct{}
	flush   func([]byte)
	delay   time.Duration
	maxSize int
}

func newBatcher(delay time.Duration, maxSize int, flush func([]byte)) *batcher {
	b := &batcher{
		signal:  make(chan struct{}, 1),
		stopped: make(chan struct{}),
		flush:   flush,
		delay:   delay,
		maxSize: maxSize,
	}
	go b.run()
	return b
}

// Write queues p; it never blocks on the consumer.
func (b *batcher) Write(p []byte) {
	b.mu.Lock()
	b.buf = append(b.buf, p...)
	b.mu.Unlock()
	select {
	case b.signal <- struct{}{}:
	default:
	}
}

// Close flushes pending data and stops the worker.
func (b *batcher) Close() {
	close(b.signal)
	<-b.stopped
}

func (b *batcher) take() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.buf
	b.buf = nil
	return out
}

func (b *batcher) pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buf)
}

func (b *batcher) run() {
	defer close(b.stopped)
	for range b.signal {
		// Give the producer a moment to add more, unless the buffer is already big.
		if b.pending() < b.maxSize {
			time.Sleep(b.delay)
		}
		if data := b.take(); len(data) > 0 {
			b.flush(data)
		}
	}
	if data := b.take(); len(data) > 0 {
		b.flush(data)
	}
}
