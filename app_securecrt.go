package main

import (
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"easytty/internal/securecrt"
	"easytty/internal/store"
)

// SecureCRTResult reports a SecureCRT import.
type SecureCRTResult struct {
	NotFound bool     `json:"notFound"` // auto-detect found no SecureCRT sessions
	Source   string   `json:"source"`
	Imported int      `json:"imported"`
	Existing int      `json:"existing"` // already in easyTTY
	Skipped  []string `json:"skipped"`  // Telnet, RDP … with name
}

// ImportSecureCRT imports SecureCRT sessions into the folder "SecureCRT Import".
// from: "" = find SecureCRT's Sessions folder, "dir" = pick that folder,
// "xml" = pick an XML export. Passwords are not imported.
func (a *App) ImportSecureCRT(from string) (SecureCRTResult, error) {
	var path string
	var err error
	switch from {
	case "":
		var ok bool
		if path, ok = securecrt.DefaultDir(securecrt.ConfigPath()); !ok {
			return SecureCRTResult{NotFound: true}, nil
		}
	case "dir":
		path, err = runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "SecureCRT-Ordner \"Sessions\" wählen"})
	case "xml":
		path, err = runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
			Title:   "SecureCRT-Export (XML) wählen",
			Filters: []runtime.FileFilter{{DisplayName: "SecureCRT-Export (*.xml)", Pattern: "*.xml"}},
		})
	default:
		return SecureCRTResult{}, fmt.Errorf("unbekannte Quelle %q", from)
	}
	if err != nil || path == "" {
		return SecureCRTResult{}, err
	}
	return a.importSecureCRT(path)
}

func (a *App) importSecureCRT(path string) (SecureCRTResult, error) {
	r, err := securecrt.Load(path)
	if err != nil {
		return SecureCRTResult{}, err
	}
	const top = "scrt-root"
	b := store.Bundle{Format: store.ExportFormat, Version: 1, Folders: []store.Folder{{ID: top, Name: "SecureCRT Import"}}}
	for _, f := range r.Folders {
		if f.ParentID == "" {
			f.ParentID = top
		}
		b.Folders = append(b.Folders, f)
	}
	for _, s := range r.Sessions {
		if s.FolderID == "" {
			s.FolderID = top
		}
		b.Sessions = append(b.Sessions, a.applyNaming(s))
	}
	b.Sessions = autoColor(a.store.Tree().Sessions, b.Sessions)
	sum, err := a.store.ImportOwn(b)
	return SecureCRTResult{Source: path, Imported: sum.Sessions, Existing: sum.Skipped, Skipped: r.Skipped}, err
}
