package rdpview

import "testing"

func TestSplitUser(t *testing.T) {
	for in, want := range map[string][2]string{
		`CORP\admin`:       {"CORP", "admin"},
		"admin@corp.local": {"", "admin@corp.local"},
		"admin":            {"", "admin"},
		`\admin`:           {"", `\admin`},
	} {
		if d, u := splitUser(in); d != want[0] || u != want[1] {
			t.Errorf("%q → %q %q", in, d, u)
		}
	}
}

func TestDesktopSize(t *testing.T) {
	if w, h := desktopSize(1283, 701); w != 1280 || h != 700 {
		t.Errorf("got %d×%d", w, h)
	}
	if w, h := desktopSize(10, 99999); w != 200 || h != 8192 {
		t.Errorf("clamp: %d×%d", w, h)
	}
}
