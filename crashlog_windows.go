//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// redirectStderr points the process's stderr at f. The runtime looks the handle
// up for every write, and only this also catches exceptions in foreign code:
// debug.SetCrashOutput misses those.
func redirectStderr(f *os.File) error {
	return windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(f.Fd()))
}
