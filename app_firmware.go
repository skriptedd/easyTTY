package main

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"easytty/internal/fwserver"
)

const fwEventKeep = 200 // events kept for the UI log

// FirmwareState is what the firmware dialog shows on open.
type FirmwareState struct {
	Info       fwserver.Info      `json:"info"`
	DefaultDir string             `json:"defaultDir"`
	Addresses  []fwserver.Address `json:"addresses"`
	Events     []fwserver.Event   `json:"events"`
}

func (a *App) firmware() *fwserver.Server {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fw == nil {
		a.fw = fwserver.New(a.fwEvent)
	}
	return a.fw
}

func (a *App) fwEvent(e fwserver.Event) {
	a.mu.Lock()
	a.fwEvents = append(a.fwEvents, e)
	if len(a.fwEvents) > fwEventKeep {
		a.fwEvents = append([]fwserver.Event(nil), a.fwEvents[len(a.fwEvents)-fwEventKeep:]...)
	}
	a.mu.Unlock()
	a.emit("fw:event", e)
}

// defaultFirmwareDir sits beside the log folder (Documents\easyTTY\Firmware,
// or next to the exe in portable mode).
func (a *App) defaultFirmwareDir() string {
	if a.portable {
		return filepath.Join(a.dir, "Firmware")
	}
	return filepath.Join(filepath.Dir(defaultLogDir(a.home)), "Firmware")
}

// FirmwareState returns server state, local addresses and recent events.
func (a *App) FirmwareState() FirmwareState {
	info := a.firmware().Info()
	a.mu.Lock()
	events := append([]fwserver.Event(nil), a.fwEvents...)
	a.mu.Unlock()
	return FirmwareState{Info: info, DefaultDir: a.defaultFirmwareDir(), Addresses: fwserver.LocalAddresses(), Events: events}
}

// FirmwareStart serves cfg.Dir read-only; the default folder is created on demand.
func (a *App) FirmwareStart(cfg fwserver.Config) (fwserver.Info, error) {
	if cfg.Dir == "" {
		cfg.Dir = a.defaultFirmwareDir()
	}
	if cfg.Dir == a.defaultFirmwareDir() {
		if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
			return fwserver.Info{}, err
		}
	}
	return a.firmware().Start(cfg)
}

// FirmwareStop shuts the server down.
func (a *App) FirmwareStop() { a.firmware().Stop() }

// FirmwareFiles lists the images in dir with size and MD5.
func (a *App) FirmwareFiles(dir string) ([]fwserver.File, error) {
	if dir == "" {
		dir = a.defaultFirmwareDir()
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return []fwserver.File{}, nil
	}
	files, err := fwserver.ListFiles(dir)
	if files == nil {
		files = []fwserver.File{}
	}
	return files, err
}

// PickFirmwareDir opens a folder dialog.
func (a *App) PickFirmwareDir(current string) (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Ordner mit Firmware-Images", DefaultDirectory: current, CanCreateDirectories: true,
	})
}

// OpenFirmwareDir opens the folder in Explorer (creating the default one).
func (a *App) OpenFirmwareDir(dir string) error {
	if dir == "" {
		dir = a.defaultFirmwareDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return exec.Command(explorerExe(), dir).Start()
}
