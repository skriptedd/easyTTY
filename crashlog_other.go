//go:build !windows

package main

import "os"

// redirectStderr does nothing here: stderr is visible on these systems.
func redirectStderr(*os.File) error { return nil }
