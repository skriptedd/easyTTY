package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"easytty/internal/store"
)

// A shell on this PC must never be reachable for an AI client: not listed,
// not typed into, not read.
func TestMcpCannotReachLocalTabs(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "sessions.json"), plainProtector{})
	if err != nil {
		t.Fatal(err)
	}
	ps, err := st.SaveSession(store.Session{Protocol: store.ProtocolLocal, Name: "PS"})
	if err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	a.store, a.settingsPath, a.home = st, filepath.Join(dir, "settings.json"), dir
	a.localTabs["t1"] = true
	a.tabMeta["t1"] = tabMeta{title: "PowerShell", sessionID: ps.ID}
	b := mcpBackend{a}

	if got := b.Sessions(); len(got) != 0 {
		t.Errorf("local session listed: %+v", got)
	}
	if tabs := b.Tabs(); len(tabs) != 0 {
		t.Errorf("local tab listed: %+v", tabs)
	}
	if err := b.SendToTab(context.Background(), "test", "t1", "Remove-Item C:\\", true); !errors.Is(err, errLocalTab) {
		t.Errorf("SendToTab = %v, want errLocalTab", err)
	}
	if _, _, err := b.resolveSessions([]string{"PS"}); !errors.Is(err, errLocalTab) {
		t.Errorf("run_commands on a local session = %v, want errLocalTab", err)
	}
	if _, err := a.openHeadless(context.Background(), ps.ID); err == nil {
		t.Error("a task must not run in a local shell")
	}
}

func TestOpenLocalShellRejectsUnknownShell(t *testing.T) {
	a := NewApp()
	if err := a.OpenLocalShell("t1", "bash -c evil", 80, 24); err == nil {
		t.Error("unknown shell accepted")
	}
	if a.isLocalTab("t1") {
		t.Error("a rejected shell must not mark the tab")
	}
}
