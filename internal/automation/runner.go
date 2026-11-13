package automation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Device kinds decide how paging is switched off.
const (
	DeviceAuto  = "auto"  // Cisco-like prompt (no "@", ends in # or >) → Cisco
	DeviceCisco = "cisco" // terminal length 0 / terminal width 0
	DeviceOther = "other" // send nothing extra (Linux, other vendors)
)

const (
	defaultIdleTimeout = 30 * time.Second
	maxCommandTime     = 10 * time.Minute
	settleTime         = 2 * time.Second // quiet time before trusting a generic prompt
	wakeTimeout        = 15 * time.Second
	pollEvery          = 40 * time.Millisecond
	maxOutputBytes     = 8 << 20
)

var (
	// genericPrompt matches typical CLI prompts: "sw1#", "sw1(config-if)#",
	// "user@host:~$", "[root@host ~]#", "router>".
	genericPrompt = regexp.MustCompile(`^[\w.\-@:~/()\[\] ]{1,100}[#>$%]\s*$`)
	// pagerLine matches "--More--", " -- More -- ", "---(more)---", "---- More ----".
	pagerLine = regexp.MustCompile(`(?i)^\s*-+\s*\(?\s*more\b[^\n]{0,40}$`)
	// confirmLine matches yes/Enter questions like "[confirm]" or "Destination filename [startup-config]?".
	confirmLine = regexp.MustCompile(`(\[confirm\]|\]\?|\[yes/no\]:?|\[y/n\]:?)\s*$`)
	// errorLine marks a failed command (Cisco IOS/NX-OS style, shells).
	errorLine = regexp.MustCompile(`(?i)^\s*(% ?(invalid|incomplete|ambiguous|unknown|unrecognized|bad)\b|.*: command not found$|error: )`)
)

// ErrDisconnected is returned when the device closes the connection.
var ErrDisconnected = errors.New("Verbindung getrennt")

// Options control a run.
type Options struct {
	Device      string        // DeviceAuto (default), DeviceCisco, DeviceOther
	IdleTimeout time.Duration // fail a command after this long without output; 0 = 30 s
	// TypeDelay types commands character by character and StepPause waits
	// before each further command, so a watching user can read along.
	TypeDelay   time.Duration
	StepPause   time.Duration
	StopOnError bool // stop after the first failed command
	AutoConfirm bool // answer "[confirm]"/"…]?" questions with Enter
	// Transcript (optional) receives every byte the device sends.
	Transcript io.Writer
}

// Step is the result of one command.
type Step struct {
	Command string `json:"command"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
	Millis  int64  `json:"ms"`
}

// Result is the outcome of Run.
type Result struct {
	Prompt string `json:"prompt"`
	Steps  []Step `json:"steps"`
	Error  string `json:"error,omitempty"` // why the run stopped early
}

// OK reports whether every command ran without error.
func (r Result) OK() bool {
	if r.Error != "" {
		return false
	}
	for _, s := range r.Steps {
		if s.Error != "" {
			return false
		}
	}
	return true
}

// SplitCommands turns a text block into commands: one per line, blank lines
// and comment lines ("!" or "#") skipped, trailing whitespace trimmed.
func SplitCommands(text string) []string {
	var out []string
	// A lone CR stays in the line on purpose: CheckCommands then refuses it,
	// because it would act as Enter and hide a second command.
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		l = strings.TrimRight(l, " \t\r")
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "!") || strings.HasPrefix(t, "#") {
			continue
		}
		out = append(out, l)
	}
	return out
}

// stream collects what the device sends.
type stream struct {
	mu   sync.Mutex
	buf  []byte
	last time.Time
	err  error
	sig  chan struct{}
}

// capOutput keeps the end of an oversized output: the prompt that ends the
// command is there, and a marker tells the reader that the middle is gone.
func capOutput(b []byte) []byte {
	if len(b) <= maxOutputBytes {
		return b
	}
	const marker = "\n[easyTTY: Ausgabe gekürzt]\n"
	tail := b[len(b)-maxOutputBytes/2:]
	return append(append(make([]byte, 0, maxOutputBytes), marker...), tail...)
}

func newStream(r io.Reader, transcript io.Writer) *stream {
	s := &stream{sig: make(chan struct{}, 1), last: time.Now()}
	go func() {
		p := make([]byte, 32*1024)
		for {
			n, err := r.Read(p)
			s.mu.Lock()
			if n > 0 {
				if transcript != nil {
					_, _ = transcript.Write(p[:n])
				}
				s.buf = capOutput(append(s.buf, p[:n]...))
				s.last = time.Now()
			}
			if err != nil {
				s.err = err
			}
			s.mu.Unlock()
			select {
			case s.sig <- struct{}{}:
			default:
			}
			if err != nil {
				return
			}
		}
	}()
	return s
}

// snapshot returns the text so far, its raw length, time of the last data and a read error.
func (s *stream) snapshot() (string, int, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return CleanText(s.buf), len(s.buf), s.last, s.err
}

func (s *stream) reset() {
	s.mu.Lock()
	s.buf = s.buf[:0]
	s.last = time.Now()
	s.mu.Unlock()
}

type runner struct {
	w      io.Writer
	s      *stream
	opts   Options
	prompt *regexp.Regexp // learned from the first prompt
	typed  int            // commands sent so far (StepPause before the next)
}

// promptFor builds a matcher from a seen prompt: its stem ("sw1" from
// "sw1(config)#", "user@host" from "user@host:~$") followed by anything.
func promptFor(seen string) *regexp.Regexp {
	stem := strings.TrimLeft(strings.TrimSpace(seen), "[")
	if i := strings.IndexAny(stem, ":( ]"); i > 0 {
		stem = stem[:i]
	}
	stem = strings.TrimRight(stem, "#>$% ")
	if stem == "" {
		return genericPrompt
	}
	return regexp.MustCompile(`^\[?` + regexp.QuoteMeta(stem) + `[^\n]{0,120}[#>$%]\s*$`)
}

// promptQuiet is the silence needed to trust a generic prompt. Once the real
// prompt is known it takes longer: a progress line ("Copying: 45%") looks like
// a prompt too, while a hostname change is rare and only costs a few seconds.
func (r *runner) promptQuiet() time.Duration {
	if r.prompt != nil {
		return 5 * settleTime
	}
	return settleTime
}

// wait collects output until a prompt shows. It pages through "--More--"
// and, with AutoConfirm, answers confirmation questions.
func (r *runner) wait(ctx context.Context, idleLimit time.Duration) (string, error) {
	start := time.Now()
	answeredAt := -1
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		text, n, last, rerr := r.s.snapshot()
		ll := strings.TrimRight(lastLine(text), " ")
		idle := time.Since(last)
		switch {
		case r.prompt != nil && r.prompt.MatchString(ll) && n > 0:
			return text, nil
		case pagerLine.MatchString(ll) && answeredAt != n:
			answeredAt = n
			if _, err := io.WriteString(r.w, " "); err != nil {
				return text, err
			}
		case idle >= r.promptQuiet() && genericPrompt.MatchString(ll):
			if r.prompt == nil || !r.prompt.MatchString(ll) {
				r.prompt = promptFor(ll) // first prompt, or the hostname changed
			}
			return text, nil
		case r.opts.AutoConfirm && idle >= settleTime/2 && confirmLine.MatchString(ll) && answeredAt != n:
			answeredAt = n
			if _, err := io.WriteString(r.w, "\r"); err != nil {
				return text, err
			}
		}
		if rerr != nil {
			return text, ErrDisconnected
		}
		if idle > idleLimit {
			return text, fmt.Errorf("keine Antwort seit %d s (letzte Zeile: %q)", int(idleLimit.Seconds()), ll)
		}
		if time.Since(start) > maxCommandTime {
			return text, errors.New("Befehl läuft länger als 10 Minuten")
		}
		select {
		case <-ctx.Done():
			return text, ctx.Err()
		case <-r.s.sig:
		case <-tick.C:
		}
	}
}

// exec sends one command and returns its cleaned output.
func (r *runner) exec(ctx context.Context, cmd string) Step {
	if r.typed > 0 && sleepCtx(ctx, r.opts.StepPause) != nil {
		return Step{Command: cmd, Error: ctx.Err().Error()}
	}
	r.typed++
	start := time.Now()
	r.s.reset()
	st := Step{Command: cmd}
	if err := r.typeLine(ctx, cmd); err != nil {
		st.Error = err.Error()
		return st
	}
	text, err := r.wait(ctx, r.idleTimeout())
	st.Millis = time.Since(start).Milliseconds()
	st.Output = commandOutput(text, cmd)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	for _, l := range strings.Split(st.Output, "\n") {
		if errorLine.MatchString(l) {
			st.Error = strings.TrimSpace(l)
			break
		}
	}
	return st
}

// typeLine sends cmd and Enter, character by character when TypeDelay is set.
func (r *runner) typeLine(ctx context.Context, cmd string) error {
	if r.opts.TypeDelay <= 0 {
		_, err := io.WriteString(r.w, cmd+"\r")
		return err
	}
	for _, c := range cmd {
		if _, err := io.WriteString(r.w, string(c)); err != nil {
			return err
		}
		if err := sleepCtx(ctx, r.opts.TypeDelay); err != nil {
			return err
		}
	}
	_, err := io.WriteString(r.w, "\r")
	return err
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (r *runner) idleTimeout() time.Duration {
	if r.opts.IdleTimeout > 0 {
		return r.opts.IdleTimeout
	}
	return defaultIdleTimeout
}

// commandOutput drops the echoed command line, the closing prompt and pager remnants.
func commandOutput(text, cmd string) string {
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && strings.Contains(lines[0], strings.TrimSpace(cmd)) {
		lines = lines[1:]
	}
	if len(lines) > 0 && genericPrompt.MatchString(strings.TrimRight(lines[len(lines)-1], " ")) {
		lines = lines[:len(lines)-1]
	}
	out := lines[:0]
	for _, l := range lines {
		if pagerLine.MatchString(l) {
			continue
		}
		out = append(out, strings.TrimRight(l, " "))
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

// RunCommands wakes the device with Enter, learns its prompt, switches paging off
// for Cisco-like devices and then sends the commands one by one.
// onStep (optional) is called after each command.
func RunCommands(ctx context.Context, rw io.ReadWriter, commands []string, opts Options, onStep func(Step)) Result {
	if opts.Device == "" {
		opts.Device = DeviceAuto
	}
	var res Result
	if err := CheckCommands(commands); err != nil {
		res.Error = err.Error()
		return res
	}
	r := &runner{w: rw, s: newStream(rw, opts.Transcript), opts: opts}
	if _, err := io.WriteString(rw, "\r"); err != nil {
		res.Error = err.Error()
		return res
	}
	text, err := r.wait(ctx, wakeTimeout)
	if err != nil {
		res.Error = "kein Prompt: " + err.Error()
		return res
	}
	res.Prompt = strings.TrimSpace(lastLine(text))
	if r.prompt == nil {
		r.prompt = promptFor(res.Prompt)
	}
	if isCisco(opts.Device, res.Prompt) {
		for _, c := range []string{"terminal length 0", "terminal width 0"} {
			if st := r.exec(ctx, c); ctx.Err() != nil || st.Error == ErrDisconnected.Error() {
				res.Error = st.Error
				return res
			}
		}
	}
	for _, c := range commands {
		if ctx.Err() != nil {
			res.Error = "abgebrochen"
			return res
		}
		st := r.exec(ctx, c)
		res.Steps = append(res.Steps, st)
		if onStep != nil {
			onStep(st)
		}
		if st.Error == ErrDisconnected.Error() {
			res.Error = st.Error
			return res
		}
		if st.Error != "" && opts.StopOnError {
			res.Error = fmt.Sprintf("gestoppt nach Fehler in Zeile %d", len(res.Steps))
			return res
		}
	}
	return res
}

func isCisco(device, prompt string) bool {
	switch device {
	case DeviceCisco:
		return true
	case DeviceAuto:
		p := strings.TrimSpace(prompt)
		return !strings.Contains(p, "@") && (strings.HasSuffix(p, "#") || strings.HasSuffix(p, ">"))
	}
	return false
}
