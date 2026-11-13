package conn

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"easytty/internal/sshtest"
)

func hostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(p)
	return h, n
}

func readUntil(t *testing.T, r io.Reader, want string) string {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 256)
		for !strings.Contains(sb.String(), want) {
			n, err := r.Read(buf)
			sb.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- sb.String()
	}()
	select {
	case s := <-done:
		return s
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for %q", want)
		return ""
	}
}

func TestDialPasswordPromptShellResizeBreak(t *testing.T) {
	srv := sshtest.Start(t, "admin", "s3cret", nil)
	host, port := hostPort(t, srv.Addr)

	var prompts []string
	d := Dialer{
		HostKeys: ssh.FixedHostKey(srv.HostKey),
		HomeDir:  t.TempDir(),
		Ask: func(r PromptRequest) ([]string, bool) {
			prompts = append(prompts, r.Kind)
			switch r.Kind {
			case "username":
				return []string{"admin"}, true
			case "password":
				return []string{"s3cret"}, true
			}
			return nil, false
		},
	}
	c, err := d.Dial(context.Background(), SSHConfig{Host: host, Port: port, Auth: "auto", Password: "wrong", KeepAlive: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if len(prompts) < 2 || prompts[0] != "username" {
		t.Fatalf("expected username then password prompt, got %v", prompts)
	}

	if _, err := c.Write([]byte("show version\n")); err != nil {
		t.Fatal(err)
	}
	if out := readUntil(t, c, "echo:show version"); !strings.Contains(out, "echo:show version") {
		t.Fatalf("unexpected output %q", out)
	}
	if err := c.Resize(200, 50); err != nil {
		t.Fatal(err)
	}
	if err := c.SendBreak(); err != nil {
		t.Fatal(err)
	}
	if c.Client() == nil {
		t.Fatal("client nil")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(srv.Resizes()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if r := srv.Resizes(); len(r) != 1 || r[0] != [2]int{200, 50} {
		t.Fatalf("resize not received: %v", r)
	}
	if srv.Breaks() != 1 {
		t.Fatalf("break not received")
	}

	_, _ = c.Write([]byte("exit\n"))
	buf := make([]byte, 64)
	for {
		if _, err := c.Read(buf); err != nil {
			break // EOF after shell exit
		}
	}
}

func TestShellExitError(t *testing.T) {
	if shellExitError(nil) != io.EOF || shellExitError(&ssh.ExitMissingError{}) != io.EOF {
		t.Fatal("normal exits should map to EOF")
	}
	other := io.ErrUnexpectedEOF
	if shellExitError(other) != other {
		t.Fatal("other errors must pass through")
	}
}

func TestDialStoredPasswordNoPrompt(t *testing.T) {
	srv := sshtest.Start(t, "u", "pw", nil)
	host, port := hostPort(t, srv.Addr)
	d := Dialer{HostKeys: ssh.FixedHostKey(srv.HostKey), HomeDir: t.TempDir(),
		Ask: func(PromptRequest) ([]string, bool) { t.Error("unexpected prompt"); return nil, false }}
	c, err := d.Dial(context.Background(), SSHConfig{Host: host, Port: port, User: "u", Auth: "password", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	c.Close() // idempotent
}

func TestDialCancelledPromptFails(t *testing.T) {
	srv := sshtest.Start(t, "u", "pw", nil)
	host, port := hostPort(t, srv.Addr)
	d := Dialer{HostKeys: ssh.FixedHostKey(srv.HostKey), HomeDir: t.TempDir(),
		Ask: func(PromptRequest) ([]string, bool) { return nil, false }}
	if _, err := d.Dial(context.Background(), SSHConfig{Host: host, Port: port, User: "u"}); err == nil {
		t.Fatal("expected error after cancelled prompt")
	}
	if _, err := d.Dial(context.Background(), SSHConfig{Host: host, Port: port}); err == nil {
		t.Fatal("expected error when username prompt is cancelled")
	}
}

func TestDialRejectsWrongHostKey(t *testing.T) {
	srv := sshtest.Start(t, "u", "pw", nil)
	other := sshtest.Start(t, "u", "pw", nil)
	host, port := hostPort(t, srv.Addr)
	d := Dialer{HostKeys: ssh.FixedHostKey(other.HostKey), HomeDir: t.TempDir()}
	if _, err := d.Dial(context.Background(), SSHConfig{Host: host, Port: port, User: "u", Password: "pw"}); err == nil {
		t.Fatal("expected host key mismatch")
	}
	if _, err := (Dialer{}).DialClient(context.Background(), SSHConfig{Host: host, Port: port, User: "u"}); err == nil {
		t.Fatal("expected error without host key callback")
	}
}

func TestDialSilentServerTimesOut(t *testing.T) {
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
			defer c.Close() // accept but never speak SSH
		}
	}()
	host, port := hostPort(t, ln.Addr().String())
	d := Dialer{HostKeys: ssh.InsecureIgnoreHostKey(), HandshakeTimeout: 200 * time.Millisecond}
	start := time.Now()
	_, err = d.Dial(context.Background(), SSHConfig{Host: host, Port: port, User: "u"})
	if err == nil || !strings.Contains(err.Error(), "antwortet nicht") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout took too long")
	}
}

func TestPromptPausesHandshakeTimeout(t *testing.T) {
	srv := sshtest.Start(t, "u", "pw", nil)
	host, port := hostPort(t, srv.Addr)
	d := Dialer{HostKeys: ssh.FixedHostKey(srv.HostKey), HomeDir: t.TempDir(), HandshakeTimeout: 150 * time.Millisecond,
		Ask: func(PromptRequest) ([]string, bool) { time.Sleep(400 * time.Millisecond); return []string{"pw"}, true }}
	c, err := d.Dial(context.Background(), SSHConfig{Host: host, Port: port, User: "u", Auth: "password"})
	if err != nil {
		t.Fatalf("slow user answer must not trip the handshake timeout: %v", err)
	}
	c.Close()
}

func TestKeyAuthIgnoresStoredPassword(t *testing.T) {
	srv := sshtest.Start(t, "u", "pw", nil)
	host, port := hostPort(t, srv.Addr)
	asked := false
	d := Dialer{HostKeys: ssh.FixedHostKey(srv.HostKey), HomeDir: t.TempDir(),
		Ask: func(PromptRequest) ([]string, bool) { asked = true; return nil, false }}
	_, err := d.Dial(context.Background(), SSHConfig{Host: host, Port: port, User: "u", Auth: "agent", Password: "pw"})
	if err == nil || !asked {
		t.Fatalf("stored password must not be used for agent auth (err=%v asked=%v)", err, asked)
	}
}

func TestDialUnreachable(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	host, port := hostPort(t, addr)
	d := Dialer{HostKeys: ssh.InsecureIgnoreHostKey()}
	if _, err := d.Dial(context.Background(), SSHConfig{Host: host, Port: port, User: "u"}); err == nil {
		t.Fatal("expected connection error")
	}
}

func writeKey(t *testing.T, path string, passphrase string) ssh.PublicKey {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	var block *pem.Block
	var err error
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	sp, _ := ssh.NewPublicKey(pub)
	return sp
}

func TestDialDefaultKeyAuto(t *testing.T) {
	home := t.TempDir()
	pub := writeKey(t, filepath.Join(home, ".ssh", "id_ed25519"), "")
	srv := sshtest.Start(t, "u", "", pub)
	host, port := hostPort(t, srv.Addr)
	d := Dialer{HostKeys: ssh.FixedHostKey(srv.HostKey), HomeDir: home,
		Ask: func(PromptRequest) ([]string, bool) { t.Error("unexpected prompt"); return nil, false }}
	c, err := d.Dial(context.Background(), SSHConfig{Host: host, Port: port, User: "u", Auth: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

func TestDialEncryptedKeyAsksPassphrase(t *testing.T) {
	home := t.TempDir()
	keyPath := filepath.Join(home, "keys", "router")
	pub := writeKey(t, keyPath, "geheim")
	srv := sshtest.Start(t, "u", "", pub)
	host, port := hostPort(t, srv.Addr)
	attempts := 0
	d := Dialer{HostKeys: ssh.FixedHostKey(srv.HostKey), HomeDir: home,
		Ask: func(r PromptRequest) ([]string, bool) {
			if r.Kind != "passphrase" {
				return nil, false
			}
			attempts++
			if attempts == 1 {
				return []string{"falsch"}, true
			}
			return []string{"geheim"}, true
		}}
	c, err := d.Dial(context.Background(), SSHConfig{Host: host, Port: port, User: "u", Auth: "key", KeyPath: "~/keys/router"})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if attempts != 2 {
		t.Fatalf("expected 2 passphrase attempts, got %d", attempts)
	}
}

func TestDialContextCancelDuringPrompt(t *testing.T) {
	srv := sshtest.Start(t, "u", "pw", nil)
	host, port := hostPort(t, srv.Addr)
	ctx, cancel := context.WithCancel(context.Background())
	d := Dialer{HostKeys: ssh.FixedHostKey(srv.HostKey), HomeDir: t.TempDir(),
		Ask: func(PromptRequest) ([]string, bool) {
			cancel()
			time.Sleep(50 * time.Millisecond)
			return []string{"x"}, true
		}}
	if _, err := d.Dial(ctx, SSHConfig{Host: host, Port: port, User: "u"}); err == nil {
		t.Fatal("expected cancellation error")
	}
}

// Console servers (Opengear, Avocent …) often only offer CBC ciphers and SHA-1.
func TestDialLegacyConsoleServer(t *testing.T) {
	srv := sshtest.StartLegacy(t, "console:6", "pw")
	host, port := hostPort(t, srv.Addr)
	d := Dialer{HostKeys: ssh.FixedHostKey(srv.HostKey), HomeDir: t.TempDir()}
	c, err := d.Dial(context.Background(), SSHConfig{Host: host, Port: port, User: "console:6", Auth: "password", Password: "pw", KeepAlive: time.Hour})
	if err != nil {
		t.Fatalf("legacy server: %v", err)
	}
	c.Close()
}

func TestModernAlgorithmsStayFirst(t *testing.T) {
	cfg := legacyFriendly()
	if strings.HasSuffix(cfg.Ciphers[0], "-cbc") || !slices.Contains(cfg.Ciphers, "aes128-cbc") {
		t.Errorf("ciphers: %v", cfg.Ciphers)
	}
	if strings.Contains(cfg.KeyExchanges[0], "sha1") {
		t.Errorf("kex: %v", cfg.KeyExchanges)
	}
}

func TestChosenKeyThatCannotBeLoadedIsReported(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.Start(t, "u", "pw", signer.PublicKey()) // the server must offer key login
	host, port := hostPort(t, srv.Addr)
	asked := false
	d := Dialer{
		HostKeys: ssh.FixedHostKey(srv.HostKey),
		HomeDir:  t.TempDir(),
		Ask:      func(PromptRequest) ([]string, bool) { asked = true; return []string{"pw"}, true },
	}
	_, err = d.Dial(context.Background(), SSHConfig{Host: host, Port: port, User: "u", Auth: "key", KeyPath: filepath.Join(t.TempDir(), "gibt-es-nicht")})
	if err == nil || !strings.Contains(err.Error(), "gibt-es-nicht") {
		t.Fatalf("want the key error, got %v", err)
	}
	if asked {
		t.Error("a broken key must not turn into a password prompt")
	}
}
