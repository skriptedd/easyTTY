//go:build windows

package main

import "golang.org/x/sys/windows"

// DLLs load from System32 only: a DLL placed next to the exe (say in the
// Downloads folder) must not be picked up. WebView2 and the Remote Desktop
// control load by full path and are not affected.
func init() {
	_ = windows.SetDefaultDllDirectories(windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
}
