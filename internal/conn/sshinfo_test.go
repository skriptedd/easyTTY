package conn

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"easytty/internal/sshtest"
)

func TestInfoAfterPasswordLogin(t *testing.T) {
	srv := sshtest.Start(t, "u", "pw", nil)
	host, port := hostPort(t, srv.Addr)
	d := Dialer{HostKeys: ssh.FixedHostKey(srv.HostKey), HomeDir: t.TempDir()}

	c, err := d.Dial(context.Background(), SSHConfig{Host: host, Port: port, User: "u", Auth: "password", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	info := c.Info()

	if info.User != "u" || info.Addr != srv.Addr || len(info.Jump) != 0 {
		t.Errorf("target: %+v", info)
	}
	if info.Auth != "password" || info.ClientKey != nil {
		t.Errorf("auth %q key %+v", info.Auth, info.ClientKey)
	}
	if info.HostKey.Fingerprint != ssh.FingerprintSHA256(srv.HostKey) || info.HostKey.Type != srv.HostKey.Type() {
		t.Errorf("host key %+v", info.HostKey)
	}
	if info.KeyExchange.Name == "" || info.CipherTo.Name == "" || info.CipherFrom.Name == "" || info.HostKeyAlgo.Name == "" {
		t.Errorf("algorithms missing: %+v", info)
	}
	if info.KeyExchange.Legacy || info.CipherTo.Legacy {
		t.Errorf("modern server marked legacy: %+v", info)
	}
	if !strings.HasPrefix(info.ServerVersion, "SSH-2.0-") || !strings.HasPrefix(info.ClientVersion, "SSH-2.0-") {
		t.Errorf("versions %q %q", info.ServerVersion, info.ClientVersion)
	}
}

func TestInfoMarksLegacyAlgorithms(t *testing.T) {
	srv := sshtest.StartLegacy(t, "u", "pw")
	host, port := hostPort(t, srv.Addr)
	d := Dialer{HostKeys: ssh.FixedHostKey(srv.HostKey), HomeDir: t.TempDir()}

	c, err := d.Dial(context.Background(), SSHConfig{Host: host, Port: port, User: "u", Auth: "password", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	info := c.Info()

	if !info.CipherTo.Legacy || !info.CipherFrom.Legacy || !info.KeyExchange.Legacy {
		t.Errorf("legacy not marked: kex %+v cipher %+v/%+v", info.KeyExchange, info.CipherTo, info.CipherFrom)
	}
}

func TestInfoNamesTheChosenKeyFile(t *testing.T) {
	home := t.TempDir()
	keyPath := filepath.Join(home, "keys", "router")
	pub := writeKey(t, keyPath, "")
	srv := sshtest.Start(t, "u", "", pub)
	host, port := hostPort(t, srv.Addr)
	d := Dialer{HostKeys: ssh.FixedHostKey(srv.HostKey), HomeDir: home}

	c, err := d.Dial(context.Background(), SSHConfig{Host: host, Port: port, User: "u", Auth: "key", KeyPath: keyPath})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	info := c.Info()

	if info.Auth != "publickey" || info.ClientKey == nil {
		t.Fatalf("auth %q key %+v", info.Auth, info.ClientKey)
	}
	if info.ClientKey.Path != keyPath || info.ClientKey.Fingerprint != ssh.FingerprintSHA256(pub) {
		t.Errorf("client key %+v", info.ClientKey)
	}
}
