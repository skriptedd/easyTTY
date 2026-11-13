package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWebviewDirAvoidsOneDrive(t *testing.T) {
	root := t.TempDir()
	oneDrive := filepath.Join(root, "OneDrive - Firma")
	local := filepath.Join(root, "Local")
	t.Setenv("OneDrive", "")
	t.Setenv("OneDriveConsumer", "")
	t.Setenv("OneDriveCommercial", oneDrive)
	t.Setenv("LOCALAPPDATA", local)

	plain := filepath.Join(root, "Tools", "easyTTY", "easyTTY-data")
	synced := filepath.Join(oneDrive, "Desktop", "easyTTY", "easyTTY-data")
	twin := filepath.Join(oneDrive+"-old", "easyTTY-data") // starts with the same letters, is not inside

	if got := webviewDir(plain); got != filepath.Join(plain, "webview") {
		t.Errorf("outside OneDrive: %s", got)
	}
	got := webviewDir(synced)
	if !strings.HasPrefix(got, filepath.Join(local, "easyTTY", "portable-webview")) {
		t.Errorf("inside OneDrive: %s", got)
	}
	if again := webviewDir(synced); again != got {
		t.Errorf("not stable: %s vs %s", got, again)
	}
	if webviewDir(twin) != filepath.Join(twin, "webview") {
		t.Errorf("folder next to OneDrive treated as inside")
	}
}
