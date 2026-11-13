package terminal

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// pagerDevice is a Cisco console with "terminal length 24": a show command
// stops at "--More--" and the next key decides. Space shows the next page,
// any other key ends the output and is swallowed, like on a real switch.
type pagerDevice struct {
	r        *io.PipeReader
	w        *io.PipeWriter
	mu       sync.Mutex
	in       chan byte
	received []string
}

func newPagerDevice(pages int) *pagerDevice {
	r, w := io.Pipe()
	d := &pagerDevice{r: r, w: w, in: make(chan byte, 4096)}
	go d.run(pages)
	return d
}

func (d *pagerDevice) Read(p []byte) (int, error) { return d.r.Read(p) }
func (d *pagerDevice) Write(p []byte) (int, error) {
	for _, b := range p {
		d.in <- b
	}
	return len(p), nil
}
func (d *pagerDevice) Close() error          { return d.w.Close() }
func (d *pagerDevice) Resize(int, int) error { return nil }

func (d *pagerDevice) out(s string) bool {
	_, err := d.w.Write([]byte(s))
	return err == nil
}

func (d *pagerDevice) run(pages int) {
	var line []byte
	for b := range d.in {
		if b != '\r' {
			line = append(line, b)
			if !d.out(string(b)) { // echo
				return
			}
			continue
		}
		cmd := string(line)
		line = nil
		d.mu.Lock()
		d.received = append(d.received, cmd)
		d.mu.Unlock()
		if !d.out("\r\n") {
			return
		}
		time.Sleep(2 * time.Millisecond) // the switch runs the command
		if strings.HasPrefix(cmd, "show") {
			for p := 1; p <= pages; p++ {
				if !d.out("output page\r\n") {
					return
				}
				if p == pages {
					break
				}
				if !d.out(" --More-- ") {
					return
				}
				if key := <-d.in; key != ' ' {
					break // any other key quits the pager and is gone
				}
				d.out("\b\b\b\b\b\b\b\b\b\b          \b\b\b\b\b\b\b\b\b\b")
			}
		}
		if !d.out("sw1#") {
			return
		}
	}
}

func TestPasteKeepsFirstLetterAfterPagedOutput(t *testing.T) {
	m := NewManager((&recorder{}).emit)
	dev := newPagerDevice(3)
	m.Attach("sw", dev, nil)
	defer m.CloseAll()
	text := "show version\nshow ip interface brief\nshow clock\n"

	var final PasteProgress
	err := m.Paste(context.Background(), "sw", text, PasteOptions{}, func(p PasteProgress) { final = p })

	if err != nil {
		t.Fatal(err)
	}
	dev.mu.Lock()
	defer dev.mu.Unlock()
	want := []string{"show version", "show ip interface brief", "show clock"}
	if strings.Join(dev.received, "|") != strings.Join(want, "|") {
		t.Errorf("device got %q, want %q", dev.received, want)
	}
	if final.State != "done" || final.Timeouts != 0 {
		t.Errorf("final progress %+v", final)
	}
}

func TestPromptHost(t *testing.T) {
	for line, want := range map[string]string{
		"sw1#":                                   "sw1",
		"sw1(config-if)#":                        "sw1",
		"core-sw.rz1>":                           "core-sw.rz1",
		"user@nas:~$":                            "user@nas:~",
		"Password:":                              "",
		"Interface status:":                      "",
		"Destination filename [startup-config]?": "",
		"output page":                            "",
		"":                                       "",
	} {
		if got := promptHost(line); got != want {
			t.Errorf("promptHost(%q) = %q, want %q", line, got, want)
		}
	}
}

func TestPastePagerOnlyMatchesPagerLines(t *testing.T) {
	for line, want := range map[string]bool{
		" --More--":           true,
		"--More--":            true,
		" -- More -- ":        true,
		"---(more 42%)---":    true,
		"sw-more#":            false,
		"user@ask-more:~$":    false,
		"description no-more": false,
		"":                    false,
	} {
		if got := pastePager.MatchString(line); got != want {
			t.Errorf("pastePager(%q) = %v, want %v", line, got, want)
		}
	}
}
