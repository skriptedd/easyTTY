package fwserver

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

// Event is one line in the firmware server log.
type Event struct {
	Time   int64  `json:"time"` // unix ms
	Proto  string `json:"proto"`
	Client string `json:"client"`
	File   string `json:"file"`
	State  string `json:"state"` // läuft | fertig | fehler | abgelehnt | info
	Done   int64  `json:"done"`
	Total  int64  `json:"total"`
	Detail string `json:"detail"`
	MD5    string `json:"md5,omitempty"`    // set on "fertig", for "verify /md5"
	Upload bool   `json:"upload,omitempty"` // the device sent this file to us
}

// Config selects folder, address and protocols. Port 0 = protocol off.
type Config struct {
	Dir      string `json:"dir"`
	BindIP   string `json:"bindIp"` // "" = all interfaces
	TFTPPort int    `json:"tftpPort"`
	HTTPPort int    `json:"httpPort"`
	SSHPort  int    `json:"sshPort"` // SCP + SFTP
	// UploadDir takes files that devices send by TFTP (copy running-config
	// tftp:). "" = read only. It must not lie inside Dir, which anyone may read.
	UploadDir string `json:"uploadDir"`
}

// Info describes a running server for the UI.
type Info struct {
	Running  bool   `json:"running"`
	Config   Config `json:"config"`
	User     string `json:"user"`     // SCP/SFTP login (random per start)
	Password string `json:"password"` // shown in the UI so the device can log in
	Started  int64  `json:"started"`
}

// Server runs the enabled protocols until Stop.
type Server struct {
	run     sync.Mutex // serialises Start/Stop
	mu      sync.Mutex
	info    Info
	closers []io.Closer
	log     func(Event)
}

// New creates a stopped server; log receives every event.
func New(log func(Event)) *Server {
	s := &Server{}
	s.log = func(e Event) {
		if e.Time == 0 {
			e.Time = time.Now().UnixMilli()
		}
		if e.State == "fertig" && e.File != "" {
			e.MD5 = s.md5Of(e.File)
		}
		log(e)
	}
	return s
}

// endState is "fertig" only when the whole file went out.
func endState(done, total int64) string {
	if done >= total {
		return "fertig"
	}
	return "abgebrochen"
}

// md5Of returns the MD5 of a served file ("" if unknown); cached by fileMD5.
func (s *Server) md5Of(name string) string {
	s.mu.Lock()
	root := s.info.Config.Dir
	s.mu.Unlock()
	if root == "" { // stopped meanwhile: never resolve against the working directory
		return ""
	}
	p, err := resolve(root, name)
	if err != nil {
		return ""
	}
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return ""
	}
	sum, _ := fileMD5(p, fi)
	return sum
}

// Info returns the current state.
func (s *Server) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info
}

// Start launches the enabled protocols (stopping a previous run first).
func (s *Server) Start(cfg Config) (Info, error) {
	s.run.Lock()
	defer s.run.Unlock()
	s.stop()
	fi, err := os.Stat(cfg.Dir)
	if err != nil || !fi.IsDir() {
		return Info{}, fmt.Errorf("Firmware-Ordner nicht gefunden: %s", cfg.Dir)
	}
	if cfg.TFTPPort == 0 && cfg.HTTPPort == 0 && cfg.SSHPort == 0 {
		return Info{}, errors.New("Mindestens ein Protokoll einschalten")
	}
	if cfg.BindIP != "" && net.ParseIP(cfg.BindIP) == nil {
		return Info{}, fmt.Errorf("ungültige IP: %s", cfg.BindIP)
	}
	if filepath.Dir(filepath.Clean(cfg.Dir)) == filepath.Clean(cfg.Dir) {
		return Info{}, errors.New("Ein ganzes Laufwerk freizugeben ist zu riskant – bitte einen Unterordner wählen")
	}
	if err := checkUploadDir(cfg); err != nil {
		return Info{}, err
	}
	pw, err := randomPassword()
	if err != nil {
		return Info{}, err
	}
	info := Info{Running: true, Config: cfg, User: "firmware", Password: pw, Started: time.Now().UnixMilli()}
	addr := func(port int) string { return net.JoinHostPort(cfg.BindIP, strconv.Itoa(port)) }
	var closers []io.Closer
	fail := func(err error) (Info, error) {
		for _, c := range closers {
			c.Close()
		}
		return Info{}, err
	}
	if cfg.TFTPPort > 0 {
		t, err := startTFTP(addr(cfg.TFTPPort), cfg.Dir, cfg.UploadDir, s.log)
		if err != nil {
			return fail(err)
		}
		closers = append(closers, t)
	}
	if cfg.HTTPPort > 0 {
		h, err := startHTTP(addr(cfg.HTTPPort), cfg.Dir, s.log)
		if err != nil {
			return fail(err)
		}
		closers = append(closers, h)
	}
	if cfg.SSHPort > 0 {
		x, err := startSSH(addr(cfg.SSHPort), cfg.Dir, info.User, info.Password, s.log)
		if err != nil {
			return fail(err)
		}
		closers = append(closers, x)
	}
	s.mu.Lock()
	s.info, s.closers = info, closers
	s.mu.Unlock()
	s.log(Event{Proto: "Server", State: "info", Detail: "gestartet: " + cfg.Dir})
	return info, nil
}

// Stop shuts all protocols down, including transfers still running.
func (s *Server) Stop() {
	s.run.Lock()
	defer s.run.Unlock()
	s.stop()
}

func (s *Server) stop() {
	s.mu.Lock()
	closers, running := s.closers, s.info.Running
	s.closers, s.info = nil, Info{}
	s.mu.Unlock()
	for _, c := range closers {
		c.Close()
	}
	if running {
		s.log(Event{Proto: "Server", State: "info", Detail: "gestoppt"})
	}
}

// rsaBits: older Cisco IOS (15.x) only accepts RSA host keys (ssh-rsa,
// rsa-sha2-256/512); newer clients pick ed25519.
const rsaBits = 2048

// hostKeys creates fresh ed25519 and RSA host keys for one server run.
func hostKeys() ([]ssh.Signer, error) {
	_, edKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		return nil, err
	}
	var out []ssh.Signer
	for _, k := range []any{edKey, rsaKey} {
		sg, err := ssh.NewSignerFromKey(k)
		if err != nil {
			return nil, err
		}
		out = append(out, sg)
	}
	return out, nil
}

func randomPassword() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

/* ---------- HTTP ---------- */

type httpServer struct{ srv *http.Server }

func (h httpServer) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := h.srv.Shutdown(ctx); err != nil {
		return h.srv.Close() // cut downloads that are still running
	}
	return nil
}

func startHTTP(addr, root string, log func(Event)) (io.Closer, error) {
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return nil, fmt.Errorf("HTTP %s: %w", addr, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		client, _, _ := net.SplitHostPort(r.RemoteAddr)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "nur Lesen", http.StatusMethodNotAllowed)
			log(Event{Proto: "HTTP", Client: client, File: r.URL.Path, State: "abgelehnt", Detail: r.Method})
			return
		}
		if r.URL.Path == "/" {
			serveIndex(w, root)
			return
		}
		f, fi, err := openFile(root, r.URL.Path)
		if err != nil {
			http.NotFound(w, r)
			if r.URL.Path == "/favicon.ico" {
				return // browsers ask for it; not worth a log line
			}
			log(Event{Proto: "HTTP", Client: client, File: r.URL.Path, State: "fehler", Detail: reason(err)})
			return
		}
		defer f.Close()
		ev := Event{Proto: "HTTP", Client: client, File: r.URL.Path, Total: fi.Size(), State: "läuft"}
		log(ev)
		cw := &countWriter{ResponseWriter: w, p: newProgress(ev, log)}
		http.ServeContent(cw, r, fi.Name(), fi.ModTime(), f)
		if r.Method == http.MethodHead {
			return
		}
		ev.Done, ev.State = cw.p.done(), endState(cw.p.done(), fi.Size())
		log(ev)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: time.Minute}
	go srv.Serve(ln)
	return httpServer{srv}, nil
}

type countWriter struct {
	http.ResponseWriter
	p *progress
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.ResponseWriter.Write(p)
	c.p.add(int64(n))
	return n, err
}

/* ---------- SSH: SCP source + read-only SFTP ---------- */

const (
	maxSSHConns    = 32
	maxSSHChannels = 8
)

type sshServer struct {
	ln    net.Listener
	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

// Close stops accepting and drops sessions that are still connected.
func (x *sshServer) Close() error {
	err := x.ln.Close()
	x.mu.Lock()
	for c := range x.conns {
		c.Close()
	}
	x.conns = nil
	x.mu.Unlock()
	return err
}

func (x *sshServer) track(c net.Conn) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.conns == nil || len(x.conns) >= maxSSHConns {
		return false
	}
	x.conns[c] = struct{}{}
	return true
}

func (x *sshServer) untrack(c net.Conn) {
	x.mu.Lock()
	delete(x.conns, c)
	x.mu.Unlock()
}

func startSSH(addr, root, user, password string, log func(Event)) (io.Closer, error) {
	signers, err := hostKeys()
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ServerConfig{
		MaxAuthTries: 3,
		PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			okUser := subtle.ConstantTimeCompare([]byte(c.User()), []byte(user))
			okPw := subtle.ConstantTimeCompare(pw, []byte(password))
			if okUser&okPw == 1 {
				return nil, nil
			}
			time.Sleep(500 * time.Millisecond) // slow down guessing
			host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
			log(Event{Proto: "SCP/SFTP", Client: host, State: "abgelehnt", Detail: "falsche Anmeldung als " + c.User()})
			return nil, errors.New("denied")
		},
	}
	for _, sg := range signers {
		cfg.AddHostKey(sg)
	}
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return nil, fmt.Errorf("SCP/SFTP %s: %w", addr, err)
	}
	x := &sshServer{ln: ln, conns: map[net.Conn]struct{}{}}
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			if !x.track(nc) {
				nc.Close()
				continue
			}
			go func() {
				defer x.untrack(nc)
				serveSSHConn(nc, cfg, root, log)
			}()
		}
	}()
	return x, nil
}

func serveSSHConn(nc net.Conn, cfg *ssh.ServerConfig, root string, log func(Event)) {
	_ = nc.SetDeadline(time.Now().Add(30 * time.Second)) // handshake only
	sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		nc.Close()
		return
	}
	_ = nc.SetDeadline(time.Time{})
	defer sc.Close()
	go ssh.DiscardRequests(reqs)
	client, _, _ := net.SplitHostPort(sc.RemoteAddr().String())
	var open atomic.Int32
	for nch := range chans {
		if nch.ChannelType() != "session" {
			nch.Reject(ssh.UnknownChannelType, "nur session")
			continue
		}
		if open.Load() >= maxSSHChannels {
			nch.Reject(ssh.ResourceShortage, "zu viele Kanäle")
			continue
		}
		ch, creqs, err := nch.Accept()
		if err != nil {
			continue
		}
		open.Add(1)
		go func() {
			defer open.Add(-1)
			serveSession(ch, creqs, root, client, log)
		}()
	}
}

// checkUploadDir: an upload folder must exist, be no drive root and lie
// outside the served folder (uploaded configs must not be readable by all).
func checkUploadDir(cfg Config) error {
	if cfg.UploadDir == "" {
		return nil
	}
	up, err := filepath.Abs(cfg.UploadDir)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(up); err != nil || !fi.IsDir() {
		return fmt.Errorf("Upload-Ordner nicht gefunden: %s", cfg.UploadDir)
	}
	if filepath.Dir(up) == up {
		return errors.New("Ein ganzes Laufwerk als Upload-Ordner ist zu riskant – bitte einen Unterordner wählen")
	}
	if cfg.TFTPPort == 0 {
		return errors.New("Uploads gehen nur per TFTP – TFTP einschalten")
	}
	served, err := filepath.Abs(cfg.Dir)
	if err != nil {
		return err
	}
	if rel, err := filepath.Rel(served, up); err == nil && !strings.HasPrefix(rel, "..") {
		return errors.New("Der Upload-Ordner darf nicht im Firmware-Ordner liegen, sonst kann jeder im Netz die Configs lesen")
	}
	return nil
}
