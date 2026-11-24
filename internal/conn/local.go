package conn

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Local shells a session can start on this PC.
const (
	ShellPowerShell = "powershell" // Windows PowerShell 5.1, always there
	ShellPwsh       = "pwsh"       // PowerShell 7, if installed
	ShellCmd        = "cmd"
)

// LocalConfig describes a shell started on this PC in a pseudo console.
type LocalConfig struct {
	Shell string // ShellPowerShell, ShellPwsh or ShellCmd
	Dir   string // start folder ("" = home)
	Home  string
	Cols  int
	Rows  int
}

// ErrNoShell means the chosen shell is not installed.
var ErrNoShell = errors.New("Shell nicht gefunden")

// shellCommand returns the program and its command line. Windows' own shells
// come from the system folder by full path, so nothing on PATH or in the
// start folder can stand in for them.
func shellCommand(shell, systemRoot string, lookPath func(string) (string, error)) (string, string, error) {
	sys := filepath.Join(systemRoot, "System32")
	switch shell {
	case ShellPowerShell, "":
		exe := filepath.Join(sys, "WindowsPowerShell", "v1.0", "powershell.exe")
		return exe, quoteArg(exe) + " -NoLogo", nil
	case ShellPwsh:
		exe, err := lookPath("pwsh.exe")
		if err != nil || !filepath.IsAbs(exe) {
			return "", "", fmt.Errorf("%w: PowerShell 7 (pwsh.exe) ist nicht installiert", ErrNoShell)
		}
		return exe, quoteArg(exe) + " -NoLogo", nil
	case ShellCmd:
		exe := filepath.Join(sys, "cmd.exe")
		return exe, quoteArg(exe), nil
	}
	return "", "", fmt.Errorf("%w: %q", ErrNoShell, shell)
}

// ShellName is the label of a shell for titles and logs.
func ShellName(shell string) string {
	switch shell {
	case ShellPwsh:
		return "PowerShell 7"
	case ShellCmd:
		return "Eingabeaufforderung"
	}
	return "PowerShell"
}

// startDir checks the start folder; "" means home.
func startDir(dir, home string) (string, error) {
	if dir == "" {
		dir = home
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("Startordner %s gibt es nicht", dir)
	}
	return dir, nil
}

func quoteArg(s string) string {
	if !strings.ContainsAny(s, " \t\"") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
