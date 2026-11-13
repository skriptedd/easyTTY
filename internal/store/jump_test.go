package store

import (
	"errors"
	"strings"
	"testing"
)

func saveSSH(t *testing.T, st *Store, name, jump string) Session {
	t.Helper()
	s, err := st.SaveSession(Session{Name: name, Host: name + ".example", JumpID: jump})
	if err != nil {
		t.Fatalf("save %s: %v", name, err)
	}
	return s
}

func TestJumpHostMustBeSavedSSHSession(t *testing.T) {
	st, _ := openTemp(t)
	rdp, err := st.SaveSession(Session{Protocol: ProtocolRDP, Host: "win"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := st.SaveSession(Session{Host: "a", JumpID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown jump host: %v", err)
	}
	if _, err := st.SaveSession(Session{Host: "a", JumpID: rdp.ID}); err == nil {
		t.Error("RDP session accepted as jump host")
	}
}

func TestJumpChainLimitsAndLoops(t *testing.T) {
	st, _ := openTemp(t)
	a := saveSSH(t, st, "a", "")
	b := saveSSH(t, st, "b", a.ID)
	c := saveSSH(t, st, "c", b.ID)
	d := saveSSH(t, st, "d", c.ID) // d -> c -> b -> a: three hops

	if _, err := st.SaveSession(Session{Host: "e", JumpID: d.ID}); err == nil {
		t.Error("four hops accepted")
	}
	a.JumpID = c.ID // a -> c -> b -> a
	if _, err := st.SaveSession(a); err == nil || !strings.Contains(err.Error(), "Kreis") {
		t.Errorf("loop accepted: %v", err)
	}
	a.JumpID = a.ID
	if _, err := st.SaveSession(a); err == nil {
		t.Error("session as its own jump host accepted")
	}
}

func TestDeletingJumpHostConnectsDirectly(t *testing.T) {
	st, _ := openTemp(t)
	jump := saveSSH(t, st, "jump", "")
	target := saveSSH(t, st, "target", jump.ID)

	if err := st.DeleteSession(jump.ID); err != nil {
		t.Fatal(err)
	}

	got, err := st.Session(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.JumpID != "" {
		t.Errorf("JumpID = %q after its jump host was deleted", got.JumpID)
	}
}

func TestImportMapsJumpHostsToNewIDs(t *testing.T) {
	src, _ := openTemp(t)
	jump := saveSSH(t, src, "jump", "")
	saveSSH(t, src, "target", jump.ID)
	lonely := saveSSH(t, src, "lonely", jump.ID)
	all := src.Export(nil, nil, true)
	single := src.Export(nil, []string{lonely.ID}, false)
	dst, _ := openTemp(t)
	alone, _ := openTemp(t)

	if _, err := dst.ImportBundle(all); err != nil {
		t.Fatal(err)
	}
	if _, err := alone.ImportBundle(single); err != nil {
		t.Fatal(err)
	}

	byName := map[string]Session{}
	for _, s := range dst.Tree().Sessions {
		byName[s.Name] = s
	}
	if byName["target"].JumpID == "" || byName["target"].JumpID != byName["jump"].ID {
		t.Errorf("target jumps via %q, want %q", byName["target"].JumpID, byName["jump"].ID)
	}
	if got := alone.Tree().Sessions; len(got) != 1 || got[0].JumpID != "" {
		t.Errorf("session imported without its jump host: %+v", got)
	}
}

func TestForwardsAreValidated(t *testing.T) {
	ok := []Forward{{Listen: 8443, Host: "10.0.0.1", Port: 443}, {Listen: 2200, Host: "sw1", Port: 22}}
	cases := map[string][]Forward{
		"listen port 0":   {{Listen: 0, Host: "h", Port: 22}},
		"target port big": {{Listen: 1, Host: "h", Port: 70000}},
		"empty host":      {{Listen: 1, Host: " ", Port: 22}},
		"host with space": {{Listen: 1, Host: "a b", Port: 22}},
		"same listen":     {{Listen: 9, Host: "a", Port: 1}, {Listen: 9, Host: "b", Port: 2}},
		"too many":        make([]Forward, MaxForwards+1),
	}
	if err := (Session{Host: "h", Forwards: ok}).Normalize().Validate(); err != nil {
		t.Errorf("valid forwards refused: %v", err)
	}
	for name, fs := range cases {
		if err := (Session{Host: "h", Forwards: fs}).Normalize().Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestNormalizeDropsJumpAndForwardsOutsideSSH(t *testing.T) {
	for _, p := range []string{ProtocolRDP, ProtocolSerial} {
		s := Session{Protocol: p, Host: "h", SerialPort: "COM1", JumpID: "x", Forwards: []Forward{{Listen: 1, Host: "a", Port: 1}}}.Normalize()
		if s.JumpID != "" || s.Forwards != nil {
			t.Errorf("%s keeps SSH-only fields: %+v", p, s)
		}
	}
}

func TestNormalizeCopiesForwards(t *testing.T) {
	orig := []Forward{{Listen: 1, Host: " a ", Port: 2}}

	got := Session{Host: "h", Forwards: orig}.Normalize()
	got.Forwards[0].Port = 99

	if orig[0].Port != 2 || got.Forwards[0].Host != "a" {
		t.Errorf("forwards shared or not trimmed: %+v %+v", orig, got.Forwards)
	}
}

func TestImportDropsBrokenJumpChains(t *testing.T) {
	b := Bundle{Format: ExportFormat, Version: exportVersion, Sessions: []Session{
		{ID: "a", Name: "a", Host: "a.example", JumpID: "b"},
		{ID: "b", Name: "b", Host: "b.example", JumpID: "a"}, // loop
		{ID: "r", Name: "r", Protocol: ProtocolRDP, Host: "win"},
		{ID: "c", Name: "c", Host: "c.example", JumpID: "r"}, // jump over RDP
		{ID: "ok", Name: "ok", Host: "ok.example", JumpID: "plain"},
		{ID: "plain", Name: "plain", Host: "plain.example"},
	}}
	st, _ := openTemp(t)

	if _, err := st.ImportBundle(b); err != nil {
		t.Fatal(err)
	}

	byName := map[string]Session{}
	for _, s := range st.Tree().Sessions {
		byName[s.Name] = s
	}
	if byName["a"].JumpID != "" && byName["b"].JumpID != "" {
		t.Errorf("loop kept: a→%q b→%q", byName["a"].JumpID, byName["b"].JumpID)
	}
	if byName["c"].JumpID != "" {
		t.Errorf("jump over an RDP session kept: %q", byName["c"].JumpID)
	}
	if byName["ok"].JumpID != byName["plain"].ID {
		t.Errorf("valid jump lost: %q", byName["ok"].JumpID)
	}
}
