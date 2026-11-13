package conn

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func newKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

var addr = &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 22}

func TestKnownHostsTrustOnFirstUse(t *testing.T) {
	file := filepath.Join(t.TempDir(), "known_hosts")
	var prompts []HostKeyPrompt
	accept := true
	kh := NewKnownHosts(file, nil, func(p HostKeyPrompt) bool { prompts = append(prompts, p); return accept })
	cb := kh.Callback()
	key := newKey(t)

	if err := cb("sw1:22", addr, key); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0].Changed || prompts[0].Host != "sw1" || !strings.HasPrefix(prompts[0].Fingerprint, "SHA256:") {
		t.Fatalf("bad prompt: %+v", prompts)
	}
	if err := cb("sw1:22", addr, key); err != nil || len(prompts) != 1 {
		t.Fatalf("known key should pass silently: %v %d", err, len(prompts))
	}

	// Changed key: warn, and replace on accept.
	newer := newKey(t)
	accept = false
	if err := cb("sw1:22", addr, newer); err == nil {
		t.Fatal("rejected changed key must fail")
	}
	if !prompts[1].Changed {
		t.Fatal("prompt should flag changed key")
	}
	accept = true
	if err := cb("sw1:22", addr, newer); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	if strings.Count(string(raw), "sw1 ") != 1 {
		t.Fatalf("old key should be replaced:\n%s", raw)
	}
	accept = false
	if err := cb("sw1:22", addr, key); err == nil || !prompts[len(prompts)-1].Changed {
		t.Fatal("old key must no longer be trusted")
	}
}

func TestKnownHostsReadOnlyFileAndNonStandardPort(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user_known_hosts")
	key := newKey(t)
	line := knownhosts.Line([]string{knownhosts.Normalize("srv:2222")}, key)
	_ = os.WriteFile(user, []byte(line+"\n"), 0o600)

	kh := NewKnownHosts(filepath.Join(dir, "app"), []string{user, filepath.Join(dir, "missing")},
		func(HostKeyPrompt) bool { t.Error("should not prompt"); return false })
	if err := kh.Callback()("srv:2222", addr, key); err != nil {
		t.Fatal(err)
	}
}

func TestKnownHostsCorruptForeignFileIgnored(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad")
	_ = os.WriteFile(bad, []byte("@@@ totally broken\n"), 0o600)
	prompted := false
	kh := NewKnownHosts(filepath.Join(dir, "app"), []string{bad}, func(HostKeyPrompt) bool { prompted = true; return true })
	key := newKey(t)
	if err := kh.Callback()("h:22", addr, key); err != nil || !prompted {
		t.Fatalf("err=%v prompted=%v", err, prompted)
	}
	if err := kh.Callback()("h:22", addr, key); err != nil {
		t.Fatal(err)
	}
}

func TestHostKeyAlgorithmsAndTypeDowngrade(t *testing.T) {
	file := filepath.Join(t.TempDir(), "known_hosts")
	kh := NewKnownHosts(file, nil, func(HostKeyPrompt) bool { return true })
	if algos := kh.HostKeyAlgorithms("sw1:22"); algos != nil {
		t.Fatalf("unknown host should have no pinned algorithms: %v", algos)
	}
	ed := newKey(t)
	if err := kh.Callback()("sw1:22", addr, ed); err != nil {
		t.Fatal(err)
	}
	if algos := kh.HostKeyAlgorithms("sw1:22"); len(algos) != 1 || algos[0] != ssh.KeyAlgoED25519 {
		t.Fatalf("got %v", algos)
	}
	if got := algorithmsFor(ssh.KeyAlgoRSA); len(got) != 3 || got[0] != ssh.KeyAlgoRSASHA512 {
		t.Fatalf("rsa algorithms: %v", got)
	}

	// A key of another type for a known host must be flagged as changed.
	var last HostKeyPrompt
	kh2 := NewKnownHosts(file, nil, func(p HostKeyPrompt) bool { last = p; return false })
	other := newKeyOfType(t)
	_ = kh2.Callback()("sw1:22", addr, other)
	if !last.Changed {
		t.Fatal("different key type for known host must count as changed")
	}
}

func newKeyOfType(t *testing.T) ssh.PublicKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := ssh.NewPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

func TestKnownHostsNoConfirmRejects(t *testing.T) {
	kh := NewKnownHosts(filepath.Join(t.TempDir(), "app"), nil, nil)
	if err := kh.Callback()("h:22", addr, newKey(t)); err == nil {
		t.Fatal("expected rejection")
	}
}

// A host reinstalled since OpenSSH stored its key: the prompt names the old key
// and its file; once accepted, easyTTY's own entry wins and nobody asks again.
func TestChangedKeyFromOpenSSHFileShowsOldAndSticks(t *testing.T) {
	dir := t.TempDir()
	oldKey, newKey := newKey(t), newKey(t)
	userFile := filepath.Join(dir, "ssh_known_hosts")
	if err := os.WriteFile(userFile, []byte(knownhosts.Line([]string{"10.228.5.37"}, oldKey)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var prompts []HostKeyPrompt
	kh := NewKnownHosts(filepath.Join(dir, "app"), []string{userFile}, func(p HostKeyPrompt) bool { prompts = append(prompts, p); return true })
	addr := &net.TCPAddr{IP: net.ParseIP("10.228.5.37"), Port: 22}
	if err := kh.Callback()("10.228.5.37:22", addr, newKey); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || !prompts[0].Changed || prompts[0].OldFingerprint != ssh.FingerprintSHA256(oldKey) ||
		!strings.Contains(prompts[0].OldSource, "ssh_known_hosts, Zeile 1") {
		t.Fatalf("prompt: %+v", prompts)
	}
	if err := kh.Callback()("10.228.5.37:22", addr, newKey); err != nil || len(prompts) != 1 {
		t.Errorf("asked again after accepting: %v, %d prompts", err, len(prompts))
	}
}
