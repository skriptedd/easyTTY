//go:build !windows

package transfer

func driveMask() uint32 { return 0 }
