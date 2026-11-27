package terminal

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// ciscoDevice echoes every line after the prompt "sw1(config)#" and answers
// the lines in bad with IOS' error: a '^' under the bad word, then the message.
type ciscoDevice struct {
	r    *io.PipeReader
	w    *io.PipeWriter
	mu   sync.Mutex
	got  []string
	bad  map[string]string // line -> word the marker points at
	logs map[string]string // line -> syslog message printed after it
	buf  []byte
	outq chan string // one writer keeps the output in order, like a real device
}

const ciscoPrompt = "sw1(config)#"

func newCiscoDevice(bad, logs map[string]string) *ciscoDevice {
	r, w := io.Pipe()
	d := &ciscoDevice{r: r, w: w, bad: bad, logs: logs, outq: make(chan string, 64)}
	d.outq <- ciscoPrompt
	go func() {
		for out := range d.outq {
			if _, err := w.Write([]byte(out)); err != nil {
				return
			}
		}
	}()
	return d
}

func (d *ciscoDevice) Read(p []byte) (int, error) { return d.r.Read(p) }
func (d *ciscoDevice) Close() error               { return d.w.Close() }
func (d *ciscoDevice) Resize(int, int) error      { return nil }

func (d *ciscoDevice) Write(p []byte) (int, error) {
	d.mu.Lock()
	d.buf = append(d.buf, p...)
	var lines []string
	for {
		i := strings.IndexByte(string(d.buf), '\r')
		if i < 0 {
			break
		}
		lines = append(lines, string(d.buf[:i]))
		d.buf = d.buf[i+1:]
	}
	d.got = append(d.got, lines...)
	d.mu.Unlock()
	for _, line := range lines {
		out := line + "\r\n"
		if word, ok := d.bad[line]; ok {
			out += strings.Repeat(" ", len(ciscoPrompt)+strings.Index(line, word)) + "^\r\n" +
				"% Invalid input detected at '^' marker.\r\n\r\n"
		}
		if msg, ok := d.logs[line]; ok {
			out += msg + "\r\n"
		}
		d.outq <- out + ciscoPrompt
	}
	return len(p), nil
}

func (d *ciscoDevice) received() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.got...)
}

func pasteInto(t *testing.T, dev Conn, text string, opts PasteOptions) (PasteProgress, error) {
	t.Helper()
	m := NewManager((&recorder{}).emit)
	m.Attach("sw", dev, nil)
	defer m.CloseAll()
	var final PasteProgress
	err := m.Paste(context.Background(), "sw", text, opts, func(p PasteProgress) { final = p })
	return final, err
}

func TestPasteStopsAtRejectedLineAndShowsWhere(t *testing.T) {
	cfg := "interface Gi1/0/1\n description Uplink\n switchport mode acess\n no shutdown\n"
	dev := newCiscoDevice(map[string]string{" switchport mode acess": "acess"}, nil)
	final, err := pasteInto(t, dev, cfg, PasteOptions{StopOnError: true})
	if err == nil || final.State != "error" {
		t.Fatalf("paste must stop with an error: %v %+v", err, final)
	}
	if got := dev.received(); len(got) != 3 {
		t.Fatalf("lines after the error must not be sent: %q", got)
	}
	if final.ErrorCount != 1 || len(final.Errors) != 1 {
		t.Fatalf("errors: %+v", final)
	}
	e := final.Errors[0]
	if e.Line != 3 || e.Text != " switchport mode acess" || e.Column != 18 {
		t.Errorf("error = %+v, want line 3, column 18", e)
	}
	if len(e.Reply) != 1 || !strings.Contains(e.Reply[0], "Invalid input") {
		t.Errorf("reply = %q", e.Reply)
	}
	if final.StopLine != 4 || final.StopText != " no shutdown" {
		t.Errorf("stop = %d %q", final.StopLine, final.StopText)
	}
}

func TestPasteCollectsErrorsWithoutStopping(t *testing.T) {
	cfg := "interfase Gi1/0/1\nhostname sw1\nvlan 10\n"
	dev := newCiscoDevice(map[string]string{"interfase Gi1/0/1": "interfase"},
		map[string]string{"vlan 10": "*Mar  1 00:00:01: %LINK-3-UPDOWN: Interface Vlan10, changed state to up"})
	final, err := pasteInto(t, dev, cfg, PasteOptions{})
	if err != nil || final.State != "done" {
		t.Fatalf("paste: %v %+v", err, final)
	}
	if len(dev.received()) != 3 {
		t.Fatalf("all lines must be sent: %q", dev.received())
	}
	if final.ErrorCount != 1 || final.Errors[0].Line != 1 || final.Errors[0].Column != 1 {
		t.Errorf("errors = %+v (a log message is no error)", final.Errors)
	}
}

func TestDeviceErrorPatterns(t *testing.T) {
	for _, l := range []string{
		"% Invalid input detected at '^' marker.",
		"% Incomplete command.",
		"% Ambiguous command:  \"sh\"",
		"% Unrecognized command found at '^' position.",
		"Error: Unrecognized command found at '^' position.",
		"error: syntax error: interfaces",
		"syntax error, expecting <command>.",
		"unknown command.",
		"Invalid input: swichport",
		"Command fail. Return code -61",
		"bad command name foo (line 1 column 1)",
		"%Error opening tftp://10.0.0.1/x (Timed out)",
	} {
		if !deviceError.MatchString(l) {
			t.Errorf("not recognised as error: %q", l)
		}
	}
	for _, l := range []string{
		"% Warning: do not enable portfast on links to switches",
		"Building configuration...",
		" description Invalid input handling",
		"sw1(config)#",
	} {
		if deviceError.MatchString(l) {
			t.Errorf("taken for an error: %q", l)
		}
	}
	if !syslogLine.MatchString("%LINK-3-UPDOWN: Interface Gi1/0/1, changed state to down") {
		t.Error("syslog line not recognised")
	}
}

func TestEchoPaceAdapts(t *testing.T) {
	var p echoPace
	if p.wait(3*time.Second) != 3*time.Second {
		t.Error("without measurements the full timeout applies")
	}
	for range echoPaceMin {
		p.observe(10 * time.Millisecond)
	}
	if got := p.wait(3 * time.Second); got != echoPaceFloor {
		t.Errorf("fast device: wait %v, want %v", got, echoPaceFloor)
	}
	p.observe(200 * time.Millisecond) // slow serial line
	if got := p.wait(3 * time.Second); got != 1600*time.Millisecond {
		t.Errorf("slow echo: wait %v", got)
	}
	p.observe(time.Second)
	if got := p.wait(3 * time.Second); got != 3*time.Second {
		t.Errorf("never above the maximum: %v", got)
	}
}
