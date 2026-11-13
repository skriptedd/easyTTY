package automation

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDevice simulates a Cisco-like CLI (or a shell with linux=true) over pipes.
type fakeDevice struct {
	toDev   *io.PipeWriter // runner writes here
	fromDev *io.PipeReader // runner reads here
	in      *io.PipeReader
	out     *io.PipeWriter

	mu       sync.Mutex
	host     string
	mode     string // "", "config", "config-if"
	paging   bool
	linux    bool
	cwd      string
	spaces   int
	received []string
}

func newFakeDevice(linux bool) *fakeDevice {
	d := &fakeDevice{host: "sw1", paging: true, linux: linux, cwd: "~"}
	d.in, d.toDev = io.Pipe()
	d.fromDev, d.out = io.Pipe()
	go d.serve()
	return d
}

func (d *fakeDevice) Read(p []byte) (int, error)  { return d.fromDev.Read(p) }
func (d *fakeDevice) Write(p []byte) (int, error) { return d.toDev.Write(p) }
func (d *fakeDevice) Close()                      { d.toDev.Close(); d.out.Close() }

func (d *fakeDevice) prompt() string {
	if d.linux {
		return fmt.Sprintf("test@%s:%s$ ", d.host, d.cwd)
	}
	if d.mode != "" {
		return d.host + "(" + d.mode + ")#"
	}
	return d.host + "#"
}

func (d *fakeDevice) print(s string) { _, _ = io.WriteString(d.out, s) }

// readByte reads one input byte (used for pager/confirm answers).
func (d *fakeDevice) readByte() (byte, error) {
	b := make([]byte, 1)
	_, err := io.ReadFull(d.in, b)
	return b[0], err
}

func (d *fakeDevice) serve() {
	var line []byte
	for {
		b, err := d.readByte()
		if err != nil {
			return
		}
		if b != '\r' {
			line = append(line, b)
			d.print(string(b)) // echo
			continue
		}
		cmd := strings.TrimSpace(string(line))
		line = line[:0]
		d.mu.Lock()
		d.received = append(d.received, cmd)
		d.mu.Unlock()
		d.print("\r\n")
		if !d.handle(cmd) {
			d.out.Close()
			return
		}
		d.print(d.prompt())
	}
}

func (d *fakeDevice) handle(cmd string) bool {
	switch {
	case cmd == "":
	case cmd == "terminal length 0":
		d.paging = false
	case cmd == "terminal width 0":
	case cmd == "show version":
		d.print("\x1b[1mCisco IOS Software\x1b[0m, C2960X Version 15.2(7)E9\r\nuptime is 3 weeks\r\n")
	case cmd == "show long":
		for i := 1; i <= 60; i++ {
			if d.paging && i%20 == 0 {
				d.print(" --More-- ")
				if b, err := d.readByte(); err != nil || b != ' ' {
					return false
				}
				d.spaces++
				d.print("\b\b\b\b\b\b\b\b\b\b          \b\b\b\b\b\b\b\b\b\b")
			}
			d.print(fmt.Sprintf("line %d\r\n", i))
		}
	case cmd == "conf t" || cmd == "configure terminal":
		d.print("Enter configuration commands, one per line.  End with CNTL/Z.\r\n")
		d.mode = "config"
	case strings.HasPrefix(cmd, "interface "):
		d.mode = "config-if"
	case strings.HasPrefix(cmd, "description "):
	case strings.HasPrefix(cmd, "hostname "):
		d.host = strings.TrimPrefix(cmd, "hostname ")
	case cmd == "end":
		d.mode = ""
	case cmd == "copy run start":
		d.print("Destination filename [startup-config]? ")
		if b, err := d.readByte(); err != nil || b != '\r' {
			return false
		}
		d.print("\r\nBuilding configuration...\r\n[OK]\r\n")
	case cmd == "slow":
		for i := 0; i < 5; i++ {
			time.Sleep(300 * time.Millisecond)
			d.print("tick\r\n")
		}
	case strings.HasPrefix(cmd, "cd ") && d.linux:
		d.cwd = strings.TrimPrefix(cmd, "cd ")
	case cmd == "ls" && d.linux:
		d.print("a.txt  b.txt\r\n")
	case cmd == "exit":
		return false
	default:
		if d.linux {
			d.print("bash: " + strings.Fields(cmd)[0] + ": command not found\r\n")
		} else {
			d.print("                   ^\r\n% Invalid input detected at '^' marker.\r\n\r\n")
		}
	}
	return true
}

func run(t *testing.T, d *fakeDevice, cmds string, opts Options) Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return RunCommands(ctx, d, SplitCommands(cmds), opts, nil)
}

func TestRunCiscoConfig(t *testing.T) {
	t.Parallel()
	d := newFakeDevice(false)
	defer d.Close()
	var transcript bytes.Buffer
	res := run(t, d, "show version\n! comment\nconf t\ninterface Gi1/0/1\n description Uplink\nend\nshow long", Options{Transcript: &transcript})
	if !res.OK() {
		t.Fatalf("result: %+v", res)
	}
	if res.Prompt != "sw1#" || len(res.Steps) != 6 {
		t.Fatalf("prompt %q steps %d", res.Prompt, len(res.Steps))
	}
	if res.Steps[0].Output != "Cisco IOS Software, C2960X Version 15.2(7)E9\nuptime is 3 weeks" {
		t.Errorf("show version output: %q", res.Steps[0].Output)
	}
	if !strings.HasPrefix(res.Steps[1].Output, "Enter configuration") {
		t.Errorf("conf t output: %q", res.Steps[1].Output)
	}
	if n := strings.Count(res.Steps[5].Output, "\n") + 1; n != 60 || d.spaces != 0 {
		t.Errorf("show long: %d lines, %d pager answers (paging should be off)", n, d.spaces)
	}
	if !strings.Contains(transcript.String(), "Uplink") {
		t.Error("transcript missing")
	}
	if d.received[1] != "terminal length 0" {
		t.Errorf("auto device did not switch paging off: %v", d.received[:3])
	}
}

func TestRunPagesThroughMore(t *testing.T) {
	t.Parallel()
	d := newFakeDevice(false)
	defer d.Close()
	res := run(t, d, "show long", Options{Device: DeviceOther})
	if !res.OK() {
		t.Fatalf("%+v", res)
	}
	out := res.Steps[0].Output
	if d.spaces != 3 || strings.Contains(out, "More") || strings.Count(out, "line ") != 60 {
		t.Errorf("spaces=%d output=%q", d.spaces, out)
	}
}

func TestRunErrorsAndStopOnError(t *testing.T) {
	t.Parallel()
	d := newFakeDevice(false)
	defer d.Close()
	res := run(t, d, "bogus\nshow version", Options{StopOnError: true})
	if res.OK() || len(res.Steps) != 1 || !strings.Contains(res.Steps[0].Error, "Invalid input") || res.Error == "" {
		t.Fatalf("stop: %+v", res)
	}
	d2 := newFakeDevice(false)
	defer d2.Close()
	res = run(t, d2, "bogus\nshow version", Options{})
	if len(res.Steps) != 2 || res.Steps[1].Error != "" {
		t.Fatalf("continue: %+v", res)
	}
}

func TestRunAutoConfirm(t *testing.T) {
	t.Parallel()
	d := newFakeDevice(false)
	defer d.Close()
	res := run(t, d, "copy run start", Options{AutoConfirm: true})
	if !res.OK() || !strings.Contains(res.Steps[0].Output, "[OK]") {
		t.Fatalf("%+v", res)
	}
	d2 := newFakeDevice(false)
	defer d2.Close()
	res = run(t, d2, "copy run start", Options{IdleTimeout: 2500 * time.Millisecond})
	if res.OK() || !strings.Contains(res.Steps[0].Error, "keine Antwort") {
		t.Fatalf("ohne AutoConfirm: %+v", res)
	}
}

func TestRunHostnameChangeAndSlowOutput(t *testing.T) {
	t.Parallel()
	d := newFakeDevice(false)
	defer d.Close()
	res := run(t, d, "conf t\nhostname core-sw1\nend\nslow\nshow version", Options{})
	if !res.OK() {
		t.Fatalf("%+v", res)
	}
	if strings.Count(res.Steps[3].Output, "tick") != 5 {
		t.Errorf("slow: %q", res.Steps[3].Output)
	}
	if !strings.Contains(res.Steps[4].Output, "Cisco IOS") {
		t.Errorf("after rename: %q", res.Steps[4].Output)
	}
}

func TestRunLinuxShell(t *testing.T) {
	t.Parallel()
	d := newFakeDevice(true)
	defer d.Close()
	res := run(t, d, "ls\ncd /tmp\nls\nfoo", Options{})
	if res.Prompt != "test@sw1:~$" || len(res.Steps) != 4 {
		t.Fatalf("%+v", res)
	}
	if res.Steps[2].Output != "a.txt  b.txt" || res.Steps[3].Error == "" {
		t.Errorf("steps: %+v", res.Steps)
	}
	for _, c := range d.received {
		if strings.HasPrefix(c, "terminal") {
			t.Errorf("linux got %q", c)
		}
	}
}

func TestRunDisconnectAndCancel(t *testing.T) {
	t.Parallel()
	d := newFakeDevice(false)
	defer d.Close()
	res := run(t, d, "exit\nshow version", Options{})
	if res.Error != ErrDisconnected.Error() || len(res.Steps) != 1 {
		t.Fatalf("disconnect: %+v", res)
	}

	d2 := newFakeDevice(false)
	defer d2.Close()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(3*time.Second, cancel)
	res = RunCommands(ctx, d2, []string{"slow", "slow", "slow"}, Options{}, nil)
	if res.OK() || len(res.Steps) == 3 {
		t.Fatalf("cancel: %+v", res)
	}
}

func TestCleanText(t *testing.T) {
	cases := map[string]string{
		"a\x1b[31mred\x1b[0m\r\nb":     "ared\nb",
		"--More--\r        \rnext\r\n": "next\n",
		"abc\b\bX":                     "aX",
		"\x1b]0;title\x07prompt#":      "prompt#",
		"x\x1b(By\x07z":                "xyz",
		"line\r":                       "line",
		"ümlaut\b!":                    "ümlau!",
	}
	for in, want := range cases {
		if got := CleanText([]byte(in)); got != want {
			t.Errorf("CleanText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitCommandsAndPrompt(t *testing.T) {
	got := SplitCommands("conf t\r\n\n! kommentar\n  # auch\n interface Gi1/0/1  \nend")
	if strings.Join(got, "|") != "conf t| interface Gi1/0/1|end" {
		t.Errorf("SplitCommands = %q", got)
	}
	for seen, other := range map[string]string{
		"sw1#":          "sw1(config-if)#",
		"user@host:~$":  "user@host:/var/log$",
		"[root@srv ~]#": "[root@srv /etc]#",
		"router>":       "router#",
	} {
		if !promptFor(seen).MatchString(other) {
			t.Errorf("prompt %q should match %q", seen, other)
		}
	}
	if promptFor("sw1#").MatchString("line 5") {
		t.Error("output line matched prompt")
	}
	if !isCisco(DeviceAuto, "sw1#") || isCisco(DeviceAuto, "root@x:~#") || isCisco(DeviceOther, "sw1#") {
		t.Error("isCisco")
	}
}

func TestRunTypesSlowlyForReading(t *testing.T) {
	t.Parallel()
	d := newFakeDevice(true)
	defer d.Close()
	start := time.Now()
	res := run(t, d, "ls\nls", Options{TypeDelay: 40 * time.Millisecond, StepPause: 300 * time.Millisecond})
	took := time.Since(start)
	if !res.OK() || len(res.Steps) != 2 || res.Steps[1].Output != "a.txt  b.txt" {
		t.Fatalf("%+v", res)
	}
	// 2 × "ls" at 40 ms per char + one pause of 300 ms, on top of the 2 s prompt settle.
	if took < 2*time.Second+400*time.Millisecond {
		t.Errorf("typed too fast: %v", took)
	}
}
