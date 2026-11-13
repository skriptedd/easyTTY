package conn

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const (
	dialTimeout      = 15 * time.Second
	handshakeTimeout = 20 * time.Second
	passwordAttempts = 3
	termType         = "xterm-256color"
	breakMillis      = 500
)

// PromptField is one input in a user prompt.
type PromptField struct {
	Label  string `json:"label"`
	Secret bool   `json:"secret"`
	Value  string `json:"value"`
}

// PromptRequest asks the user for input (password, passphrase, username...).
type PromptRequest struct {
	Kind    string        `json:"kind"`
	Title   string        `json:"title"`
	Message string        `json:"message"`
	Fields  []PromptField `json:"fields"`
	// Jump: asked for a jump host, not for the target (its password is not the target's).
	Jump bool `json:"-"`
}

// AskFunc shows a prompt and returns the answers; ok=false means cancelled.
type AskFunc func(PromptRequest) (answers []string, ok bool)

// SSHConfig describes one SSH connection.
type SSHConfig struct {
	Host      string
	Port      int
	User      string
	Auth      string // auto | password | key | agent
	KeyPath   string
	Password  string // stored password, tried once before prompting
	KeepAlive time.Duration
	Cols      int
	Rows      int
	// Jump is the host to connect through (ssh -J); it may have its own Jump.
	Jump *SSHConfig
}

// Dialer opens SSH connections.
type Dialer struct {
	HostKeys  ssh.HostKeyCallback
	Ask       AskFunc
	AgentDial func() (net.Conn, error) // nil = no agent
	HomeDir   string                   // for default keys (~/.ssh/id_*)
	// HostKeyAlgorithms (optional) pins accepted key types for known hosts.
	HostKeyAlgorithms func(addr string) []string
	// HandshakeTimeout limits waiting on a silent server; time spent in
	// user prompts does not count. 0 = default.
	HandshakeTimeout time.Duration
}

// SSHConn is an interactive shell over SSH.
type SSHConn struct {
	client    *ssh.Client
	session   *ssh.Session
	stdin     io.WriteCloser
	out       *io.PipeReader
	closeOnce sync.Once
	done      chan struct{}

	mu      sync.Mutex
	closers []io.Closer // port forwards, ended with the connection
	closed  bool
}

// addCloser registers something to stop on Close; false if already closed.
func (c *SSHConn) addCloser(cl io.Closer) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.closers = append(c.closers, cl)
	return true
}

var errCancelled = errors.New("abgebrochen")

// Dial connects, authenticates and starts a shell with a PTY.
func (d Dialer) Dial(ctx context.Context, cfg SSHConfig) (*SSHConn, error) {
	client, err := d.DialClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	c, err := startShell(client, cfg)
	if err != nil {
		client.Close()
		return nil, err
	}
	if cfg.KeepAlive > 0 {
		go c.keepAlive(cfg.KeepAlive)
	}
	return c, nil
}

// DialClient connects and authenticates without starting a shell.
func (d Dialer) DialClient(ctx context.Context, cfg SSHConfig) (*ssh.Client, error) {
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.User == "" {
		ans, ok := d.ask(PromptRequest{Kind: "username", Title: "Benutzername", Message: "Anmelden an " + cfg.Host,
			Fields: []PromptField{{Label: "Benutzer"}}})
		if !ok || strings.TrimSpace(ans[0]) == "" {
			return nil, errCancelled
		}
		cfg.User = strings.TrimSpace(ans[0])
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	if d.HostKeys == nil {
		return nil, errors.New("keine Hostkey-Prüfung konfiguriert")
	}
	if cfg.Auth == "key" || cfg.Auth == "agent" {
		cfg.Password = "" // never offer a stored password when key auth was chosen
	}

	raw, via, err := d.open(ctx, cfg, addr)
	if err != nil {
		return nil, err
	}
	if via != nil {
		defer func() {
			if err != nil {
				via.Close()
			}
		}()
	}
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()

	// Deadline against silent servers, paused while the user answers a prompt.
	timeout := d.HandshakeTimeout
	if timeout <= 0 {
		timeout = handshakeTimeout
	}
	// A timer on top of the deadline: the channel through a jump host has no
	// deadlines, there only closing it ends a handshake that hangs.
	var timedOut atomic.Bool
	watch := time.AfterFunc(timeout, func() {
		timedOut.Store(true)
		raw.Close()
	})
	watch.Stop()
	defer watch.Stop()
	arm := func() {
		_ = raw.SetDeadline(time.Now().Add(timeout))
		watch.Reset(timeout)
	}
	pause := func() {
		_ = raw.SetDeadline(time.Time{})
		watch.Stop()
	}
	paused := d
	paused.Ask = func(r PromptRequest) ([]string, bool) {
		pause()
		defer arm()
		return d.ask(r)
	}
	hostKeys := func(host string, remote net.Addr, key ssh.PublicKey) error {
		pause()
		defer arm()
		return d.HostKeys(host, remote, key)
	}

	var agentConn net.Conn
	if d.AgentDial != nil && (cfg.Auth == "auto" || cfg.Auth == "agent" || cfg.Auth == "") {
		if ac, err := d.AgentDial(); err == nil {
			agentConn = ac
			defer ac.Close()
		}
	}
	var keyErr atomic.Pointer[error] // why the chosen key could not be used
	config := &ssh.ClientConfig{
		Config:            legacyFriendly(),
		User:              cfg.User,
		Auth:              paused.authMethods(cfg, agentConn, &keyErr),
		HostKeyCallback:   hostKeys,
		HostKeyAlgorithms: withLegacy(ssh.SupportedAlgorithms().HostKeys, ssh.InsecureAlgorithms().HostKeys),
	}
	if d.HostKeyAlgorithms != nil {
		if pinned := d.HostKeyAlgorithms(addr); len(pinned) > 0 {
			config.HostKeyAlgorithms = pinned
		}
	}

	arm()
	sc, chans, reqs, hsErr := ssh.NewClientConn(raw, addr, config)
	watch.Stop()
	if hsErr == nil && timedOut.Load() { // the timer closed the connection just as the handshake ended
		hsErr = errors.New("Zeitüberschreitung")
	}
	if err = hsErr; err != nil {
		raw.Close()
		if ctx.Err() != nil {
			return nil, errCancelled
		}
		if timedOut.Load() {
			return nil, fmt.Errorf("SSH-Anmeldung an %s: Server antwortet nicht", addr)
		}
		if ke := keyErr.Load(); ke != nil {
			return nil, fmt.Errorf("SSH-Anmeldung an %s: %w", addr, *ke)
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, fmt.Errorf("SSH-Anmeldung an %s: Server antwortet nicht", addr)
		}
		if strings.Contains(err.Error(), "no common algorithm for host key") {
			return nil, fmt.Errorf("SSH-Anmeldung an %s: Der Server bietet keinen Hostkey vom gespeicherten Typ mehr an. Wurde er neu aufgesetzt, den alten Eintrag aus known_hosts entfernen. (%w)", addr, err)
		}
		return nil, fmt.Errorf("SSH-Anmeldung an %s: %w", addr, err)
	}
	_ = raw.SetDeadline(time.Time{})
	client := ssh.NewClient(sc, chans, reqs)
	if via != nil {
		// The jump connection lives exactly as long as the one through it.
		go func() {
			_ = client.Wait()
			via.Close()
		}()
	}
	return client, nil
}

// open returns the transport to addr: a TCP connection, or a channel through
// the jump host (via, to be closed with the connection that uses it).
func (d Dialer) open(ctx context.Context, cfg SSHConfig, addr string) (net.Conn, *ssh.Client, error) {
	if cfg.Jump == nil {
		nd := net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
		raw, err := nd.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, nil, fmt.Errorf("Verbindung zu %s fehlgeschlagen: %w", addr, err)
		}
		return raw, nil, nil
	}
	hop := d
	hop.Ask = func(r PromptRequest) ([]string, bool) {
		r.Jump = true
		r.Title = "Sprung-Host " + cfg.Jump.Host + ": " + r.Title
		return d.ask(r)
	}
	via, err := hop.DialClient(ctx, *cfg.Jump)
	if err != nil {
		return nil, nil, fmt.Errorf("Sprung-Host %s: %w", cfg.Jump.Host, err)
	}
	dctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	raw, err := via.DialContext(dctx, "tcp", addr)
	if err != nil {
		via.Close()
		return nil, nil, fmt.Errorf("Verbindung von %s zu %s fehlgeschlagen: %w", cfg.Jump.Host, addr, err)
	}
	return raw, via, nil
}

func (d Dialer) ask(req PromptRequest) ([]string, bool) {
	if d.Ask == nil {
		return nil, false
	}
	ans, ok := d.Ask(req)
	if ok && len(ans) < len(req.Fields) {
		return nil, false
	}
	return ans, ok
}

func (d Dialer) authMethods(cfg SSHConfig, agentConn net.Conn, keyErr *atomic.Pointer[error]) []ssh.AuthMethod {
	var methods []ssh.AuthMethod
	var signers []func() ([]ssh.Signer, error)

	if agentConn != nil {
		signers = append(signers, agent.NewClient(agentConn).Signers)
	}
	switch cfg.Auth {
	case "key":
		path := expandHome(cfg.KeyPath, d.HomeDir)
		signers = append(signers, func() ([]ssh.Signer, error) { return d.loadKey(path, true) })
	case "auto", "":
		if d.HomeDir == "" {
			break // no home: never fall back to .ssh below the working directory
		}
		for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			path := filepath.Join(d.HomeDir, ".ssh", name)
			signers = append(signers, func() ([]ssh.Signer, error) { return d.loadKey(path, false) })
		}
	}
	if cfg.Auth != "password" && len(signers) > 0 {
		methods = append(methods, ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			var all []ssh.Signer
			for _, get := range signers {
				s, err := get()
				if err != nil && (cfg.Auth == "key" || errors.Is(err, errCancelled)) {
					keyErr.Store(&err) // a chosen key that fails must say so, not end in a password prompt
					return nil, err
				}
				all = append(all, s...)
			}
			return all, nil
		}))
	}

	usedStored := cfg.Password == ""
	var mu sync.Mutex
	takeStored := func() (string, bool) {
		mu.Lock()
		defer mu.Unlock()
		if usedStored {
			return "", false
		}
		usedStored = true
		return cfg.Password, true
	}
	target := cfg.User + "@" + cfg.Host

	methods = append(methods, ssh.RetryableAuthMethod(ssh.KeyboardInteractive(
		func(name, instruction string, questions []string, echos []bool) ([]string, error) {
			if ke := keyErr.Load(); ke != nil {
				return nil, *ke
			}
			if len(questions) == 0 {
				return nil, nil
			}
			if len(questions) == 1 && !echos[0] {
				if pw, ok := takeStored(); ok {
					return []string{pw}, nil
				}
			}
			fields := make([]PromptField, len(questions))
			for i, q := range questions {
				fields[i] = PromptField{Label: strings.TrimSpace(q), Secret: !echos[i]}
			}
			msg := strings.TrimSpace(strings.Join([]string{target, name, instruction}, "\n"))
			ans, ok := d.ask(PromptRequest{Kind: interactiveKind(questions, echos), Title: "Anmeldung", Message: msg, Fields: fields})
			if !ok {
				return nil, errCancelled
			}
			return ans, nil
		}), passwordAttempts))

	methods = append(methods, ssh.RetryableAuthMethod(ssh.PasswordCallback(func() (string, error) {
		if ke := keyErr.Load(); ke != nil {
			return "", *ke
		}
		if pw, ok := takeStored(); ok {
			return pw, nil
		}
		ans, ok := d.ask(PromptRequest{Kind: "password", Title: "Passwort", Message: target,
			Fields: []PromptField{{Label: "Passwort", Secret: true}}})
		if !ok {
			return "", errCancelled
		}
		return ans[0], nil
	}), passwordAttempts))
	return methods
}

// loadKey reads a private key; encrypted keys prompt for a passphrase if interactive.
func (d Dialer) loadKey(path string, interactive bool) ([]ssh.Signer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(raw)
	var missing *ssh.PassphraseMissingError
	if errors.As(err, &missing) {
		if !interactive {
			return nil, err
		}
		for range passwordAttempts {
			ans, ok := d.ask(PromptRequest{Kind: "passphrase", Title: "Schlüssel-Passphrase", Message: path,
				Fields: []PromptField{{Label: "Passphrase", Secret: true}}})
			if !ok {
				return nil, errCancelled
			}
			signer, err = ssh.ParsePrivateKeyWithPassphrase(raw, []byte(ans[0]))
			if err == nil {
				break
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("Schlüssel %s: %w", path, err)
	}
	return []ssh.Signer{signer}, nil
}

func expandHome(p, home string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		return filepath.Join(home, p[1:])
	}
	return p
}

func startShell(client *ssh.Client, cfg SSHConfig) (*SSHConn, error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	cols, rows := cfg.Cols, cfg.Rows
	if cols <= 0 || rows <= 0 {
		cols, rows = 120, 32
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 115200, ssh.TTY_OP_OSPEED: 115200}
	if err := sess.RequestPty(termType, rows, cols, modes); err != nil {
		sess.Close()
		return nil, fmt.Errorf("PTY anfordern: %w", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		return nil, err
	}
	pr, pw := io.Pipe()
	sess.Stdout = pw
	sess.Stderr = pw
	if err := sess.Shell(); err != nil {
		sess.Close()
		return nil, fmt.Errorf("Shell starten: %w", err)
	}
	c := &SSHConn{client: client, session: sess, stdin: stdin, out: pr, done: make(chan struct{})}
	go func() { pw.CloseWithError(shellExitError(sess.Wait())) }()
	return c, nil
}

// shellExitError maps the shell's end to io.EOF for normal exits.
func shellExitError(err error) error {
	var missing *ssh.ExitMissingError
	var exit *ssh.ExitError
	switch {
	case err == nil, errors.As(err, &missing):
		return io.EOF
	case errors.As(err, &exit) && exit.Signal() == "":
		return fmt.Errorf("Shell beendet (Exit-Code %d)", exit.ExitStatus())
	}
	return err
}

func (c *SSHConn) keepAlive(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			if err := pingWithin(keepAliveWait(every), func() error {
				_, _, err := c.client.SendRequest("keepalive@openssh.com", true, nil)
				return err
			}); err != nil {
				c.Close()
				return
			}
		}
	}
}

// Read returns shell output (stdout and stderr merged).
func (c *SSHConn) Read(p []byte) (int, error) {
	n, err := c.out.Read(p)
	if errors.Is(err, io.ErrClosedPipe) {
		err = io.EOF // closed by us: a normal end, not "read/write on closed pipe"
	}
	return n, err
}

// Write sends input to the shell.
func (c *SSHConn) Write(p []byte) (int, error) { return c.stdin.Write(p) }

// Resize updates the remote PTY size.
func (c *SSHConn) Resize(cols, rows int) error { return c.session.WindowChange(rows, cols) }

// SendBreak sends an RFC 4335 break (e.g. for serial console servers).
func (c *SSHConn) SendBreak() error {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, breakMillis)
	_, err := c.session.SendRequest("break", true, payload)
	return err
}

// Client exposes the underlying SSH client (used for SFTP).
func (c *SSHConn) Client() *ssh.Client { return c.client }

// Close tears down the session and connection.
func (c *SSHConn) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		closers := c.closers
		c.mu.Unlock()
		for _, cl := range closers {
			cl.Close()
		}
		close(c.done)
		c.out.Close() // a copy goroutine blocked on pw.Write returns, so sess.Wait can end
		c.session.Close()
		c.client.Close()
	})
	return nil
}

// legacyFriendly offers the old algorithms of console servers and older
// switches (CBC ciphers, SHA-1 key exchange and MACs) after the modern ones.
// The client's order decides, so modern servers still get modern algorithms,
// and the negotiation is signed – an attacker cannot force the old ones.
func legacyFriendly() ssh.Config {
	sup, old := ssh.SupportedAlgorithms(), ssh.InsecureAlgorithms()
	return ssh.Config{
		Ciphers:      withLegacy(sup.Ciphers, old.Ciphers),
		KeyExchanges: withLegacy(sup.KeyExchanges, old.KeyExchanges),
		MACs:         withLegacy(sup.MACs, old.MACs),
	}
}

func withLegacy(modern, legacy []string) []string {
	out := slices.Clone(modern)
	for _, a := range legacy {
		if !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	return out
}

// interactiveKind tells a keyboard-interactive password question ("password")
// apart from one-time codes and other challenges ("challenge"), so only real
// passwords are offered for saving.
func interactiveKind(questions []string, echos []bool) string {
	if len(questions) != 1 || echos[0] {
		return "challenge"
	}
	q := strings.ToLower(questions[0])
	for _, w := range []string{"passw", "kennw"} {
		if strings.Contains(q, w) && !strings.Contains(q, "one-time") && !strings.Contains(q, "otp") {
			return "password"
		}
	}
	return "challenge"
}

// keepAliveWait is how long a keepalive may stay unanswered before the link
// counts as dead: two intervals, at least 15 s (slow devices answer late).
func keepAliveWait(every time.Duration) time.Duration { return max(2*every, 15*time.Second) }

// pingWithin runs ping and gives up after wait: a reply that never comes (a
// half-dead peer) must end the connection instead of blocking forever.
func pingWithin(wait time.Duration, ping func() error) error {
	done := make(chan error, 1)
	go func() { done <- ping() }()
	select {
	case err := <-done:
		return err
	case <-time.After(wait):
		return errors.New("keine Antwort auf Keepalive")
	}
}
