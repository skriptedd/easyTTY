//go:build windows

package rdpview

import (
	"unsafe"

	"github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

// While a remote desktop has the keyboard, every key goes to the remote PC.
// The host thread runs the message loop itself, so it can keep a few easyTTY
// shortcuts (Alt+W closes the tab …) and keep the clipboards in step.

var (
	procIsChild     = user32.NewProc("IsChild")
	procClipboardSN = user32.NewProc("GetClipboardSequenceNumber")
	procGetKeyState = user32.NewProc("GetKeyState")

	iidNonScriptable7 = ole.NewGUID("{71B4A60A-FE21-46D8-A39B-8E32BA0C5ECC}") // IMsRdpClientNonScriptable7
)

const (
	wmSysKeyDown = 0x0104
	wmSysKeyUp   = 0x0105
	wmSysChar    = 0x0106
	altDown      = 1 << 29 // lParam: Alt held
	keyRepeat    = 1 << 30 // lParam: key was already down

	slotGetClipboard     = 69 // IMsRdpClientNonScriptable7::get_Clipboard
	slotCanLocalToRemote = 3  // IMsRdpClipboard::CanSyncLocalClipboardToRemoteSession
	slotLocalToRemote    = 4  // IMsRdpClipboard::SyncLocalClipboardToRemoteSession
	slotCanRemoteToLocal = 5  // IMsRdpClipboard::CanSyncRemoteClipboardToLocalSession
	slotRemoteToLocal    = 6  // IMsRdpClipboard::SyncRemoteClipboardToLocalSession
)

// appKeys: Alt+key combos (virtual-key codes) easyTTY keeps for itself.
// Others – Alt+X for a remote menu, Alt+F4 … – stay with the remote PC.
var appKeys = func() map[uintptr]string {
	m := map[uintptr]string{'W': "w", 'E': "e", 'F': "f", 'S': "s", 'K': "k", 'T': "t",
		0x25: "ArrowLeft", 0x26: "ArrowUp", 0x27: "ArrowRight", 0x28: "ArrowDown"}
	for c := '0'; c <= '9'; c++ {
		m[uintptr(c)] = string(c)
	}
	return m
}()

// interceptKey reports whether msg (a MSG) is an easyTTY shortcut typed into
// a view. It is then reported as Event{State: "key"} and not dispatched.
func (h *Host) interceptKey(msg *[48]byte) bool {
	if h.noAppKeys.Load() {
		return false // every Alt combination goes to the remote PC
	}
	m := *(*uint32)(unsafe.Pointer(&msg[8]))
	if m != wmSysKeyDown && m != wmSysKeyUp && m != wmSysChar {
		return false
	}
	wp := *(*uintptr)(unsafe.Pointer(&msg[16]))
	lp := *(*uintptr)(unsafe.Pointer(&msg[24]))
	if lp&altDown == 0 || ctrlHeld() { // Ctrl+Alt is AltGr: € { [ ] } belong to the remote PC
		return false
	}
	if m == wmSysChar { // the character of a key swallowed just before ('w' or 'W')
		return h.swallowed != 0 && (wp == h.swallowed || wp == h.swallowed+32)
	}
	key, ok := appKeys[wp]
	if !ok {
		return false
	}
	id := h.viewOf(*(*uintptr)(unsafe.Pointer(&msg[0])))
	if id == "" {
		return false
	}
	if m == wmSysKeyDown {
		h.swallowed = wp
		if lp&keyRepeat == 0 { // holding Alt+W must not close tab after tab
			h.emit(Event{ID: id, State: "key", Message: key})
		}
	}
	return true
}

// SetAppKeys follows the "Alt shortcuts" setting.
func (h *Host) SetAppKeys(on bool) { h.noAppKeys.Store(!on) }

// viewOf returns the view a window belongs to ("" if none).
func (h *Host) viewOf(hwnd uintptr) string {
	for id, v := range h.views {
		if hwnd == v.hwnd {
			return id
		}
		if r, _, _ := procIsChild.Call(v.hwnd, hwnd); r != 0 {
			return id
		}
	}
	return ""
}

// focusedView returns the view that has the keyboard right now ("" if none).
func (h *Host) focusedView() string {
	info := windows.GUIThreadInfo{Size: uint32(unsafe.Sizeof(windows.GUIThreadInfo{}))}
	if windows.GetGUIThreadInfo(0, &info) != nil || info.Focus == 0 {
		return ""
	}
	return h.viewOf(uintptr(info.Focus))
}

func clipboardSeq() uintptr {
	n, _, _ := procClipboardSN.Call()
	return n
}

// clipPlan decides which view gives its clipboard back (pull) and which gets
// the local one (push). Only the session you work in gets it, so a password
// copied locally never lands on every open remote PC.
func clipPlan(lastFocus, focus string, clipChanged bool) (pull, push string) {
	if lastFocus != focus {
		pull = lastFocus
	}
	if clipChanged || lastFocus != focus {
		push = focus
	}
	return pull, push
}

// syncClipboards keeps both sides in step (called from poll): copy remotely,
// paste locally, and the other way round.
func (h *Host) syncClipboards() {
	seq := clipboardSeq()
	focused := h.focusedView()
	pull, push := clipPlan(h.lastFocus, focused, seq != h.clipSeq)
	h.clipSeq = seq
	h.lastFocus = focused
	if v := h.views[pull]; v != nil && v.state == "connected" && !v.noClip {
		syncClipboard(v, false)
		h.clipSeq = clipboardSeq() // our own change must not echo back
	}
	if v := h.views[push]; v != nil && v.state == "connected" && !v.noClip {
		syncClipboard(v, true)
	}
}

// syncClipboard copies the clipboard of a view to the PC (toRemote=false) or
// the other way, if the control has something to sync.
func syncClipboard(v *view, toRemote bool) {
	var nsc unsafe.Pointer
	if r := vcall(unsafe.Pointer(v.unk), 0, uintptr(unsafe.Pointer(iidNonScriptable7)), uintptr(unsafe.Pointer(&nsc))); r != 0 || nsc == nil {
		return // older control: it syncs on its own
	}
	defer vcall(nsc, 2)
	var clip unsafe.Pointer
	if r := vcall(nsc, slotGetClipboard, uintptr(unsafe.Pointer(&clip))); r != 0 || clip == nil {
		return
	}
	defer vcall(clip, 2)
	can, sync := slotCanRemoteToLocal, slotRemoteToLocal
	if toRemote {
		can, sync = slotCanLocalToRemote, slotLocalToRemote
	}
	var ok int16 // VARIANT_BOOL
	if r := vcall(clip, can, uintptr(unsafe.Pointer(&ok))); r != 0 || ok == 0 {
		return
	}
	vcall(clip, sync)
}

// ctrlHeld reports whether Ctrl is down (Ctrl+Alt = AltGr on German keyboards).
var ctrlHeld = func() bool {
	r, _, _ := procGetKeyState.Call(0x11) // VK_CONTROL
	return r&0x8000 != 0
}
