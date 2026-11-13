package sshtest

import (
	"io"
	"net"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"
)

// DirectTCPIP serves a "direct-tcpip" channel (ssh -L / -J): it connects to
// the requested address and copies both ways. Used by the test server and
// tools/devsshd.
func DirectTCPIP(nch ssh.NewChannel) {
	var req struct {
		Host       string
		Port       uint32
		OriginHost string
		OriginPort uint32
	}
	if err := ssh.Unmarshal(nch.ExtraData(), &req); err != nil {
		nch.Reject(ssh.ConnectionFailed, "bad request")
		return
	}
	remote, err := net.Dial("tcp", net.JoinHostPort(req.Host, strconv.Itoa(int(req.Port))))
	if err != nil {
		nch.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	ch, reqs, err := nch.Accept()
	if err != nil {
		remote.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(ch, remote)
		ch.CloseWrite()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(remote, ch)
		if tc, ok := remote.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()
	wg.Wait()
	ch.Close()
	remote.Close()
}
