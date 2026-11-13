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
	startCrashLog()
	app := NewApp()
	webviewData := "" // default: %APPDATA%\easyTTY.exe
	if dir, ok := portableDir(); ok {
		webviewData = webviewDir(dir)
	}
	err := wails.Run(&options.App{
		Title:            "easyTTY",
		Width:            1400,
		Height:           880,
		MinWidth:         860,
		MinHeight:        520,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 13, G: 15, B: 20, A: 255},
		OnStartup:        app.startup,
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
