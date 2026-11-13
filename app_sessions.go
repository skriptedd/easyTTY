package main

import (
	"fmt"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"

	"easytty/internal/conn"
	"easytty/internal/putty"
	"easytty/internal/store"
)

// GetTree returns all folders and sessions.
func (a *App) GetTree() store.Tree { return a.store.Tree() }

// SaveSession creates or updates a session. password: nil = unchanged,
// "" = remove stored password, otherwise store it encrypted.
func (a *App) SaveSession(s store.Session, password *string) (store.Session, error) {
	saved, err := a.store.SaveSession(a.applyNaming(s))
	if err != nil {
		return store.Session{}, err
	}
	if password != nil {
		if err := a.store.SetPassword(saved.ID, *password); err != nil {
			return saved, err
		}
	}
	return a.store.Session(saved.ID)
}

// DeleteSession removes a session.
func (a *App) DeleteSession(id string) error { return a.store.DeleteSession(id) }

// DuplicateSession copies a session (the copy's name follows the naming scheme).
func (a *App) DuplicateSession(id string) (store.Session, error) {
	dup, err := a.store.DuplicateSession(id)
	if err != nil {
		return dup, err
	}
	if named := a.applyNaming(dup); named.Name != dup.Name {
		return a.store.SaveSession(named)
	}
	return dup, nil
}

// applyNaming formats the session name with the global naming scheme.
// Normalize first, so a name defaulted from host/COM port is formatted too.
func (a *App) applyNaming(s store.Session) store.Session {
	out := s.Normalize()
	if formatted := a.GetSettings().FormatName(out.Name); formatted != "" {
		out.Name = formatted
	}
	return out
}

// FormatSessionName previews the naming scheme (session editor hint).
func (a *App) FormatSessionName(name string) string { return a.GetSettings().FormatName(name) }

// ApplyNamingToAll renames every saved session to the current scheme.
// Returns how many names changed.
func (a *App) ApplyNamingToAll() (int, error) {
	changed := 0
	for _, s := range a.store.Tree().Sessions {
		named := a.applyNaming(s)
		if named.Name == s.Name {
			continue
		}
		if _, err := a.store.SaveSession(named); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

// MoveSession moves a session to a folder ("" = root).
func (a *App) MoveSession(id, folderID string) error { return a.store.MoveSession(id, folderID) }

// SaveFolder creates or renames a folder.
func (a *App) SaveFolder(f store.Folder) (store.Folder, error) { return a.store.SaveFolder(f) }

// SaveFolderLayout stores which session opens in which tile (nil = remove).
func (a *App) SaveFolderLayout(id string, l *store.Layout) (store.Folder, error) {
	return a.store.SaveFolderLayout(id, l)
}

// MoveFolder re-parents a folder.
func (a *App) MoveFolder(id, parentID string) error { return a.store.MoveFolder(id, parentID) }

// DeleteFolder removes a folder; contents move up one level.
// DeleteFolder removes a folder; withSessions also deletes every session and
// subfolder inside, otherwise they move one level up.
func (a *App) DeleteFolder(id string, withSessions bool) error {
	if withSessions {
		return a.store.DeleteFolderWithSessions(id)
	}
	return a.store.DeleteFolder(id)
}

// DeleteSessions removes several sessions at once.
func (a *App) DeleteSessions(ids []string) error { return a.store.DeleteSessions(ids) }

// ImportResult summarises an import.
type ImportResult struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
}

// ImportPutty imports all saved PuTTY sessions into the folder "PuTTY Import".
func (a *App) ImportPutty() (ImportResult, error) {
	sessions, skipped, err := putty.LoadSessions()
	if err != nil {
		return ImportResult{}, err
	}
	for i := range sessions {
		sessions[i] = a.applyNaming(sessions[i])
	}
	sessions = autoColor(a.store.Tree().Sessions, sessions)
	n, err := a.store.Import("PuTTY Import", sessions)
	return ImportResult{Imported: n, Skipped: skipped + len(sessions) - n}, err
}

// GetSettings returns the global preferences with the log folder filled in.
// settingsForUpdate loads settings for a read-modify-write; an unreadable or
// damaged settings.json is not overwritten with defaults (that would wipe the
// MCP token, log folder …).
func (a *App) settingsForUpdate() (store.Settings, error) {
	s, err := store.LoadSettings(a.settingsPath)
	if err != nil {
		return s, fmt.Errorf("Einstellungen nicht lesbar, nichts gespeichert: %w", err)
	}
	return s, nil
}

// updateSettings runs one read-modify-write of settings.json under a lock, so
// two writers (dialog, MCP token, import) cannot undo each other's change.
func (a *App) updateSettings(change func(store.Settings) (store.Settings, error)) (store.Settings, error) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	cur, err := a.settingsForUpdate()
	if err != nil {
		return cur, err
	}
	next, err := change(cur)
	if err != nil {
		return cur, err
	}
	return store.SaveSettings(a.settingsPath, next)
}

func (a *App) GetSettings() store.Settings {
	s, _ := store.LoadSettings(a.settingsPath)
	if s.LogDir == "" {
		s.LogDir = a.defaultLogDir()
	}
	return s
}

// SaveSettings persists the global preferences.
func (a *App) SaveSettings(s store.Settings) (store.Settings, error) {
	saved, err := a.updateSettings(func(cur store.Settings) (store.Settings, error) {
		if s.McpToken == "" { // the UI never needs to send the token back
			s.McpToken = cur.McpToken
		}
		return s, nil
	})
	if err == nil && saved.LogDir == "" {
		saved.LogDir = a.defaultLogDir()
	}
	if err == nil {
		a.applyMCP()
		if h := a.runningRDP(); h != nil {
			h.SetAppKeys(saved.AltShortcuts)
		}
	}
	return saved, err
}

// defaultLogDir is <Dokumente>\easyTTY\Logs, using the real Documents folder
// (which may be redirected, e.g. to OneDrive).
func defaultLogDir(home string) string {
	docs, err := windows.KnownFolderPath(windows.FOLDERID_Documents, 0)
	if err != nil || docs == "" {
		docs = filepath.Join(home, "Documents")
	}
	return filepath.Join(docs, appName, "Logs")
}

// PickLogDir opens a folder dialog for the global log folder.
func (a *App) PickLogDir(current string) (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Ordner für Session-Logs", DefaultDirectory: current, CanCreateDirectories: true,
	})
}

// ListSerialPorts returns available COM ports with device names.
func (a *App) ListSerialPorts() ([]conn.SerialPortInfo, error) { return conn.SerialPortInfos() }

// PickKeyFile opens a file dialog for a private key.
func (a *App) PickKeyFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Privaten Schlüssel wählen",
		DefaultDirectory: a.home + `\.ssh`,
		ShowHiddenFiles:  true,
	})
}

// PickLogFile opens a save dialog for a session log path.
func (a *App) PickLogFile() (string, error) {
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Log-Datei",
		DefaultFilename: "{session}_{date}_{time}.log",
	})
}

// defaultLogDir is the log folder used when the settings name none:
// next to the exe in portable mode, otherwise in the user's Documents.
func (a *App) defaultLogDir() string {
	if a.portable {
		return filepath.Join(a.dir, "Logs")
	}
	return defaultLogDir(a.home)
}

// PatchSessions changes the given fields for all sessions of a folder ("" =
// top level; recursive includes subfolders, "" + recursive = every session).
func (a *App) PatchSessions(folderID string, recursive bool, p store.SessionPatch) (int, error) {
	return a.store.PatchSessions(folderID, recursive, p)
}

// DefaultHighlights returns the highlight rules of a fresh installation
// ("Standard wiederherstellen" in the settings).
func (a *App) DefaultHighlights() []store.Highlight { return store.DefaultHighlights() }
