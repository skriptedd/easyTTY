package main

import (
	"errors"
	"strings"
	"testing"

	"easytty/internal/conn"
	"easytty/internal/store"
)

func lookupIn(sessions map[string]store.Session, passwords map[string]string) sessionLookup {
	return func(id string) (store.Session, string, error) {
		s, ok := sessions[id]
		if !ok {
			return s, "", store.ErrNotFound
		}
		return s, passwords[id], nil
	}
}

func TestWithJumpsBuildsChainWithOwnLogins(t *testing.T) {
	sessions := map[string]store.Session{
		"j1": {ID: "j1", Protocol: store.ProtocolSSH, Host: "bastion", Port: 22, User: "hop", Auth: "auto", JumpID: "j2"},
		"j2": {ID: "j2", Protocol: store.ProtocolSSH, Host: "edge", Port: 2222, User: "edge", Auth: "key", KeyPath: "k"},
	}
	target := store.Session{ID: "t", Protocol: store.ProtocolSSH, Host: "sw1", JumpID: "j1"}

	cfg, err := withJumps(conn.SSHConfig{Host: "sw1", Password: "target-pw"}, target, lookupIn(sessions, map[string]string{"j1": "hop-pw"}))

	if err != nil {
		t.Fatal(err)
	}
	if cfg.Password != "target-pw" || cfg.Jump == nil || cfg.Jump.Host != "bastion" || cfg.Jump.Password != "hop-pw" {
		t.Fatalf("first hop wrong: %+v", cfg)
	}
	second := cfg.Jump.Jump
	if second == nil || second.Host != "edge" || second.Port != 2222 || second.KeyPath != "k" || second.Password != "" || second.Jump != nil {
		t.Errorf("second hop wrong: %+v", second)
	}
}

func TestWithJumpsRefusesBrokenChains(t *testing.T) {
	cases := map[string]map[string]store.Session{
		"missing": {},
		"loop": {
			"a": {ID: "a", Protocol: store.ProtocolSSH, Host: "a", JumpID: "b"},
			"b": {ID: "b", Protocol: store.ProtocolSSH, Host: "b", JumpID: "a"},
		},
		"not ssh": {"a": {ID: "a", Protocol: store.ProtocolRDP, Host: "a"}},
		"too long": {
			"a": {ID: "a", Protocol: store.ProtocolSSH, Host: "a", JumpID: "b"},
			"b": {ID: "b", Protocol: store.ProtocolSSH, Host: "b", JumpID: "c"},
			"c": {ID: "c", Protocol: store.ProtocolSSH, Host: "c", JumpID: "d"},
			"d": {ID: "d", Protocol: store.ProtocolSSH, Host: "d"},
		},
	}
	for name, sessions := range cases {
		t.Run(name, func(t *testing.T) {
			target := store.Session{ID: "t", Protocol: store.ProtocolSSH, Host: "t", JumpID: "a"}

			_, err := withJumps(conn.SSHConfig{}, target, lookupIn(sessions, nil))

			if err == nil {
				t.Error("broken chain accepted")
			}
		})
	}
}

func TestWithJumpsWithoutJumpLeavesConfig(t *testing.T) {
	called := false
	lookup := func(string) (store.Session, string, error) {
		called = true
		return store.Session{}, "", errors.New("x")
	}

	cfg, err := withJumps(conn.SSHConfig{Host: "h"}, store.Session{Host: "h"}, lookup)

	if err != nil || cfg.Jump != nil || called {
		t.Errorf("cfg %+v, err %v, lookup called %v", cfg, err, called)
	}
}

func TestLocalForwardsConvert(t *testing.T) {
	s := store.Session{Forwards: []store.Forward{{Listen: 8443, Host: "10.0.0.1", Port: 443}}}

	got := localForwards(s)

	if len(got) != 1 || got[0] != (conn.LocalForward{Listen: 8443, Host: "10.0.0.1", Port: 443}) || !strings.Contains(got[0].String(), "127.0.0.1:8443") {
		t.Errorf("forwards = %+v", got)
	}
}

func TestIsJumpHost(t *testing.T) {
	cases := []struct {
		prompt, target string
		want           bool
	}{
		{"sw1", "sw1", false},
		{"[sw1]:2222", "sw1", false},
		{"SW1", "sw1", false},
		{"bastion", "sw1", true},
		{"[10.0.0.1]:22", "10.0.0.2", true},
		{"sw1", "", false},
	}
	for _, c := range cases {
		if got := isJumpHost(c.prompt, c.target); got != c.want {
			t.Errorf("isJumpHost(%q, %q) = %v, want %v", c.prompt, c.target, got, c.want)
		}
	}
}
