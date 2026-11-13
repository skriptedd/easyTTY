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

	"easytty/internal/store"
	"easytty/internal/update"
)

// updateAPI is the "latest release" endpoint. Builds for another host set it
// with -ldflags "-X main.updateAPI=https://…/releases/latest".
var updateAPI = "https://api.github.com/repos/skriptedd/easyTTY/releases/latest"

// releaseKey is the public half of the key that signs every release
// (SHA256SUMS.txt.sig). Updates signed with any other key are refused.
// Builds for another publisher set their own with -ldflags "-X main.releaseKey=…".
var releaseKey = "R2Drs0jSEV6OX5eDwUjJBj+zFQEUtMy6UaFmnSbyAPc="

const (
	devVersion         = "0.0.0-dev"
	updateStartDelay   = 4 * time.Second
	updateCheckTimeout = 30 * time.Second
	updateTimeout      = 15 * time.Minute
	progressStep       = 2 // percent between two progress events
)

// UpdateInfo tells the UI whether a newer version exists.
type UpdateInfo struct {
	Current   string `json:"current"`
	Version   string `json:"version"`
	Available bool   `json:"available"`
	Notes     string `json:"notes"`
	Page      string `json:"page"`
}

func updateClient() (*update.Client, error) {
	key, err := update.ParsePublicKey(releaseKey)
	if err != nil {
		return nil, err
	}
	return update.New(updateAPI, appName+"/"+appVersion, key)
}

func (a *App) latestRelease(ctx context.Context) (*update.Client, update.Release, error) {
	c, err := updateClient()
	if err != nil {
		return nil, update.Release{}, err
	}
	rel, err := c.Latest(ctx)
	return c, rel, err
}

// CheckUpdate asks the release server for the newest version.
func (a *App) CheckUpdate() (UpdateInfo, error) {
	ctx, cancel := context.WithTimeout(a.ctx, updateCheckTimeout)
	defer cancel()
	info := UpdateInfo{Current: appVersion}
	_, rel, err := a.latestRelease(ctx)
	if errors.Is(err, update.ErrNoRelease) {
		return info, nil
	}
	if err != nil {
		return info, err
	}
	info.Version, info.Notes, info.Page = rel.Version, rel.Notes, rel.Page
	info.Available = update.Newer(appVersion, rel.Version)
	return info, nil
}

// startUpdateCheck looks for a new version shortly after the start and tells
// the UI. Failures stay quiet: no network is not worth a message.
func (a *App) startUpdateCheck() {
	if appVersion == devVersion {
		return
	}
	// The previous exe stays as easyTTY.old.exe until the next update: the way
	// back if a new version does not suit.
	if s, _ := store.LoadSettings(a.settingsPath); !s.UpdateCheck {
		return
	}
	go func() {
		select {
		case <-time.After(updateStartDelay):
		case <-a.ctx.Done():
			return
		}
		if info, err := a.CheckUpdate(); err == nil && info.Available {
			a.emit("update:available", info)
		}
	}()
}

// InstallUpdate downloads the given version, checks it, puts it in place of
// the running exe and restarts easyTTY. version must be the one the user
// confirmed; if the server offers something else by now, nothing happens.
func (a *App) InstallUpdate(version string) error {
	if appVersion == devVersion {
		return errors.New("Entwicklungs-Build: Updates gibt es nur für veröffentlichte Versionen")
	}
	if !a.updating.CompareAndSwap(false, true) {
		return errors.New("Update läuft schon")
	}
	defer a.updating.Store(false)

	ctx, cancel := context.WithTimeout(a.ctx, updateTimeout)
	defer cancel()
	c, rel, err := a.latestRelease(ctx)
	if err != nil {
		return err
	}
	if rel.Version != version || !update.Newer(appVersion, rel.Version) {
		return fmt.Errorf("Version %s wird nicht mehr angeboten, bitte neu suchen", version)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	fresh := strings.TrimSuffix(exe, ".exe") + ".new.exe"
	if err := c.Download(ctx, rel, fresh, a.updateProgress()); err != nil {
		return err
	}
	// Sessions, settings and host keys of this version, before the new one
	// may convert them. No backup, no update.
	if _, err := backupData(a.dir, appVersion, time.Now()); err != nil {
		os.Remove(fresh)
		return fmt.Errorf("Sicherung vor dem Update fehlgeschlagen, nichts geändert: %w", err)
	}
	if err := update.Swap(exe, fresh); err != nil {
		os.Remove(fresh)
		return err
	}
	a.restartWith(exe)
	return nil
}

// updateProgress emits "update:progress" every few percent.
func (a *App) updateProgress() func(done, total int64) {
	last := -progressStep
	return func(done, total int64) {
		if total <= 0 {
			return
		}
		if pct := int(done * 100 / total); pct >= last+progressStep || pct == 100 && last != 100 {
			last = pct
			a.emit("update:progress", pct)
		}
	}
}

// restartWith closes the window without asking and starts exe once
// everything is shut down.
func (a *App) restartWith(exe string) {
	a.mu.Lock()
	a.relaunch = exe
	a.mu.Unlock()
	a.quit()
}

// relaunchIfWanted runs at the very end of shutdown.
func (a *App) relaunchIfWanted() {
	a.mu.Lock()
	exe := a.relaunch
	a.mu.Unlock()
	if exe == "" {
		return
	}
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err == nil {
		_ = cmd.Process.Release()
	}
}

func (a *App) restarting() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.relaunch != ""
}
