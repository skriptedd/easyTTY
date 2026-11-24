//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"

	"easytty/internal/rdpview"
)

var (
	winUser32              = windows.NewLazySystemDLL("user32.dll")
	procGetWindowPlacement = winUser32.NewProc("GetWindowPlacement")
	procSetWindowPlacement = winUser32.NewProc("SetWindowPlacement")
	procGetWindowRect      = winUser32.NewProc("GetWindowRect")
	procMonitorFromRect    = winUser32.NewProc("MonitorFromRect")
	procMonitorFromWindow  = winUser32.NewProc("MonitorFromWindow")
	procGetMonitorInfo     = winUser32.NewProc("GetMonitorInfoW")
	procShowWindow         = winUser32.NewProc("ShowWindow")
	procIsIconic           = winUser32.NewProc("IsIconic")
	procIsZoomed           = winUser32.NewProc("IsZoomed")
	procMoveWindowPos      = winUser32.NewProc("SetWindowPos")
	procSetForeground      = winUser32.NewProc("SetForegroundWindow")
)

const (
	swShowNormal    = 1
	swShowMinimized = 2
	swShowMaximized = 3
	swShow          = 5
	swRestore       = 9

	wpfRestoreToMaximized = 2

	monitorDefaultToNull    = 0
	monitorDefaultToPrimary = 1

	swpNoSize     = 0x1
	swpNoMove     = 0x2
	swpNoZOrder   = 0x4
	swpShowWindow = 0x40
)

var (
	hwndTopmost   = ^uintptr(0) // HWND_TOPMOST (-1)
	hwndNoTopmost = ^uintptr(1) // HWND_NOTOPMOST (-2)
)

type winRect struct{ Left, Top, Right, Bottom int32 }

func (r winRect) rect() rect { return rect{int(r.Left), int(r.Top), int(r.Right), int(r.Bottom)} }

type winPlacement struct {
	Length, Flags, ShowCmd uint32
	MinPos, MaxPos         [2]int32
	Normal                 winRect
}

type monitorInfo struct {
	Size          uint32
	Monitor, Work winRect
	Flags         uint32
}

// captureWindow reads the main window's place (the normal place even while
// it is maximised or minimised).
func captureWindow() (windowState, bool) {
	hwnd, err := rdpview.MainWindow()
	if err != nil {
		return windowState{}, false
	}
	p := winPlacement{Length: uint32(unsafe.Sizeof(winPlacement{}))}
	if ok, _, _ := procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&p))); ok == 0 {
		return windowState{}, false
	}
	n := p.Normal.rect()
	maxed := p.ShowCmd == swShowMaximized || (p.ShowCmd == swShowMinimized && p.Flags&wpfRestoreToMaximized != 0)
	return windowState{X: n.Left, Y: n.Top, W: n.w(), H: n.h(), Max: maxed}, true
}

// placeWindow restores the saved place (if any), then makes sure the window
// is on a screen, not minimised, and in front of the other windows.
func placeWindow(s windowState, saved bool) {
	hwnd, err := rdpview.MainWindow()
	if err != nil {
		return
	}
	lost := false // the saved place lies on no screen any more (monitor gone)
	if saved {
		show := uint32(swShowNormal)
		if s.Max {
			show = swShowMaximized
		}
		normal := winRect{int32(s.X), int32(s.Y), int32(s.X + s.W), int32(s.Y + s.H)}
		mon, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&normal)), monitorDefaultToNull)
		lost = mon == 0
		p := winPlacement{Length: uint32(unsafe.Sizeof(winPlacement{})), ShowCmd: show, Normal: normal}
		procSetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	}
	keepOnScreen(hwnd, lost)
	bringToFront(hwnd)
}

// keepOnScreen fits the window onto its screen; centre puts it in the middle
// (Windows only pulls a lost window to the corner).
func keepOnScreen(hwnd uintptr, centre bool) {
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		procShowWindow.Call(hwnd, swRestore)
	}
	if zoomed, _, _ := procIsZoomed.Call(hwnd); zoomed != 0 {
		return // Windows puts a maximised window on its monitor itself
	}
	var wr winRect
	if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr))); ok == 0 {
		return
	}
	win := wr.rect()
	mon, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&wr)), monitorDefaultToNull)
	offScreen := mon == 0
	if offScreen {
		mon, _, _ = procMonitorFromWindow.Call(hwnd, monitorDefaultToPrimary)
	}
	mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if ok, _, _ := procGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return
	}
	work := mi.Work.rect()
	target, maximise := fitWindow(win, work)
	if offScreen || centre {
		target, maximise = centred(win, work), false
	}
	if maximise {
		procShowWindow.Call(hwnd, swShowMaximized)
		return
	}
	if target != win {
		procMoveWindowPos.Call(hwnd, 0, uintptr(target.Left), uintptr(target.Top), uintptr(target.w()), uintptr(target.h()), swpNoZOrder)
	}
}

// bringToFront shows the window above the others. Windows does not let a
// program take the focus from another one at will (a start by the update
// helper, or while the user clicks elsewhere), so the window is briefly made
// topmost: it then lies on top even when the focus stays where it was.
func bringToFront(hwnd uintptr) {
	procShowWindow.Call(hwnd, swShow)
	procMoveWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpShowWindow)
	procMoveWindowPos.Call(hwnd, hwndNoTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpShowWindow)
	procSetForeground.Call(hwnd)
}
