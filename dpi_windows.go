//go:build windows

package main

import "golang.org/x/sys/windows"

// Sharp text on high-DPI screens even without an embedded manifest
// (build.ps1 -Resources adds one; then this call is a harmless no-op).
func init() {
	const perMonitorAwareV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	proc := windows.NewLazySystemDLL("user32.dll").NewProc("SetProcessDpiAwarenessContext")
	if proc.Find() == nil {
		_, _, _ = proc.Call(perMonitorAwareV2)
	}
}
