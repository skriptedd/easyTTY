package conn

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh/knownhosts"
)

// One broken line in ~/.ssh/known_hosts must not hide the other hosts in it:
// a changed key of such a host has to be reported as changed, not unknown.
func TestBrokenForeignLineStillReportsChangedKey(t *testing.T) {
	dir := t.TempDir()
	real, attacker := newKey(t), newKey(t)
	user := filepath.Join(dir, "user_known_hosts")
	content := "garbage line here\n" + knownhosts.Line([]string{"sw1"}, real) + "\nother ssh-ed25519 notbase64!!\n"
	if err := os.WriteFile(user, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "app_known_hosts")
	if err := os.WriteFile(app, []byte(knownhosts.Line([]string{"x"}, real)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	k := NewKnownHosts(app, []string{user}, nil)

	var got HostKeyPrompt
	asked := false
	cb := k.CallbackWith(func(p HostKeyPrompt) bool { got, asked = p, true; return false })
	if err := cb("sw1:22", &net.TCPAddr{}, attacker); err == nil {
		t.Fatal("changed key accepted")
	}
	if !asked || !got.Changed {
		t.Fatalf("changed key not reported as changed: asked=%v prompt=%+v", asked, got)
	}
	// The stored key is still accepted without a question.
	asked = false
	if err := cb("sw1:22", &net.TCPAddr{}, real); err != nil || asked {
		t.Fatalf("known key: err=%v asked=%v", err, asked)
	}
}
