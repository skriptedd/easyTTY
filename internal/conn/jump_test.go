package conn

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"easytty/internal/sshtest"
)

// trustAll accepts exactly the host keys of the given test servers.
func trustAll(servers ...*sshtest.Server) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		for _, s := range servers {
			if bytes.Equal(key.Marshal(), s.HostKey.Marshal()) {
				return nil
			}
		}
		return errors.New("unknown host key")
	}
}

func sshCfg(t *testing.T, s *sshtest.Server, user, pw string) SSHConfig {
	t.Helper()
	host, port := hostPort(t, s.Addr)
	return SSHConfig{Host: host, Port: port, User: user, Auth: "password", Password: pw}
}

func TestDialThroughJumpHost(t *testing.T) {
	jump := sshtest.Start(t, "hop", "hop-pw", nil)
	target := sshtest.Start(t, "admin", "s3cret", nil)
	cfg := sshCfg(t, target, "admin", "s3cret")
	j := sshCfg(t, jump, "hop", "hop-pw")
	cfg.Jump = &j
	d := Dialer{HostKeys: trustAll(jump, target), HomeDir: t.TempDir()}

	c, err := d.Dial(context.Background(), cfg)

	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("hallo\r")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, c, "hallo")
}

func TestJumpHostPromptIsMarked(t *testing.T) {
	jump := sshtest.Start(t, "hop", "hop-pw", nil)
	target := sshtest.Start(t, "admin", "s3cret", nil)
	cfg := sshCfg(t, target, "admin", "s3cret")
	j := sshCfg(t, jump, "hop", "") // no stored password: asks
	cfg.Jump = &j
	var mu sync.Mutex
	var asked []PromptRequest
	d := Dialer{HostKeys: trustAll(jump, target), HomeDir: t.TempDir(), Ask: func(r PromptRequest) ([]string, bool) {
		mu.Lock()
		asked = append(asked, r)
		mu.Unlock()
		return []string{"hop-pw"}, true
	}}

	c, err := d.Dial(context.Background(), cfg)

	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if len(asked) != 1 || !asked[0].Jump || !strings.HasPrefix(asked[0].Title, "Sprung-Host ") {
		t.Errorf("prompts = %+v", asked)
	}
}

func TestJumpHostFailureNamesTheJumpHost(t *testing.T) {
	jump := sshtest.Start(t, "hop", "hop-pw", nil)
	target := sshtest.Start(t, "admin", "s3cret", nil)
	cfg := sshCfg(t, target, "admin", "s3cret")
	j := sshCfg(t, jump, "hop", "wrong")
	cfg.Jump = &j
	d := Dialer{HostKeys: trustAll(jump, target), HomeDir: t.TempDir()}

	_, err := d.Dial(context.Background(), cfg)

	if err == nil || !strings.Contains(err.Error(), "Sprung-Host") {
		t.Errorf("err = %v", err)
	}
}

// echoServer answers every TCP connection with what it receives.
func echoServer(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(c, c); c.Close() }()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestLocalForwardTunnelsAndStopsWithConnection(t *testing.T) {
	srv := sshtest.Start(t, "admin", "s3cret", nil)
	d := Dialer{HostKeys: trustAll(srv), HomeDir: t.TempDir()}
	c, err := d.Dial(context.Background(), sshCfg(t, srv, "admin", "s3cret"))
	if err != nil {
		t.Fatal(err)
	}
	listen := freePort(t)

	started, errs := c.Forward([]LocalForward{{Listen: listen, Host: "127.0.0.1", Port: echoServer(t)}})

	if len(errs) != 0 || len(started) != 1 {
		t.Fatalf("started %v, errors %v", started, errs)
	}
	local, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(listen)), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	_ = local.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(local, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("read %q: %v", buf, err)
	}

	c.Close()

	_ = local.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := local.Read(buf); err == nil {
		t.Error("tunnel connection still open after Close")
	}
	local.Close()
	if again, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(listen)), time.Second); err == nil {
		again.Close()
		t.Error("forward still listening after Close")
	}
}

func TestLocalForwardReportsBusyPort(t *testing.T) {
	srv := sshtest.Start(t, "admin", "s3cret", nil)
	d := Dialer{HostKeys: trustAll(srv), HomeDir: t.TempDir()}
	c, err := d.Dial(context.Background(), sshCfg(t, srv, "admin", "s3cret"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	busyPort := busy.Addr().(*net.TCPAddr).Port

	started, errs := c.Forward([]LocalForward{
		{Listen: busyPort, Host: "127.0.0.1", Port: 1},
		{Listen: freePort(t), Host: "127.0.0.1", Port: 1},
	})

	if len(errs) != 1 || len(started) != 1 {
		t.Errorf("started %v, errors %v", started, errs)
	}
}

func TestSilentTargetBehindJumpHostTimesOut(t *testing.T) {
	jump := sshtest.Start(t, "hop", "hop-pw", nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close() // accepts, never speaks SSH
		}
	}()
	host, port := hostPort(t, ln.Addr().String())
	j := sshCfg(t, jump, "hop", "hop-pw")
	d := Dialer{HostKeys: trustAll(jump), HomeDir: t.TempDir(), HandshakeTimeout: 200 * time.Millisecond}
	start := time.Now()

	_, err = d.Dial(context.Background(), SSHConfig{Host: host, Port: port, User: "u", Jump: &j})

	if err == nil || !strings.Contains(err.Error(), "antwortet nicht") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout took too long")
	}
}
