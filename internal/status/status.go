// Package status reads live health data (load, memory, disk, processes)
// from the device behind an SSH connection, via separate exec channels so
// nothing is typed into the user's shell.
package status

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Kinds of devices.
const (
	KindLinux = "linux"
	KindCisco = "cisco"
)

// Proc is one top process.
type Proc struct {
	PID  string  `json:"pid"`
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"` // percent
	Mem  float64 `json:"mem"` // percent (Linux) or 0
}

// Disk is one mounted filesystem.
type Disk struct {
	Mount string `json:"mount"`
	Total int64  `json:"total"` // bytes
	Used  int64  `json:"used"`
}

// Snapshot is one status reading.
type Snapshot struct {
	Kind      string    `json:"kind"`
	Host      string    `json:"host"`
	OS        string    `json:"os"`
	UptimeSec int64     `json:"uptimeSec"`
	Uptime    string    `json:"uptime"` // as reported (Cisco)
	Load      []float64 `json:"load"`   // 1, 5, 15 min (Linux)
	CPUs      int       `json:"cpus"`
	CPU       []float64 `json:"cpu"` // 5 s, 1 min, 5 min in % (Cisco)
	MemTotal  int64     `json:"memTotal"`
	MemUsed   int64     `json:"memUsed"` // bytes
	SwapTotal int64     `json:"swapTotal"`
	SwapUsed  int64     `json:"swapUsed"`
	CPUUsage  float64   `json:"cpuUsage"` // whole machine in % over the last second (Linux), -1 = unknown
	Disks     []Disk    `json:"disks"`
	Procs     []Proc    `json:"procs"`
	Taken     int64     `json:"taken"` // unix ms
}

const (
	execTimeout = 8 * time.Second
	maxProcs    = 8
	maxOutput   = 256 * 1024
)

// Cisco IOS commands, each run on its own exec channel.
const (
	CiscoCPU    = "show processes cpu sorted"
	CiscoMemory = "show processes memory sorted"
	CiscoUptime = "show version | include uptime"
)

// Execer runs one command and returns its output (abstracted for tests).
type Execer func(ctx context.Context, cmd string) (string, error)

// SSHExec runs commands on new session channels of an existing client.
func SSHExec(c *ssh.Client) Execer {
	return func(ctx context.Context, cmd string) (string, error) {
		sess, err := c.NewSession()
		if err != nil {
			return "", err
		}
		defer sess.Close()
		var out limitedBuffer
		sess.Stdout = &out
		sess.Stderr = &out
		done := make(chan error, 1)
		go func() { done <- sess.Run(cmd) }()
		select {
		case err := <-done:
			var exitErr *ssh.ExitError
			if err != nil && !errors.As(err, &exitErr) {
				return out.String(), err
			}
			return out.String(), nil // non-zero exit still gives usable output
		case <-ctx.Done():
			sess.Close()
			return out.String(), ctx.Err()
		}
	}
}

// limitedBuffer caps output and is safe for the concurrent stdout/stderr
// copy goroutines of an ssh.Session (an unguarded bytes.Buffer lost data).
type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := maxOutput - b.buf.Len(); room < len(p) {
		if room > 0 {
			b.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *limitedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

// Collect reads a snapshot. kind "" auto-detects (Linux first, then Cisco)
// and returns the detected kind so callers can cache it.
func Collect(ctx context.Context, run Execer, kind string) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()
	if kind == "" || kind == KindLinux {
		out, err := run(ctx, LinuxScript)
		if s, ok := ParseLinux(out); ok {
			return stamp(s), nil
		}
		if kind == KindLinux {
			if err == nil {
				err = errors.New("keine Statusdaten erhalten")
			}
			return Snapshot{}, err
		}
	}
	cpu, err := run(ctx, CiscoCPU)
	if err != nil {
		return Snapshot{}, fmt.Errorf("Status nicht lesbar (weder Linux noch Cisco IOS): %w", err)
	}
	mem, _ := run(ctx, CiscoMemory)
	up, _ := run(ctx, CiscoUptime)
	s, ok := ParseCisco(cpu, mem, up)
	if !ok {
		return Snapshot{}, errors.New("Status nicht lesbar: Gerätetyp nicht erkannt (unterstützt: Linux, Cisco IOS/IOS-XE)")
	}
	return stamp(s), nil
}

func stamp(s Snapshot) Snapshot {
	s.Taken = time.Now().UnixMilli()
	return s
}

// sections splits "==NAME" marked output.
func sections(out string) map[string][]string {
	m := map[string][]string{}
	cur := ""
	for _, raw := range strings.Split(strings.ReplaceAll(out, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "==") {
			cur = strings.TrimPrefix(line, "==")
			m[cur] = nil
			continue
		}
		if cur != "" && line != "" {
			m[cur] = append(m[cur], line)
		}
	}
	return m
}

// ParseCisco parses IOS "show processes cpu/memory sorted" and uptime output.
func ParseCisco(cpuOut, memOut, upOut string) (Snapshot, bool) {
	s := Snapshot{Kind: KindCisco, OS: "Cisco IOS"}
	found := false
	inTable := false
	for _, raw := range strings.Split(strings.ReplaceAll(cpuOut, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "CPU utilization for five seconds:") {
			s.CPU = parseCiscoCPU(line)
			found = len(s.CPU) == 3
			continue
		}
		if strings.HasPrefix(line, "PID") && strings.Contains(line, "Process") {
			inTable = true
			continue
		}
		if !inTable || len(s.Procs) >= maxProcs {
			continue
		}
		ff := strings.Fields(line)
		// PID Runtime(ms) Invoked uSecs 5Sec 1Min 5Min TTY Process
		if len(ff) < 9 {
			continue
		}
		cpu, err := strconv.ParseFloat(strings.TrimSuffix(ff[4], "%"), 64)
		if err != nil {
			continue
		}
		s.Procs = append(s.Procs, Proc{PID: ff[0], Name: strings.Join(ff[8:], " "), CPU: cpu})
	}
	if !found {
		return Snapshot{}, false
	}
	for _, raw := range strings.Split(strings.ReplaceAll(memOut, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		// "Processor Pool Total:  329154620 Used:  156403952 Free:  172750668"
		if strings.HasPrefix(line, "Processor Pool Total:") || strings.HasPrefix(line, "System memory") {
			ff := strings.Fields(strings.NewReplacer(":", " ").Replace(line))
			for i := 0; i+1 < len(ff); i++ {
				v, err := strconv.ParseInt(ff[i+1], 10, 64)
				if err != nil {
					continue
				}
				switch ff[i] {
				case "Total":
					s.MemTotal = v
				case "Used":
					s.MemUsed = v
				}
			}
			break
		}
	}
	for _, raw := range strings.Split(strings.ReplaceAll(upOut, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		if i := strings.Index(line, " uptime is "); i > 0 {
			s.Host = line[:i]
			s.Uptime = line[i+len(" uptime is "):]
			break
		}
	}
	return s, true
}

// "CPU utilization for five seconds: 5%/0%; one minute: 6%; five minutes: 5%"
func parseCiscoCPU(line string) []float64 {
	var out []float64
	for _, part := range strings.Split(line, ";") {
		i := strings.LastIndex(part, ":")
		if i < 0 {
			continue
		}
		v := strings.TrimSpace(part[i+1:])
		if j := strings.Index(v, "%"); j >= 0 {
			v = v[:j]
		}
		x, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil
		}
		out = append(out, x)
	}
	return out
}
