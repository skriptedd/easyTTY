package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// FirewallInfo tells the firmware dialog whether devices can reach easyTTY.
type FirewallInfo struct {
	Checked bool   `json:"checked"`
	Level   string `json:"level"` // ok | warn | unknown
	Message string `json:"message"`
}

const (
	createNoWindow  = 0x08000000
	firewallTimeout = 20 * time.Second
)

// firewallScript lists enabled inbound rules for the exe ("Action|Profile|Protocol")
// and the active network categories ("NET|Private"). Read-only.
const firewallScript = `$ErrorActionPreference='SilentlyContinue'; $p=$env:EASYTTY_EXE; ` +
	`Get-NetFirewallApplicationFilter | Where-Object { $_.Program -eq $p } | ForEach-Object { ` +
	`$r = $_ | Get-NetFirewallRule; if ($r.Enabled -eq 'True' -and $r.Direction -eq 'Inbound') { ` +
	`$pf = $r | Get-NetFirewallPortFilter; "$($r.Action)|$($r.Profile)|$($pf.Protocol)" } }; ` +
	`Get-NetConnectionProfile | ForEach-Object { "NET|$($_.NetworkCategory)" }`

// FirewallStatus checks the Windows Firewall rules for this exe (takes a few seconds).
func (a *App) FirewallStatus() FirewallInfo {
	exe, err := os.Executable()
	if err != nil {
		return FirewallInfo{Level: "unknown", Message: "Firewall nicht prüfbar"}
	}
	ctx, cancel := context.WithTimeout(a.ctx, firewallTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(systemRoot(), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", firewallScript)
	cmd.Env = append(os.Environ(), "EASYTTY_EXE="+exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return FirewallInfo{Level: "unknown", Message: "Firewall nicht prüfbar: " + err.Error()}
	}
	return judgeFirewall(string(out))
}

// judgeFirewall decides from the rule lines whether TCP and UDP get in on
// the current network profile.
func judgeFirewall(out string) FirewallInfo {
	var nets []string
	type rule struct {
		allow          bool
		profile, proto string
	}
	var rules []rule
	for _, l := range strings.Split(strings.ReplaceAll(out, "\r", ""), "\n") {
		f := strings.Split(strings.TrimSpace(l), "|")
		switch {
		case len(f) == 2 && f[0] == "NET":
			nets = append(nets, f[1])
		case len(f) == 3:
			rules = append(rules, rule{allow: f[0] == "Allow", profile: f[1], proto: strings.ToUpper(f[2])})
		}
	}
	net := "Private"
	if len(nets) > 0 {
		net = nets[0]
	}
	if net == "DomainAuthenticated" {
		net = "Domain"
	}
	covers := func(r rule) bool { return r.profile == "Any" || strings.Contains(r.profile, net) }
	ok := map[string]bool{}
	for _, r := range rules {
		if !covers(r) {
			continue
		}
		if !r.allow {
			return FirewallInfo{Checked: true, Level: "warn", Message: "Die Windows-Firewall BLOCKIERT easyTTY im Netz „" + net + "“. In „Windows Defender Firewall → Eingehende Regeln“ die Block-Regel für easyTTY löschen oder auf Zulassen stellen."}
		}
		if r.proto == "ANY" {
			ok["TCP"], ok["UDP"] = true, true
		}
		ok[r.proto] = true
	}
	switch {
	case ok["TCP"] && ok["UDP"]:
		return FirewallInfo{Checked: true, Level: "ok", Message: "Firewall: easyTTY darf im Netz „" + net + "“ Verbindungen annehmen (TCP und UDP)."}
	case len(rules) == 0:
		return FirewallInfo{Checked: true, Level: "warn", Message: "Firewall: noch keine Freigabe für diese easyTTY.exe. Beim Start fragt Windows – dort „Zulassen“ wählen (Admin nötig)."}
	case !ok["TCP"] && !ok["UDP"]:
		return FirewallInfo{Checked: true, Level: "warn", Message: "Firewall: easyTTY ist nur für ein anderes Netzprofil freigegeben, nicht für „" + net + "“."}
	case !ok["UDP"]:
		return FirewallInfo{Checked: true, Level: "warn", Message: "Firewall: UDP ist im Netz „" + net + "“ nicht freigegeben – TFTP kommt nicht durch. HTTP/SCP gehen."}
	}
	return FirewallInfo{Checked: true, Level: "warn", Message: "Firewall: easyTTY ist im Netz „" + net + "“ nicht (vollständig) freigegeben. Freigabe gilt evtl. nur für ein anderes Netzprofil."}
}
