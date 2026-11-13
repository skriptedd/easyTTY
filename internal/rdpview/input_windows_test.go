//go:build windows

package rdpview

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func makeMsg(hwnd uintptr, msg uint32, wp, lp uintptr) *[48]byte {
	var m [48]byte
	*(*uintptr)(unsafe.Pointer(&m[0])) = hwnd
	*(*uint32)(unsafe.Pointer(&m[8])) = msg
	*(*uintptr)(unsafe.Pointer(&m[16])) = wp
	*(*uintptr)(unsafe.Pointer(&m[24])) = lp
	return &m
}

func TestInterceptKeepsAppShortcutsFromRemote(t *testing.T) {
	cls, _ := windows.UTF16PtrFromString("STATIC")
	viewWin, _, _ := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), 0, 0x00CF0000, 0, 0, 100, 100, 0, 0, 0, 0)
	inner, _, _ := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), 0, wsChild, 0, 0, 50, 50, viewWin, 0, 0, 0)
	other, _, _ := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), 0, 0x00CF0000, 0, 0, 100, 100, 0, 0, 0, 0)
	if viewWin == 0 || inner == 0 || other == 0 {
		t.Skip("keine Fenster möglich (kein Desktop)")
	}
	defer procDestroyWindow.Call(other)
	defer procDestroyWindow.Call(viewWin)

	var events []Event
	h := &Host{views: map[string]*view{"tab1": {hwnd: viewWin}}, emit: func(e Event) { events = append(events, e) }}

	if !h.interceptKey(makeMsg(inner, wmSysKeyDown, 'W', altDown)) {
		t.Fatal("Alt+W in the session must stay in easyTTY")
	}
	if len(events) != 1 || events[0] != (Event{ID: "tab1", State: "key", Message: "w"}) {
		t.Fatalf("events: %+v", events)
	}
	if !h.interceptKey(makeMsg(inner, wmSysChar, 'w', altDown)) || !h.interceptKey(makeMsg(inner, wmSysKeyUp, 'W', altDown)) {
		t.Error("char and key-up of Alt+W must not reach the remote PC either")
	}
	if !h.interceptKey(makeMsg(inner, wmSysKeyDown, 'W', altDown|keyRepeat)) || len(events) != 1 {
		t.Errorf("held key must not fire again: %+v", events)
	}
	for name, m := range map[string]*[48]byte{
		"Alt+X (remote menu)":  makeMsg(inner, wmSysKeyDown, 'X', altDown),
		"W without Alt":        makeMsg(inner, wmSysKeyDown, 'W', 0),
		"Alt+W outside a view": makeMsg(other, wmSysKeyDown, 'W', altDown),
		"normal key message":   makeMsg(inner, 0x0100, 'W', 0),
	} {
		if h.interceptKey(m) {
			t.Errorf("%s must go through", name)
		}
	}
	if !h.interceptKey(makeMsg(inner, wmSysKeyDown, 'F', altDown)) || events[len(events)-1] != (Event{ID: "tab1", State: "key", Message: "f"}) {
		t.Errorf("Alt+F must stay in easyTTY: %+v", events)
	}
	if !h.interceptKey(makeMsg(inner, wmSysKeyDown, 0x27, altDown)) || events[len(events)-1].Message != "ArrowRight" {
		t.Errorf("Alt+Right must switch the tile: %+v", events)
	}
	h.SetAppKeys(false)
	if h.interceptKey(makeMsg(inner, wmSysKeyDown, 'W', altDown)) {
		t.Error("with the Alt shortcuts off, Alt+W belongs to the remote PC")
	}
	h.SetAppKeys(true)
	ctrlHeld = func() bool { return true } // AltGr+E (€), AltGr+7 ({) …
	if h.interceptKey(makeMsg(inner, wmSysKeyDown, 'E', altDown)) || h.interceptKey(makeMsg(inner, wmSysKeyDown, '7', altDown)) {
		t.Error("AltGr combos must reach the remote PC")
	}
	ctrlHeld = func() bool { return false }
	if !h.interceptKey(makeMsg(viewWin, wmSysKeyDown, '3', altDown)) || events[len(events)-1].Message != "3" {
		t.Errorf("Alt+3: %+v", events)
	}
}

func TestClipPlanOnlyTouchesTheFocusedSession(t *testing.T) {
	for _, c := range []struct {
		last, focus string
		changed     bool
		pull, push  string
	}{
		{"a", "a", false, "", ""},   // nothing happened
		{"a", "a", true, "", "a"},   // copied locally: only the session in use
		{"", "", true, "", ""},      // copied with no session focused: nobody
		{"a", "", false, "a", ""},   // left a: its clipboard comes home
		{"", "a", false, "", "a"},   // entered a: it gets the local clipboard
		{"a", "b", false, "a", "b"}, // a to b: copy there, paste here
	} {
		pull, push := clipPlan(c.last, c.focus, c.changed)
		if pull != c.pull || push != c.push {
			t.Errorf("%+v: pull %q push %q", c, pull, push)
		}
	}
}
