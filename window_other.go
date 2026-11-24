//go:build !windows

package main

// Window placement is only remembered on Windows.
func captureWindow() (windowState, bool) { return windowState{}, false }
func placeWindow(windowState, bool)      {}
