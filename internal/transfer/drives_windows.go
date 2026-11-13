//go:build windows

package transfer

import "golang.org/x/sys/windows"

// driveMask has bit 0 set for A:, bit 1 for B: and so on.
func driveMask() uint32 {
	m, err := windows.GetLogicalDrives()
	if err != nil {
		return 0
	}
	return m
}
