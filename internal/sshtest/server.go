// Package sshtest runs a minimal in-process SSH server for tests.
package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Server is a test SSH server. The shell echoes input back prefixed by "echo:".
type Server struct {
	Addr     string
	HostKey  ssh.PublicKey
	User     string
	Password string
	AuthKey  ssh.PublicKey // optional accepted client key
	// Exec answers "exec" requests (nil = reject them).
	Exec func(cmd string) string

	mu      sync.Mutex
	resizes [][2]int
	breaks  int
	ln      net.Listener
}

// Start launches a server accepting user/password (and AuthKey if set).
func Start(t *testing.T, user, password string, authKey ssh.PublicKey) *Server {
	return start(t, user, password, authKey, nil)
}

// StartLegacy launches a server that only speaks old algorithms, like
// console servers and older switches: CBC ciphers, SHA-1 key exchange and MACs.
func StartLegacy(t *testing.T, user, password string) *Server {
	return start(t, user, password, nil, func(c *ssh.ServerConfig) {
		c.Ciphers = []string{"aes128-cbc", "3des-cbc"}
		c.KeyExchanges = []string{"diffie-hellman-group14-sha1", "diffie-hellman-group1-sha1"}
		c.MACs = []string{"hmac-sha1"}
	})
}

func start(t *testing.T, user, password string, authKey ssh.PublicKey, tweak func(*ssh.ServerConfig)) *Server {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{HostKey: signer.PublicKey(), User: user, Password: password, AuthKey: authKey}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if c.User() == s.User && string(pw) == s.Password {
				return nil, nil
			}
			return nil, fmt.Errorf("denied")
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			if s.AuthKey != nil && c.User() == s.User && string(k.Marshal()) == string(s.AuthKey.Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("denied")
		},
	}
	if tweak != nil {
		tweak(cfg)
	}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.ln = ln
	s.Addr = ln.Addr().String()
	t.Cleanup(func() { ln.Close() })
	go s.serve(cfg)
	return s
}

// Resizes returns recorded window-change requests as [cols, rows].
func (s *Server) Resizes() [][2]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][2]int(nil), s.resizes...)
}

// Breaks returns the number of break requests received.
func (s *Server) Breaks() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.breaks
}

func (s *Server) serve(cfg *ssh.ServerConfig) {
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			return
		}
		go func() {
			_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
			if err != nil {
				nc.Close()
				return
			}
			go ssh.DiscardRequests(reqs)
			for nch := range chans {
				if nch.ChannelType() == "direct-tcpip" {
					go DirectTCPIP(nch)
					continue
				}
				if nch.ChannelType() != "session" {
					nch.Reject(ssh.UnknownChannelType, "nope")
					continue
				}
				ch, creqs, err := nch.Accept()
				if err != nil {
					continue
				}
				go s.session(ch, creqs)
			}
		}()
	}
}

func (s *Server) session(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			req.Reply(true, nil)
		case "window-change":
			if len(req.Payload) >= 8 {
				cols := int(binary.BigEndian.Uint32(req.Payload[0:]))
				rows := int(binary.BigEndian.Uint32(req.Payload[4:]))
				s.mu.Lock()
				s.resizes = append(s.resizes, [2]int{cols, rows})
				s.mu.Unlock()
			}
		case "break":
			s.mu.Lock()
			s.breaks++
			s.mu.Unlock()
			req.Reply(true, nil)
		case "shell":
			req.Reply(true, nil)
			go func() {
				buf := make([]byte, 4096)
				for {
					n, err := ch.Read(buf)
					if n > 0 {
						if string(buf[:n]) == "exit\n" {
							ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
							ch.Close()
							return
						}
						ch.Write(append([]byte("echo:"), buf[:n]...))
					}
					if err != nil {
						return
					}
				}
			}()
		case "exec":
			if s.Exec == nil || len(req.Payload) < 4 {
				req.Reply(false, nil)
				continue
			}
			req.Reply(true, nil)
			ch.Write([]byte(s.Exec(string(req.Payload[4:]))))
			ch.CloseWrite() // EOF before close, like a real server
			ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
			return
		case "subsystem":
			if len(req.Payload) > 4 && string(req.Payload[4:]) == "sftp" {
				req.Reply(true, nil)
				srv, err := sftp.NewServer(ch)
				if err != nil {
					return
				}
				_ = srv.Serve()
				if err := srv.Close(); err != nil && err != io.EOF {
					return
				}
				return
			}
			req.Reply(false, nil)
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}
