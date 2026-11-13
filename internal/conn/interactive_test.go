package conn

import (
	"testing"
	"time"
)

func TestInteractiveKindOnlyMarksRealPasswords(t *testing.T) {
	for _, c := range []struct {
		q    []string
		echo []bool
		want string
	}{
		{[]string{"Password: "}, []bool{false}, "password"},
		{[]string{"Kennwort:"}, []bool{false}, "password"},
		{[]string{"Verification code: "}, []bool{false}, "challenge"},
		{[]string{"Enter PASSCODE:"}, []bool{false}, "challenge"},
		{[]string{"One-time password (OTP): "}, []bool{false}, "challenge"},
		{[]string{"Password:", "Token:"}, []bool{false, false}, "challenge"},
		{[]string{"Username:"}, []bool{true}, "challenge"},
	} {
		if got := interactiveKind(c.q, c.echo); got != c.want {
			t.Errorf("%q: got %s, want %s", c.q, got, c.want)
		}
	}
}

func TestPingWithinGivesUpOnSilentPeer(t *testing.T) {
	if err := pingWithin(time.Second, func() error { return nil }); err != nil {
		t.Fatalf("answered ping: %v", err)
	}
	block := make(chan struct{})
	defer close(block)
	err := pingWithin(10*time.Millisecond, func() error { <-block; return nil })
	if err == nil {
		t.Fatal("a peer that never answers must count as dead")
	}
}

func TestKeepAliveWaitLeavesSlowDevicesTime(t *testing.T) {
	if got := keepAliveWait(time.Second); got != 15*time.Second {
		t.Errorf("short interval: %v", got)
	}
	if got := keepAliveWait(time.Minute); got != 2*time.Minute {
		t.Errorf("long interval: %v", got)
	}
}
