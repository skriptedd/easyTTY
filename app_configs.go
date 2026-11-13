package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"easytty/internal/automation"
	"easytty/internal/store"
)

// Config backups: one folder for running-configs, filled two ways. Devices
// with a network path to this PC send them by TFTP (firmware server, uploads
// on); over a console without network easyTTY reads them in the open tab.

const (
	configTimeout     = 5 * time.Minute // a big config over 9600 baud takes a while
	configIdleTimeout = 60 * time.Second
	configMaxName     = 80
)

// showRunning is the command per device type; IOS, NX-OS and most others know it.
const showRunning = "show running-config"

// defaultConfigDir is <Dokumente>\easyTTY\Configs (portable: next to the data).
func (a *App) defaultConfigDir() string {
	if a.portable {
		return filepath.Join(a.dir, "Configs")
	}
	return filepath.Join(filepath.Dir(defaultLogDir(a.home)), "Configs")
}

// ConfigDir returns the folder for running-config backups and creates it.
func (a *App) ConfigDir() (string, error) {
	dir := a.GetSettings().ConfigDir
	if dir == "" {
		dir = a.defaultConfigDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("Config-Ordner %s: %w", dir, err)
	}
	return dir, nil
}

// PickConfigDir lets the user choose the backup folder and saves the choice.
func (a *App) PickConfigDir() (string, error) {
	cur, _ := a.ConfigDir()
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Ordner für gesicherte Running-Configs", DefaultDirectory: cur, CanCreateDirectories: true,
	})
	if err != nil || dir == "" {
		return cur, err
	}
	if _, err := a.updateSettings(func(s store.Settings) (store.Settings, error) {
		s.ConfigDir = dir
		return s, nil
	}); err != nil {
		return cur, err
	}
	return dir, nil
}

// OpenConfigDir shows the backup folder in Explorer.
func (a *App) OpenConfigDir() error {
	dir, err := a.ConfigDir()
	if err != nil {
		return err
	}
	return exec.Command(explorerExe(), dir).Start()
}

// SaveRunningConfig reads the running-config in an open tab (SSH or serial,
// the user watches it scroll by) and stores it in the backup folder. Returns
// the file path.
func (a *App) SaveRunningConfig(tabID string) (string, error) {
	dir, err := a.ConfigDir()
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	meta := a.tabMeta[tabID]
	a.mu.Unlock()
	stream, err := a.terms.Tap(tabID)
	if err != nil {
		return "", fmt.Errorf("Tab nicht bereit: %w", err)
	}
	defer stream.Close()
	ctx, cancel := context.WithTimeout(a.ctx, configTimeout)
	defer cancel()
	res := automation.RunCommands(ctx, stream, []string{showRunning},
		automation.Options{IdleTimeout: configIdleTimeout}, nil)
	text, err := runningConfigText(res)
	if err != nil {
		return "", err
	}
	path := uniqueFile(dir, outputFileName(meta.title, "", time.Now(), ".cfg"))
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// runningConfigText takes the config out of a run, or says why there is none.
func runningConfigText(res automation.Result) (string, error) {
	if res.Error != "" {
		return "", errors.New("Running-Config nicht gelesen: " + res.Error)
	}
	if len(res.Steps) == 0 {
		return "", errors.New("Running-Config nicht gelesen: keine Antwort")
	}
	st := res.Steps[0]
	if st.Error != "" {
		return "", errors.New("Running-Config nicht gelesen: " + st.Error)
	}
	text := strings.TrimSpace(st.Output)
	if text == "" {
		return "", errors.New("Das Gerät hat keine Config ausgegeben (angemeldet und im enable-Modus?)")
	}
	return text + "\n", nil
}

// outputFileName is "<tab title>[_<label>]_<date>_<time><ext>" with only harmless characters.
func outputFileName(title, label string, at time.Time, ext string) string {
	name := safeName(title, "config")
	if l := safeName(label, ""); l != "" {
		name += "_" + l
	}
	return fmt.Sprintf("%s_%s%s", name, at.Format("2006-01-02_150405"), ext)
}

// safeName keeps letters, digits, - _ . and turns the rest into _.
func safeName(s, fallback string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	name := strings.Trim(b.String(), "._")
	if name == "" {
		name = fallback
	}
	if len(name) > configMaxName {
		name = name[:configMaxName]
	}
	return name
}

// uniqueFile returns dir/name, or "name (1).ext" … when that exists.
func uniqueFile(dir, name string) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	p := filepath.Join(dir, name)
	for i := 1; ; i++ {
		if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
}

// OpenConfigFile opens a saved config with the program Windows uses for it.
// Only files in the config folder, so the UI cannot be used to start others.
func (a *App) OpenConfigFile(path string) error {
	dir, err := a.ConfigDir()
	if err != nil {
		return err
	}
	if !insideDir(dir, path) {
		return errors.New("Datei liegt nicht im Config-Ordner")
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return exec.Command(explorerExe(), path).Start()
}

// insideDir reports whether path lies in dir (or below).
func insideDir(dir, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && rel != "." && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// RunSnippetToFile runs the commands of a snippet in an open tab (the user
// watches them) and writes every command with its output to a text file in
// the config folder. Returns the file path.
func (a *App) RunSnippetToFile(tabID, name, text string) (string, error) {
	cmds := automation.SplitCommands(text)
	if len(cmds) == 0 {
		return "", errors.New("Der Schnipsel enthält keinen Befehl")
	}
	if err := automation.CheckCommands(cmds); err != nil {
		return "", err
	}
	dir, err := a.ConfigDir()
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	meta := a.tabMeta[tabID]
	a.mu.Unlock()
	stream, err := a.terms.Tap(tabID)
	if err != nil {
		return "", fmt.Errorf("Tab nicht bereit: %w", err)
	}
	defer stream.Close()
	ctx, cancel := context.WithTimeout(a.ctx, configTimeout)
	defer cancel()
	at := time.Now()
	res := automation.RunCommands(ctx, stream, cmds, automation.Options{IdleTimeout: configIdleTimeout}, nil)
	if len(res.Steps) == 0 && res.Error != "" {
		return "", errors.New("Schnipsel nicht ausgeführt: " + res.Error)
	}
	path := uniqueFile(dir, outputFileName(meta.title, name, at, ".txt"))
	if err := os.WriteFile(path, []byte(snippetReport(meta.title, name, at, res)), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// snippetReport is the text file: a header, then each command with its output.
func snippetReport(title, name string, at time.Time, res automation.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "! easyTTY – %s auf %s, %s\n", name, title, at.Format("02.01.2006 15:04:05"))
	for _, st := range res.Steps {
		fmt.Fprintf(&b, "!\n! %s%s\n", res.Prompt, st.Command)
		if out := strings.TrimRight(st.Output, "\n "); out != "" {
			b.WriteString(out + "\n")
		}
		if st.Error != "" {
			fmt.Fprintf(&b, "! FEHLER: %s\n", st.Error)
		}
	}
	if res.Error != "" {
		fmt.Fprintf(&b, "!\n! Abgebrochen: %s\n", res.Error)
	}
	return b.String()
}
