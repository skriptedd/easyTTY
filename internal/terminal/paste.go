package terminal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Paced pasting for devices with small input buffers (Cisco consoles):
// send one line, wait until the device has echoed it and is ready again
// (prompt or a pause), then send the next. At most one line is ever in
// flight, so a full running-config arrives intact.

const (
	defaultEchoTimeout = 3 * time.Second
	shortEchoTimeout   = 300 * time.Millisecond // after repeated misses (device does not echo)
	missesBeforeShort  = 3
	echoTailLen        = 4
	echoBufMax         = 64 * 1024
	pasteReportEvery   = 100 * time.Millisecond

	// After the echo: the device is ready when a prompt has stood still for
	// promptStill, or nothing came for readyQuiet (devices without a prompt).
	promptStill  = 60 * time.Millisecond
	readyQuiet   = 400 * time.Millisecond
	readyTimeout = 30 * time.Second // a long show output still gets its prompt
	readyPoll    = 20 * time.Millisecond
	maxPages     = 5000
)

var (
	// pastePager matches a last line that is a pager: "--More--", " -- More -- ",
	// "---(more)---". Anchored at the line start, so a prompt like "sw-more#"
	// is not answered with spaces.
	pastePager = regexp.MustCompile(`(?i)^\s*-+\s*\(?\s*more\b[^\n]{0,40}$`)
	// pastePrompt matches the end of a prompt or question: "sw1#", "sw1(config)#",
	// "router>", "user@host:~$", "Password:", "Destination filename [x]?".
	pastePrompt = regexp.MustCompile(`[#>$%:?\]]\s*$`)
)

// ErrPasteBusy is returned while another paste runs in the same tab.
var ErrPasteBusy = errors.New("Es läuft schon ein Einfügen in diesem Tab")

// PasteOptions tunes paced pasting.
type PasteOptions struct {
	LineDelay   time.Duration // extra pause after each line
	EchoTimeout time.Duration // max wait for the echo of one line (0 = default)
}

// PasteProgress reports a running paste.
type PasteProgress struct {
	ID       string `json:"id"`
	State    string `json:"state"` // running | done | cancelled | error
	Done     int    `json:"done"`
	Total    int    `json:"total"`
	Timeouts int    `json:"timeouts"` // lines sent without a confirmed echo
	Error    string `json:"error,omitempty"`
	// Where it stopped (cancel/error): 1-based line number and its text.
	StopLine int    `json:"stopLine,omitempty"`
	StopText string `json:"stopText,omitempty"`
	// Line numbers (1-based) sent without a confirmed echo, first maxMissList.
	Misses []int `json:"misses,omitempty"`
}

const maxMissList = 100

// Noter receives easyTTY events for the session log (SessionLog implements it).
type Noter interface{ Note(text string) }

// SplitPasteLines normalises CRLF/CR/LF and drops one trailing line break.
func SplitPasteLines(text string) []string {
	t := strings.ReplaceAll(text, "\r\n", "\n")
	t = strings.ReplaceAll(t, "\r", "\n")
	t = strings.TrimSuffix(t, "\n")
	if t == "" {
		return nil
	}
	return strings.Split(t, "\n")
}

// echoTail is the end of a line as the device will echo it ("" = only wait for a newline).
func echoTail(line string) string {
	clean := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, line)
	clean = strings.TrimRight(clean, " ")
	if utf8.RuneCountInString(clean) <= echoTailLen {
		return strings.TrimLeft(clean, " ")
	}
	r := []rune(clean)
	return string(r[len(r)-echoTailLen:])
}

// echoWatch collects device output while a paste runs.
type echoWatch struct {
	mu   sync.Mutex
	buf  []byte
	sig  chan struct{}
	last time.Time // when the device last sent something
	fed  int       // bytes received so far (pages are answered once per new output)
}

func newEchoWatch() *echoWatch { return &echoWatch{sig: make(chan struct{}, 1)} }

func (w *echoWatch) feed(p []byte) {
	w.mu.Lock()
	for _, b := range p {
		if b != '\r' && b != 0 { // CR placement differs between devices
			w.buf = append(w.buf, b)
		}
	}
	if len(w.buf) > echoBufMax {
		w.buf = append([]byte(nil), w.buf[len(w.buf)-echoBufMax/2:]...)
	}
	w.last = time.Now()
	w.fed += len(p)
	w.mu.Unlock()
	select {
	case w.sig <- struct{}{}:
	default:
	}
}

func (w *echoWatch) reset() {
	w.mu.Lock()
	w.buf = w.buf[:0]
	w.mu.Unlock()
}

// echoed reports whether tail and then a newline arrived since the last reset.
func (w *echoWatch) echoed(tail string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	rest := w.buf
	if tail != "" {
		i := bytes.Index(rest, []byte(tail))
		if i < 0 {
			return false
		}
		rest = rest[i+len(tail):]
	}
	return bytes.IndexByte(rest, '\n') >= 0
}

// wait blocks until the echo arrived (true) or timeout/cancel (false).
func (w *echoWatch) wait(ctx context.Context, done <-chan struct{}, tail string, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		if w.echoed(tail) {
			return true
		}
		select {
		case <-w.sig:
		case <-timer.C:
			return false
		case <-ctx.Done():
			return false
		case <-done:
			return false
		}
	}
}

// tail returns the last output line (backspace erasing applied, trailing
// spaces cut), how long the device has been silent and the bytes received.
func (w *echoWatch) tail() (line string, quiet time.Duration, fed int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	rest := w.buf
	if i := bytes.LastIndexByte(rest, '\n'); i >= 0 {
		rest = rest[i+1:]
	}
	out := make([]byte, 0, len(rest))
	for _, b := range rest {
		if b == '\b' { // pagers erase "--More--" with backspaces
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
			continue
		}
		out = append(out, b)
	}
	return strings.TrimRight(string(out), " "), time.Since(w.last), w.fed
}

// promptHost is the device name in a CLI prompt ("sw1" for "sw1(config-if)#"),
// or "" when line is no prompt.
func promptHost(line string) string {
	if !cliPrompt.MatchString(line) {
		return ""
	}
	if i := strings.IndexAny(line, "(#>$%"); i > 0 {
		return line[:i]
	}
	return ""
}

// cliPrompt matches a full prompt line: "sw1#", "sw1(config)#", "user@host:~$".
var cliPrompt = regexp.MustCompile(`^[\w.\-@:~/\[\] ]{1,100}(\([\w.\-/]{1,40}\))?[#>$%]$`)

// settle waits after an echoed line until the device can take the next one:
// the prompt of this device again (known, learned from the lines before),
// another prompt or question that stands still, or a pause in the output.
// "--More--" pages are answered with a space, so a pasted show command cannot
// swallow the first letter of the next line. ctx or done end the wait early.
func (w *echoWatch) settle(ctx context.Context, done <-chan struct{}, known *string, page func() error) {
	deadline := time.Now().Add(readyTimeout)
	tick := time.NewTicker(readyPoll)
	defer tick.Stop()
	pages, pagedAt := 0, -1
	for time.Now().Before(deadline) {
		line, quiet, fed := w.tail()
		host := promptHost(line)
		switch {
		case pastePager.MatchString(line):
			if fed != pagedAt && pages < maxPages {
				pagedAt = fed
				pages++
				if page() != nil {
					return
				}
			}
		case host != "" && host == *known:
			return // the device's own prompt: ready at once
		case pastePrompt.MatchString(line) && quiet >= promptStill:
			if host != "" {
				*known = host
			}
			return
		case quiet >= readyQuiet:
			return
		}
		select {
		case <-w.sig:
		case <-tick.C:
		case <-ctx.Done():
			return
		case <-done:
			return
		}
	}
}

// pasteSummary is the session-log line for the end of a paste.
func pasteSummary(p PasteProgress) string {
	var b strings.Builder
	switch p.State {
	case "done":
		fmt.Fprintf(&b, "Einfügen fertig: %d/%d Zeilen", p.Done, p.Total)
	case "cancelled":
		fmt.Fprintf(&b, "Einfügen ABGEBROCHEN (Esc) nach %d/%d Zeilen", p.Done, p.Total)
	default:
		fmt.Fprintf(&b, "Einfügen FEHLGESCHLAGEN nach %d/%d Zeilen: %s", p.Done, p.Total, p.Error)
	}
	if p.StopLine > 0 {
		fmt.Fprintf(&b, " – nicht mehr gesendet ab Zeile %d: %q", p.StopLine, p.StopText)
	}
	if p.Timeouts > 0 {
		nums := make([]string, len(p.Misses))
		for i, n := range p.Misses {
			nums[i] = fmt.Sprint(n)
		}
		fmt.Fprintf(&b, " – %d Zeile(n) ohne Echo: %s", p.Timeouts, strings.Join(nums, ", "))
	}
	return b.String()
}

// Paste sends text line by line, each followed by CR, waiting for the echo of
// every line. report is called with progress (throttled) and the final state.
func (m *Manager) Paste(ctx context.Context, id, text string, opts PasteOptions, report func(PasteProgress)) error {
	t, err := m.get(id)
	if err != nil {
		return err
	}
	w := newEchoWatch()
	if !t.watch.CompareAndSwap(nil, w) {
		return ErrPasteBusy
	}
	defer t.watch.Store(nil)

	timeout := opts.EchoTimeout
	if timeout <= 0 {
		timeout = defaultEchoTimeout
	}
	lines := SplitPasteLines(text)
	p := PasteProgress{ID: id, State: "running", Total: len(lines)}
	report(p)
	t.note(fmt.Sprintf("Einfügen gestartet: %d Zeilen (zeilenweise mit Echo)", len(lines)))
	last := time.Now()
	misses := 0
	prompt := "" // device name from its prompt, learned while pasting
	finish := func(state string, err error) error {
		p.State = state
		if err != nil {
			p.Error = err.Error()
		}
		if state != "done" && p.Done < len(lines) {
			p.StopLine, p.StopText = p.Done+1, lines[p.Done]
		}
		t.note(pasteSummary(p))
		report(p)
		return err
	}

	for i, line := range lines {
		if ctx.Err() != nil {
			return finish("cancelled", nil)
		}
		w.reset()
		if _, err := t.conn.Write([]byte(line + "\r")); err != nil {
			return finish("error", err)
		}
		wait := timeout
		if misses >= missesBeforeShort && shortEchoTimeout < wait {
			wait = shortEchoTimeout // device does not echo: degrade to fixed pacing
		}
		if w.wait(ctx, t.done, echoTail(line), wait) {
			misses = 0
			w.settle(ctx, t.done, &prompt, func() error {
				_, err := t.conn.Write([]byte(" "))
				return err
			})
		} else if ctx.Err() == nil {
			select {
			case <-t.done:
				return finish("error", errors.New("Verbindung getrennt"))
			default:
			}
			misses++
			p.Timeouts++
			if len(p.Misses) < maxMissList {
				p.Misses = append(p.Misses, i+1)
			}
		}
		if opts.LineDelay > 0 {
			select {
			case <-time.After(opts.LineDelay):
			case <-ctx.Done():
			}
		}
		p.Done++
		if time.Since(last) >= pasteReportEvery {
			last = time.Now()
			report(p)
		}
	}
	if ctx.Err() != nil {
		return finish("cancelled", nil)
	}
	return finish("done", nil)
}
