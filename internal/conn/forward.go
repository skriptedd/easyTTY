package conn

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"
)

// LocalForward makes 127.0.0.1:Listen reach Host:Port from the SSH server (ssh -L).
type LocalForward struct {
	Listen int
	Host   string
	Port   int
}

func (f LocalForward) String() string {
	return fmt.Sprintf("127.0.0.1:%d → %s", f.Listen, net.JoinHostPort(f.Host, strconv.Itoa(f.Port)))
}

// forwarder is one listening tunnel.
type forwarder struct {
	ln     net.Listener
	client *ssh.Client
	target string

	mu    sync.Mutex
	conns map[net.Conn]struct{}
	done  bool
}

// Forward starts the tunnels on this connection. They listen on 127.0.0.1
// only, so nobody else in the network can use them, and stop with Close.
// A port that cannot be opened is reported and skipped.
func (c *SSHConn) Forward(fs []LocalForward) (started []LocalForward, errs []error) {
	for _, f := range fs {
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(f.Listen)))
		if err != nil {
			errs = append(errs, fmt.Errorf("Weiterleitung %s: %w", f, err))
			continue
		}
		fw := &forwarder{ln: ln, client: c.client, target: net.JoinHostPort(f.Host, strconv.Itoa(f.Port)), conns: map[net.Conn]struct{}{}}
		if !c.addCloser(fw) {
			ln.Close()
			return started, append(errs, errors.New("Verbindung schon getrennt"))
		}
		go fw.serve()
		started = append(started, f)
	}
	return started, errs
}

func (fw *forwarder) serve() {
	for {
		local, err := fw.ln.Accept()
		if err != nil {
			return // listener closed
		}
		if !fw.track(local) {
			local.Close()
			return
		}
		go fw.pipe(local)
	}
}

func (fw *forwarder) pipe(local net.Conn) {
	defer fw.untrack(local)
	remote, err := fw.client.Dial("tcp", fw.target)
	if err != nil {
		local.Close()
		return
	}
	var wg sync.WaitGroup
	copyHalf := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		// One side ended: end both, so the other copy returns too.
		dst.Close()
		src.Close()
	}
	wg.Add(2)
	go copyHalf(remote, local)
	go copyHalf(local, remote)
	wg.Wait()
}

func (fw *forwarder) track(c net.Conn) bool {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if fw.done {
		return false
	}
	fw.conns[c] = struct{}{}
	return true
}

func (fw *forwarder) untrack(c net.Conn) {
	fw.mu.Lock()
	delete(fw.conns, c)
	fw.mu.Unlock()
}

// Close stops listening and ends every open tunnel connection.
func (fw *forwarder) Close() error {
	fw.mu.Lock()
	fw.done = true
	open := make([]net.Conn, 0, len(fw.conns))
	for c := range fw.conns {
		open = append(open, c)
	}
	fw.mu.Unlock()
	err := fw.ln.Close()
	for _, c := range open {
		c.Close()
	}
	return err
}
