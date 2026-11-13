package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"easytty/internal/store"
)

const (
	shareExt      = ".easytty.json"
	maxShareBytes = 20 << 20 // a share file is small; refuse anything absurd
)

var shareFilter = []runtime.FileFilter{{DisplayName: "easyTTY-Export (*.easytty.json)", Pattern: "*.easytty.json;*.json"}}

// ExportShare writes folders/sessions (and optionally settings) to a file the
// user picks. Personal data (user, passwords, key/log paths) is never exported.
// Returns the written path ("" if the dialog was cancelled).
func (a *App) ExportShare(folderIDs, sessionIDs []string, all, withSettings bool) (string, error) {
	b := a.store.Export(folderIDs, sessionIDs, all)
	if withSettings {
		s := store.ShareableSettings(a.GetSettings())
		b.Settings = &s
	}
	if len(b.Sessions) == 0 && len(b.Folders) == 0 && b.Settings == nil {
		return "", errors.New("Nichts zum Exportieren ausgewählt")
	}
	name := "easyTTY-Export-" + time.Now().Format("2006-01-02") + shareExt
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title: "easyTTY-Export speichern", DefaultFilename: name, Filters: shareFilter,
	})
	if err != nil || path == "" {
		return "", err
	}
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", fmt.Errorf("Export schreiben: %w", err)
	}
	return path, nil
}

// SharePreview describes a picked share file before it is imported.
type SharePreview struct {
	Path        string `json:"path"`
	Folders     int    `json:"folders"`
	Sessions    int    `json:"sessions"`
	HasSettings bool   `json:"hasSettings"`
	Exported    string `json:"exported"`
}

// PickShare lets the user choose a share file and summarises it.
func (a *App) PickShare() (*SharePreview, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "easyTTY-Export importieren", Filters: shareFilter})
	if err != nil || path == "" {
		return nil, err
	}
	b, err := readShare(path)
	if err != nil {
		return nil, err
	}
	return &SharePreview{
		Path: path, Folders: len(b.Folders), Sessions: len(b.Sessions),
		HasSettings: b.Settings != nil, Exported: b.Exported.Local().Format("02.01.2006 15:04"),
	}, nil
}

// ApplyShare imports a share file; settings only if withSettings (local log
// folder and first-run state are always kept).
func (a *App) ApplyShare(path string, withSettings bool) (store.ImportSummary, error) {
	b, err := readShare(path)
	if err != nil {
		return store.ImportSummary{}, err
	}
	sum, err := a.store.ImportBundle(b)
	if err != nil {
		return sum, err
	}
	if withSettings && b.Settings != nil {
		if _, err := a.updateSettings(func(local store.Settings) (store.Settings, error) {
			return mergeSharedSettings(*b.Settings, local), nil
		}); err != nil {
			return sum, err
		}
		sum.SettingsApplied = true
	}
	return sum, nil
}

// mergeSharedSettings takes look and habits from a shared file and keeps
// everything that belongs to this PC or protects it.
func mergeSharedSettings(shared, local store.Settings) store.Settings {
	merged := shared
	merged.LogDir, merged.ConfigDir = local.LogDir, local.ConfigDir
	merged.FirstRunDone = local.FirstRunDone
	merged.McpEnabled, merged.McpPort, merged.McpToken, merged.McpShowAuto = local.McpEnabled, local.McpPort, local.McpToken, local.McpShowAuto
	merged.McpReadTabs = local.McpReadTabs
	// A shared file brings look and habits, it must not switch off safety nets.
	merged.PasteConfirm, merged.ConfirmClose = local.PasteConfirm, local.ConfirmClose
	merged.UpdateCheck = local.UpdateCheck
	merged.LogEnabled, merged.LogRaw = local.LogEnabled, local.LogRaw
	// Shared snippets come on top; the user's own ones are never replaced.
	merged.Snippets = store.MergeSnippets(local.Snippets, shared.Snippets)
	return merged
}

func readShare(path string) (store.Bundle, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return store.Bundle{}, err
	}
	if fi.Size() > maxShareBytes {
		return store.Bundle{}, errors.New("Datei ist zu groß für einen easyTTY-Export")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return store.Bundle{}, err
	}
	var b store.Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return store.Bundle{}, fmt.Errorf("keine gültige easyTTY-Exportdatei: %w", err)
	}
	if b.Format != store.ExportFormat {
		return store.Bundle{}, errors.New("keine easyTTY-Exportdatei")
	}
	return b, nil
}
