package conn

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellCommandUsesSystemPaths(t *testing.T) {
	root := filepath.Join("C:", "Windows")
	noPwsh := func(string) (string, error) { return "", errors.New("not found") }

	exe, line, err := shellCommand(ShellPowerShell, root, noPwsh)
	if err != nil || exe != filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe") || !strings.HasSuffix(line, " -NoLogo") {
		t.Fatalf("powershell = %q %q %v", exe, line, err)
	}
	if exe2, _, _ := shellCommand("", root, noPwsh); exe2 != exe {
		t.Errorf("empty shell must mean PowerShell, got %q", exe2)
	}
	if exe, _, err := shellCommand(ShellCmd, root, noPwsh); err != nil || exe != filepath.Join(root, "System32", "cmd.exe") {
		t.Errorf("cmd = %q %v", exe, err)
	}
	if _, _, err := shellCommand(ShellPwsh, root, noPwsh); !errors.Is(err, ErrNoShell) {
		t.Errorf("missing pwsh must fail with ErrNoShell, got %v", err)
	}
	relative := func(string) (string, error) { return "pwsh.exe", nil }
	if _, _, err := shellCommand(ShellPwsh, root, relative); !errors.Is(err, ErrNoShell) {
		t.Error("a pwsh found relative to the current folder must not run")
	}
	if _, _, err := shellCommand("bash", root, noPwsh); !errors.Is(err, ErrNoShell) {
		t.Error("unknown shells must fail")
	}
}

func TestQuoteArg(t *testing.T) {
	if got := quoteArg(`C:\Program Files\PowerShell\7\pwsh.exe`); got != `"C:\Program Files\PowerShell\7\pwsh.exe"` {
		t.Errorf("quoteArg = %s", got)
	}
	if got := quoteArg(`C:\x.exe`); got != `C:\x.exe` {
		t.Errorf("quoteArg = %s", got)
	}
}

func TestStartDir(t *testing.T) {
	home := t.TempDir()
	if d, err := startDir("", home); err != nil || d != home {
		t.Errorf("empty dir = %q %v, want home", d, err)
	}
	if _, err := startDir(filepath.Join(home, "fehlt"), home); err == nil {
		t.Error("a missing start folder must fail")
	}
}
