//go:build windows

package rdpview

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
)

var (
	user32             = windows.NewLazySystemDLL("user32.dll")
	procCreateWindowEx = user32.NewProc("CreateWindowExW")
	procDestroyWindow  = user32.NewProc("DestroyWindow")
	procSetWindowPos   = user32.NewProc("SetWindowPos")
	procSetFocus       = user32.NewProc("SetFocus")
	procPeekMessage    = user32.NewProc("PeekMessageW")
	procTranslateMsg   = user32.NewProc("TranslateMessage")
	procDispatchMsg    = user32.NewProc("DispatchMessageW")
	procMsgWait        = user32.NewProc("MsgWaitForMultipleObjectsEx")
	procGetClassName   = user32.NewProc("GetClassNameW")
	procIsWindowVis    = user32.NewProc("IsWindowVisible")

	atl                 = windows.NewLazySystemDLL("atl.dll")
	procAtlAxWinInit    = atl.NewProc("AtlAxWinInit")
	ole32               = windows.NewLazySystemDLL("ole32.dll")
	procOleInitialize   = ole32.NewProc("OleInitialize")
	procAtlAxGetControl = atl.NewProc("AtlAxGetControl")
)

// Interfaces of mstscax.dll that are not reachable through IDispatch; IDs and
// vtable slots come from the control's type library (see tools/rdpprobe).
var (
	iidNonScriptable  = ole.NewGUID("{C1E6743A-41C1-4A74-832A-0DD06C1C7A0E}") // IMsTscNonScriptable
	iidNonScriptable3 = ole.NewGUID("{B3378D90-0728-45C7-8ED7-B6159FB92219}") // IMsRdpClientNonScriptable3
	iidNonScriptable5 = ole.NewGUID("{4F6996D5-D7B1-412C-B0FF-063718566907}") // IMsRdpClientNonScriptable5
)

const (
	slotClearTextPassword      = 3  // IMsTscNonScriptable::put_ClearTextPassword
	slotUIParentWindowHandle   = 15 // IMsRdpClientNonScriptable2::put_UIParentWindowHandle
	slotEnableCredSspSupport   = 23 // IMsRdpClientNonScriptable3::put_EnableCredSspSupport
	slotAllowPromptingForCreds = 63 // IMsRdpClientNonScriptable5::put_AllowPromptingForCredentials

	variantTrue = 0xFFFF

	wsChild        = 0x40000000
	wsClipSiblings = 0x04000000
	wsClipChildren = 0x02000000
	swpNoActivate  = 0x0010
	swpShowWindow  = 0x0040
	swpHideWindow  = 0x0080
	pmRemove       = 0x0001
	wmQuit         = 0x0012
	qsAllInput     = 0x04FF
	mwmoInputAvail = 0x0004

	pollEvery = 500 * time.Millisecond
)

// progIDs: the newest control first; "MsTscAx.MsTscAx" follows CurVer.
var progIDs = []string{"MsTscAx.MsTscAx", "MsTscAx.MsTscAx.13", "MsTscAx.MsTscAx.12", "MsTscAx.MsTscAx.11", "MsTscAx.MsTscAx.10"}

type view struct {
	hwnd    uintptr
	unk     *ole.IUnknown
	disp    *ole.IDispatch
	state   string
	visible bool
	w, h    int
	smart   bool // SmartSizing fallback when the session cannot change its resolution
	noClip  bool // clipboard stays local (Options.NoClipboard)
}

// Host owns the UI thread all RDP controls live on (COM single-threaded
// apartment with its own message loop). All calls are safe from any goroutine.
type Host struct {
	parent uintptr
	emit   func(Event)
	cmds   chan func()
	wake   windows.Handle
	views  map[string]*view // only touched on the host thread
	mu     sync.Mutex
	closed bool
	count  atomic.Int32  // open views, readable from any thread (see Count)
	gone   chan struct{} // closed when the host thread has ended

	// host thread only (see input_windows.go)
	swallowed uintptr // virtual key of the shortcut kept from the remote PC
	clipSeq   uintptr // clipboard sequence number last synced
	lastFocus string  // view that had the keyboard at the last poll

	noAppKeys atomic.Bool // the user switched the Alt shortcuts off
}

// Start creates the host thread. parent is the main window (see MainWindow).
func Start(parent uintptr, emit func(Event)) (*Host, error) {
	wake, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return nil, err
	}
	h := &Host{parent: parent, emit: emit, cmds: make(chan func(), 16), wake: wake, views: map[string]*view{}, gone: make(chan struct{})}
	ready := make(chan error, 1)
	go h.thread(ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return h, nil
}

func (h *Host) thread(ready chan<- error) {
	runtime.LockOSThread()
	defer close(h.gone)
	// OleInitialize, not just CoInitializeEx: the control uses the OLE clipboard
	// and drag & drop, which need it on this thread. S_FALSE = already done.
	if hr, _, _ := procOleInitialize.Call(0); int32(hr) < 0 {
		ready <- fmt.Errorf("OLE lässt sich nicht starten (0x%08x)", uint32(hr))
		return
	}
	if r, _, _ := procAtlAxWinInit.Call(); r == 0 {
		ready <- errors.New("ActiveX-Container (atl.dll) nicht verfügbar")
		return
	}
	h.clipSeq = clipboardSeq()
	ready <- nil
	last := time.Now()
	var msg [48]byte // MSG
	for {
		procMsgWait.Call(1, uintptr(unsafe.Pointer(&h.wake)), uintptr(pollEvery/time.Millisecond), qsAllInput, mwmoInputAvail)
		for drained := false; !drained; {
			select {
			case f := <-h.cmds:
				f()
			default:
				drained = true
			}
		}
		for {
			if r, _, _ := procPeekMessage.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0, pmRemove); r == 0 {
				break
			}
			if *(*uint32)(unsafe.Pointer(&msg[8])) == wmQuit {
				return
			}
			if h.interceptKey(&msg) {
				continue
			}
			procTranslateMsg.Call(uintptr(unsafe.Pointer(&msg[0])))
			procDispatchMsg.Call(uintptr(unsafe.Pointer(&msg[0])))
		}
		if time.Since(last) >= pollEvery {
			last = time.Now()
			h.poll()
		}
	}
}

// do runs f on the host thread and waits for it.
func (h *Host) do(f func() error) error {
	h.mu.Lock()
	closed := h.closed
	h.mu.Unlock()
	if closed {
		return errEnded
	}
	// The lock is not held while queueing: a full queue (host thread busy in a
	// login dialog) must not block Shutdown and the closed check with it.
	done := make(chan error, 1)
	select {
	case h.cmds <- func() { done <- f() }:
	case <-h.gone:
		return errEnded
	}
	windows.SetEvent(h.wake)
	select {
	case err := <-done:
		return err
	case <-h.gone:
		return errEnded
	}
}

var errEnded = errors.New("RDP beendet")

// Open creates the view for a tab and connects.
func (h *Host) Open(id string, o Options, r Rect) error {
	return h.do(func() error {
		h.destroy(id)
		v, err := h.create(r)
		if err != nil {
			return err
		}
		v.noClip = o.NoClipboard
		h.views[id] = v
		h.count.Store(int32(len(h.views)))
		if err := h.configure(v, o, r); err != nil {
			h.destroy(id)
			return err
		}
		if _, err := oleutil.CallMethod(v.disp, "Connect"); err != nil {
			h.destroy(id)
			return fmt.Errorf("RDP verbinden: %w", err)
		}
		v.state = "connecting"
		h.emit(Event{ID: id, State: "connecting", Message: o.Host})
		return nil
	})
}

func (h *Host) create(r Rect) (*view, error) {
	cls, _ := windows.UTF16PtrFromString("AtlAxWin")
	for _, pid := range progIDs {
		name, _ := windows.UTF16PtrFromString(pid)
		hwnd, _, _ := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(name)),
			wsChild|wsClipSiblings|wsClipChildren, uintptr(r.X), uintptr(r.Y), uintptr(r.W), uintptr(r.H), h.parent, 0, 0, 0)
		if hwnd == 0 {
			continue
		}
		var unk *ole.IUnknown
		if hr, _, _ := procAtlAxGetControl.Call(hwnd, uintptr(unsafe.Pointer(&unk))); hr != 0 || unk == nil {
			procDestroyWindow.Call(hwnd)
			continue
		}
		disp, err := unk.QueryInterface(ole.IID_IDispatch)
		if err != nil {
			unk.Release()
			procDestroyWindow.Call(hwnd)
			continue
		}
		return &view{hwnd: hwnd, unk: unk, disp: disp, w: r.W, h: r.H}, nil
	}
	return nil, errors.New("Remotedesktop-Steuerelement (mstscax.dll) nicht verfügbar")
}

func (h *Host) configure(v *view, o Options, r Rect) error {
	domain, user := splitUser(o.User)
	w, hh := desktopSize(r.W, r.H)
	for _, p := range []struct {
		name string
		val  any
	}{{"Server", o.Host}, {"UserName", user}, {"DesktopWidth", w}, {"DesktopHeight", hh}, {"ColorDepth", 32}} {
		if _, err := oleutil.PutProperty(v.disp, p.name, p.val); err != nil {
			return fmt.Errorf("RDP-Einstellung %s: %w", p.name, err)
		}
	}
	if domain != "" {
		_, _ = oleutil.PutProperty(v.disp, "Domain", domain)
	}
	adv := h.advanced(v)
	if adv == nil {
		return errors.New("RDP-Einstellungen nicht erreichbar")
	}
	defer adv.Release()
	port := o.Port
	if port == 0 {
		port = 3389
	}
	if _, err := oleutil.PutProperty(adv, "RDPPort", port); err != nil {
		return fmt.Errorf("RDP-Port: %w", err)
	}
	// Optional comforts: older controls may lack some, that is fine.
	for _, p := range []struct {
		name string
		val  any
	}{
		{"EnableAutoReconnect", true}, {"RedirectDrives", o.Drives}, {"RedirectClipboard", !o.NoClipboard},
		{"SmartSizing", false}, {"AuthenticationLevel", 2}, {"BitmapPersistence", 1},
		{"GrabFocusOnConnect", false}, // the UI focuses the tab you look at; a finished login must not steal keys
	} {
		_, _ = oleutil.PutProperty(adv, p.name, p.val)
	}
	// Windows' credential dialogs belong to the easyTTY window.
	_ = vcallIface(v.unk, iidNonScriptable3, slotUIParentWindowHandle, h.parent)
	// Network Level Authentication (needed by almost every current server).
	if err := vcallIface(v.unk, iidNonScriptable3, slotEnableCredSspSupport, variantTrue); err != nil {
		return fmt.Errorf("RDP (NLA): %w", err)
	}
	_ = vcallIface(v.unk, iidNonScriptable5, slotAllowPromptingForCreds, variantTrue)
	if o.Password != "" {
		b := ole.SysAllocString(o.Password)
		err := vcallIface(v.unk, iidNonScriptable, slotClearTextPassword, uintptr(unsafe.Pointer(b)))
		ole.SysFreeString(b)
		if err != nil {
			return fmt.Errorf("RDP-Passwort übergeben: %w", err)
		}
	}
	return nil
}

// advanced returns the newest AdvancedSettings interface the control offers.
func (h *Host) advanced(v *view) *ole.IDispatch {
	for _, n := range []string{"AdvancedSettings9", "AdvancedSettings8", "AdvancedSettings7", "AdvancedSettings2"} {
		if res, err := oleutil.GetProperty(v.disp, n); err == nil && res != nil {
			if d := res.ToIDispatch(); d != nil {
				return d
			}
		}
	}
	return nil
}

// Move places a view (and shows or hides it).
func (h *Host) Move(id string, r Rect, visible bool) error {
	return h.do(func() error {
		v := h.views[id]
		if v == nil {
			return nil
		}
		flags := uintptr(swpNoActivate | swpHideWindow)
		if visible && r.W > 0 && r.H > 0 {
			flags = swpNoActivate | swpShowWindow
		}
		procSetWindowPos.Call(v.hwnd, 0 /*HWND_TOP*/, uintptr(r.X), uintptr(r.Y), uintptr(max(r.W, 1)), uintptr(max(r.H, 1)), flags)
		v.visible = visible
		v.w, v.h = r.W, r.H
		return nil
	})
}

// Fit asks the remote session to take the view's current size (call after
// resizing settles). Servers that cannot do that get a scaled picture.
func (h *Host) Fit(id string) error {
	return h.do(func() error {
		v := h.views[id]
		if v == nil || v.state != "connected" || v.w <= 0 || v.h <= 0 {
			return nil
		}
		w, hh := desktopSize(v.w, v.h)
		if _, err := oleutil.CallMethod(v.disp, "UpdateSessionDisplaySettings", w, hh, w, hh, 0, 100, 100); err != nil && !v.smart {
			v.smart = true
			if adv := h.advanced(v); adv != nil {
				_, _ = oleutil.PutProperty(adv, "SmartSizing", true)
				adv.Release()
			}
		}
		return nil
	})
}

// Focus gives the keyboard to a view.
func (h *Host) Focus(id string) error {
	return h.do(func() error {
		if v := h.views[id]; v != nil && v.visible {
			procSetFocus.Call(v.hwnd)
		}
		return nil
	})
}

// Close disconnects and removes a view.
func (h *Host) Close(id string) error {
	return h.do(func() error { h.destroy(id); return nil })
}

// Shutdown closes every view and ends the host thread.
// Count returns the number of open views. It never waits for the host
// thread: the window's close handler calls it on the UI thread, which the
// host thread may itself be waiting for.
func (h *Host) Count() int { return int(h.count.Load()) }

func (h *Host) Shutdown() {
	_ = h.do(func() error {
		for id := range h.views {
			h.destroy(id)
		}
		return nil
	})
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
}

func (h *Host) destroy(id string) {
	v := h.views[id]
	if v == nil {
		return
	}
	delete(h.views, id)
	h.count.Store(int32(len(h.views)))
	if v.state == "connected" || v.state == "connecting" {
		_, _ = oleutil.CallMethod(v.disp, "Disconnect")
	}
	v.disp.Release()
	v.unk.Release()
	procDestroyWindow.Call(v.hwnd)
}

// poll turns the control's Connected property into events
// (0 = not connected, 1 = connected, 2 = connecting).
func (h *Host) poll() {
	h.syncClipboards()
	for id, v := range h.views {
		res, err := oleutil.GetProperty(v.disp, "Connected")
		if err != nil {
			continue
		}
		state := map[int64]string{0: "closed", 1: "connected", 2: "connecting"}[toInt(res.Value())]
		if state == "" || state == v.state {
			continue
		}
		prev := v.state
		v.state = state
		msg := ""
		if state == "closed" {
			msg = h.reason(v, prev)
		}
		h.emit(Event{ID: id, State: state, Message: msg})
	}
}

// reason explains a disconnect as far as the control tells.
func (h *Host) reason(v *view, prev string) string {
	if prev == "connecting" {
		return "Verbindung fehlgeschlagen (Server nicht erreichbar, Anmeldung abgelehnt oder abgebrochen)"
	}
	if res, err := oleutil.GetProperty(v.disp, "ExtendedDisconnectReason"); err == nil {
		switch toInt(res.Value()) {
		case 1, 2: // by the remote side or by an administrator
			return "Vom Server getrennt"
		case 3, 4: // idle / logon timeout
			return "Wegen Inaktivität getrennt"
		case 12:
			return "Abgemeldet"
		}
	}
	return "Verbindung getrennt"
}

func toInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case int16:
		return int64(n)
	case int:
		return int64(n)
	case uint8:
		return int64(n)
	case int8:
		return int64(n)
	}
	return -1
}

// vcallIface queries an interface and calls one vtable slot with a single argument.
func vcallIface(unk *ole.IUnknown, iid *ole.GUID, slot int, arg uintptr) error {
	var obj unsafe.Pointer
	if r := vcall(unsafe.Pointer(unk), 0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&obj))); r != 0 {
		return ole.NewError(r)
	}
	defer vcall(obj, 2) // Release
	if r := vcall(obj, slot, arg); r != 0 {
		return ole.NewError(r)
	}
	return nil
}

// vcall calls slot n of a COM object's vtable.
//
// Callers pass uintptr(unsafe.Pointer(&local)); uintptrescapes moves those
// locals to the heap so a stack move cannot leave COM writing to a stale copy.
//
//go:uintptrescapes
func vcall(obj unsafe.Pointer, slot int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, slot*int(unsafe.Sizeof(uintptr(0)))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

// MainWindow finds this process's Wails window.
func MainWindow() (uintptr, error) {
	pid := uint32(os.Getpid())
	var found uintptr
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var wpid uint32
		windows.GetWindowThreadProcessId(windows.HWND(hwnd), &wpid)
		if wpid != pid {
			return 1
		}
		buf := make([]uint16, 64)
		procGetClassName.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if windows.UTF16ToString(buf) == "wailsWindow" {
			found = hwnd
			return 0
		}
		if vis, _, _ := procIsWindowVis.Call(hwnd); vis != 0 && found == 0 {
			found = hwnd // fallback: first visible window of this process
		}
		return 1
	})
	_ = windows.EnumWindows(cb, nil)
	if found == 0 {
		return 0, errors.New("Hauptfenster nicht gefunden")
	}
	return found, nil
}
