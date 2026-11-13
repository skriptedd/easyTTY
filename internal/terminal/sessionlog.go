package terminal

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const tsLayout = "2006-01-02 15:04:05"

// maxLogLine is the longest line kept before it is written out in parts.
const maxLogLine = 64 * 1024

// LogInfo describes a session for the log header.
type LogInfo struct {
	Session string
	Target  string // e.g. "admin@10.0.0.1:22" or "COM4 @ 9600"
}

// SessionLog writes a readable transcript of terminal output: one line per
// terminal line, each prefixed with the time it started, ANSI colour/cursor
// sequences and control characters removed. With raw=true the bytes are
// written unchanged (only header/footer are added).
type SessionLog struct {
	mu      sync.Mutex
	w       io.WriteCloser
	raw     bool
	now     func() time.Time
	started time.Time

	line    []byte
	cr      bool // saw \r without \n: next text overwrites the line
	esc     escState
	closed  bool
	failure error
}

type escState int

const (
	escNone    escState = iota
	escStart            // after ESC
	escCSI              // ESC [ ... final byte
	escOSC              // ESC ] ... BEL or ESC \
	escOSCEsc           // ESC inside OSC (maybe ST)
	escCharset          // ESC ( X and similar two-byte forms
)

// NewSessionLog wraps w and writes the header line.
func NewSessionLog(w io.WriteCloser, info LogInfo, raw bool, now func() time.Time) (*SessionLog, error) {
	if now == nil {
		now = time.Now
	}
	l := &SessionLog{w: w, raw: raw, now: now, started: now()}
	// One line, whatever a (possibly imported) session name contains.
	oneLine := strings.NewReplacer("\r", " ", "\n", " ")
	header := fmt.Sprintf("===== easyTTY · %s · %s · Start %s =====\r\n", oneLine.Replace(info.Session), oneLine.Replace(info.Target), l.started.Format(tsLayout))
	if _, err := w.Write([]byte(header)); err != nil {
		return nil, err
	}
	return l, nil
}

// Write consumes terminal output. It never returns a short count; the first
// write error is returned and sticks.
func (l *SessionLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failure != nil {
		return 0, l.failure
	}
	if l.raw {
		if _, err := l.w.Write(p); err != nil {
			l.failure = err
			return 0, err
		}
		return len(p), nil
	}
	for _, b := range p {
		l.feed(b)
		if l.failure != nil {
			return 0, l.failure
		}
	}
	return len(p), nil
}

func (l *SessionLog) feed(b byte) {
	switch l.esc {
	case escStart:
		switch b {
		case '[':
			l.esc = escCSI
		case ']':
			l.esc = escOSC
		case 'P', 'X', '^', '_': // DCS, SOS, PM, APC: a string like OSC, not text
			l.esc = escOSC
		case '(', ')', '*', '+', '#', '%':
			l.esc = escCharset
		default:
			l.esc = escNone // two-byte sequence like ESC =, ESC 7
		}
		return
	case escCSI:
		if b >= 0x40 && b <= 0x7e {
			l.esc = escNone
		}
		return
	case escOSC:
		if b == 0x07 {
			l.esc = escNone
		} else if b == 0x1b {
			l.esc = escOSCEsc
		}
		return
	case escOSCEsc:
		l.esc = escNone // ESC \ (ST); anything else ends it too
		return
	case escCharset:
		l.esc = escNone
		return
	}

	switch b {
	case 0x1b:
		l.esc = escStart
	case '\n':
		l.cr = false
		l.emitLine()
	case '\r':
		l.cr = true
	case '\b':
		l.cr = false
		l.backspace()
	case '\t':
		l.put(b)
	default:
		if b < 0x20 || b == 0x7f {
			return // BEL and other controls
		}
		l.put(b)
	}
}

func (l *SessionLog) put(b byte) {
	if l.cr { // carriage return without newline: line is being redrawn
		l.cr = false
		l.line = l.line[:0]
	}
	l.line = append(l.line, b)
	if len(l.line) >= maxLogLine { // binary data without newlines must not fill the memory
		l.emitLine()
	}
}

func (l *SessionLog) backspace() {
	if len(l.line) == 0 {
		return
	}
	_, size := utf8.DecodeLastRune(l.line)
	l.line = l.line[:len(l.line)-size]
}

// emitLine stamps a line with the time it was completed, so a command line
// carries the moment Enter was pressed (not when the prompt appeared).
func (l *SessionLog) emitLine() {
	at := l.now()
	out := make([]byte, 0, len(l.line)+24)
	out = append(out, '[')
	out = append(out, at.Format(tsLayout)...)
	out = append(out, "] "...)
	out = append(out, l.line...)
	out = append(out, '\r', '\n')
	l.line = l.line[:0]
	if _, err := l.w.Write(out); err != nil {
		l.failure = err
	}
}

// Close flushes a pending partial line, writes the footer and closes the file.
func (l *SessionLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.failure == nil {
		if !l.raw && len(l.line) > 0 {
			l.emitLine()
		}
		end := l.now()
		dur := end.Sub(l.started).Round(time.Second)
		footer := fmt.Sprintf("\r\n===== Ende %s · Dauer %s =====\r\n", end.Format(tsLayout), dur)
		_, _ = l.w.Write([]byte(footer))
	}
	return l.w.Close()
}

// Note records an easyTTY event (e.g. a cancelled paste) as its own marked
// line, without disturbing the terminal line currently being collected.
func (l *SessionLog) Note(text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.failure != nil {
		return
	}
	line := fmt.Sprintf("[%s] >>> easyTTY: %s\r\n", l.now().Format(tsLayout), text)
	if _, err := l.w.Write([]byte(line)); err != nil {
		l.failure = err
	}
}
