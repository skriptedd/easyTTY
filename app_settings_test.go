package main

import (
	"path/filepath"
	"sync"
	"testing"

	"easytty/internal/store"
)

func TestUpdateSettingsLosesNoConcurrentChange(t *testing.T) {
	a := &App{settingsPath: filepath.Join(t.TempDir(), "settings.json")}
	const writers = 40
	var wg sync.WaitGroup

	for range writers {
		wg.Go(func() {
			if _, err := a.updateSettings(func(s store.Settings) (store.Settings, error) {
				s.PasteLineDelay++
				return s, nil
			}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()

	got, err := store.LoadSettings(a.settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.PasteLineDelay != writers {
		t.Errorf("PasteLineDelay = %d, want %d", got.PasteLineDelay, writers)
	}
}

func TestMergeSharedSettingsKeepsLocalSafetyNets(t *testing.T) {
	local := store.DefaultSettings()
	local.McpEnabled, local.McpToken, local.LogDir = true, "secret", `D:\Logs`
	shared := store.DefaultSettings()
	shared.Theme, shared.FontSize = "light", 18
	shared.PasteConfirm, shared.ConfirmClose, shared.UpdateCheck, shared.LogEnabled = false, false, false, false

	got := mergeSharedSettings(shared, local)

	if got.Theme != "light" || got.FontSize != 18 {
		t.Errorf("look not taken over: %+v", got)
	}
	if !got.PasteConfirm || !got.ConfirmClose || !got.UpdateCheck || !got.LogEnabled {
		t.Errorf("safety switch changed by a shared file: %+v", got)
	}
	if !got.McpEnabled || got.McpToken != "secret" || got.LogDir != `D:\Logs` {
		t.Errorf("local settings lost: %+v", got)
	}
}
