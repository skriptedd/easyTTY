package fsutil

import (
	"path/filepath"
	"sync/atomic"
	"testing"
)

// A reader holding the file open (another GetSettings, a virus scanner) must
// neither make a save fail nor fail itself.
func TestWriteAtomicWithConcurrentReader(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := WriteAtomic(p, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	var stop atomic.Bool
	var readFails atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !stop.Load() {
			if _, err := ReadFile(p); err != nil {
				readFails.Add(1)
			}
		}
	}()
	writeFails := 0
	for range 300 {
		if err := WriteAtomic(p, []byte(`{"a":2}`)); err != nil {
			writeFails++
		}
	}
	stop.Store(true)
	<-done
	if writeFails > 0 || readFails.Load() > 0 {
		t.Fatalf("writes failed %d times, reads %d times", writeFails, readFails.Load())
	}
}
