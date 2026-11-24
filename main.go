package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if isBridgeMode() {
		os.Exit(runBridge())
	}
	if target, ok := finishUpdateTarget(); ok {
		os.Exit(runFinishUpdate(target))
	}
	startCrashLog()
	removeUpdateLeftovers()
	app := NewApp()
	webviewData := "" // default: %APPDATA%\easyTTY.exe
	if dir, ok := portableDir(); ok {
		webviewData = webviewDir(dir)
	}
	startState := options.Normal
	if dir, _, err := dataDir(); err == nil {
		if s, ok := loadWindowState(dir); ok && s.Max {
			startState = options.Maximised // no jump from a centred window
		}
	}
	err := wails.Run(&options.App{
		Title:            "easyTTY",
		Width:            1400,
		Height:           880,
		WindowStartState: startState,
		MinWidth:         860,
		MinHeight:        520,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 13, G: 15, B: 20, A: 255},
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		OnBeforeClose:    app.beforeClose,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
		Windows: &windows.Options{
			Theme:                windows.Dark,
			DisablePinchZoom:     true,
			WebviewGpuIsDisabled: false,
			WebviewUserDataPath:  webviewData,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
