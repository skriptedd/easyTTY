package main

import (
	"errors"
	"fmt"
	"strings"

	"easytty/internal/conn"
	"easytty/internal/store"
	"easytty/internal/terminal"
)

// sessionLookup returns a saved session and its stored password.
type sessionLookup func(id string) (store.Session, string, error)

// withJumps puts the jump hosts of s in front of cfg (ssh -J), each with its
// own stored login. The store already refuses loops and long chains when
// saving; this checks again because the chain may have changed since.
func withJumps(cfg conn.SSHConfig, s store.Session, lookup sessionLookup) (conn.SSHConfig, error) {
	seen := map[string]bool{s.ID: true}
	hop := &cfg
	for id, n := s.JumpID, 0; id != ""; n++ {
		if n == store.MaxJumps {
			return cfg, fmt.Errorf("höchstens %d Sprung-Hosts hintereinander", store.MaxJumps)
		}
		if seen[id] {
			return cfg, errors.New("die Sprung-Hosts verweisen im Kreis aufeinander")
		}
		seen[id] = true
		j, pw, err := lookup(id)
		if err != nil {
			return cfg, fmt.Errorf("Sprung-Host-Session nicht gefunden: %w", err)
		}
		if j.Protocol != store.ProtocolSSH {
			return cfg, fmt.Errorf("Sprung-Host %s ist keine SSH-Session", j.Name)
		}
		next := conn.SSHConfig{Host: j.Host, Port: j.Port, User: j.User, Auth: j.Auth, KeyPath: j.KeyPath, Password: pw}
		hop.Jump = &next
		hop = &next
		id = j.JumpID
	}
	return cfg, nil
}

// lookupSession reads a session and its password from the store.
func (a *App) lookupSession(id string) (store.Session, string, error) {
	s, err := a.store.Session(id)
	if err != nil {
		return s, "", err
	}
	pw, _ := a.store.Password(id) // no password: the jump host asks
	return s, pw, nil
}

// localForwards converts the session's forwards for the SSH layer.
func localForwards(s store.Session) []conn.LocalForward {
	out := make([]conn.LocalForward, 0, len(s.Forwards))
	for _, f := range s.Forwards {
		out = append(out, conn.LocalForward{Listen: f.Listen, Host: f.Host, Port: f.Port})
	}
	return out
}

// startForwards opens the tunnels of a connected session and tells the user
// which ones run and which could not start (port taken …).
func (a *App) startForwards(c *conn.SSHConn, s store.Session) {
	if len(s.Forwards) == 0 {
		return
	}
	started, errs := c.Forward(localForwards(s))
	if len(started) > 0 {
		names := make([]string, len(started))
		for i, f := range started {
			names[i] = f.String()
		}
		a.notify("ok", s.Name+": Weiterleitung aktiv: "+strings.Join(names, ", "))
	}
	for _, err := range errs {
		a.notify("err", s.Name+": "+err.Error())
	}
}

// notify shows a toast in the UI.
func (a *App) notify(kind, message string) {
	a.emit(terminal.EventNotify, terminal.Notice{Kind: kind, Message: message})
}

// isJumpHost reports whether a host key prompt (host as known_hosts writes
// it: "sw1" or "[sw1]:2222") is for another host than the tab's target.
func isJumpHost(promptHost, target string) bool {
	if target == "" {
		return false
	}
	h := strings.TrimPrefix(promptHost, "[")
	if i := strings.Index(h, "]"); i >= 0 {
		h = h[:i]
	}
	return !strings.EqualFold(h, target)
}
