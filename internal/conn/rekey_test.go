package conn

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"easytty/internal/sshtest"
)

// A rekey after the login must not re-arm the login deadline (it used to
// close the connection HandshakeTimeout later).
func TestRekeyKeepsSessionAlive(t *testing.T) {
	srv := sshtest.StartRekey(t, "admin", "pw")
	host, port := hostPort(t, srv.Addr)
	var calls atomic.Int32
	d := Dialer{HostKeys: func(string, net.Addr, ssh.PublicKey) error { calls.Add(1); return nil }, HandshakeTimeout: time.Second}
	c, err := d.Dial(context.Background(), SSHConfig{Host: host, Port: port, User: "admin", Auth: "password", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	errc := make(chan error, 1)
	start := time.Now()
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := c.Read(buf); err != nil {
				errc <- err
				return
			}
		}
	}()
	c.Write([]byte(strings.Repeat("x", 600) + "\n"))
	for i := 0; i < 30; i++ {
		select {
		case err := <-errc:
			t.Fatalf("session died after %v (host key callbacks: %d): %v", time.Since(start), calls.Load(), err)
		case <-time.After(100 * time.Millisecond):
		}
		c.Write([]byte("hi\n"))
	}
	if calls.Load() < 2 {
		t.Fatalf("no rekey happened (host key callbacks: %d)", calls.Load())
	}
}
