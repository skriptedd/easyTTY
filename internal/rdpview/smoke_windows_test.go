//go:build windows

package rdpview

import (
	"os"
	"testing"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
)

// Hosts the real control in a hidden window and connects to a port nothing
// listens on. Needs a desktop session, so only with EASYTTY_RDP_SMOKE=1.
func TestSmokeHostControl(t *testing.T) {
	if os.Getenv("EASYTTY_RDP_SMOKE") == "" {
		t.Skip("EASYTTY_RDP_SMOKE=1 setzen")
	}
	// As in the app: the control has to load with DLLs limited to System32.
	if err := windows.SetDefaultDllDirectories(windows.LOAD_LIBRARY_SEARCH_SYSTEM32); err != nil {
		t.Fatal(err)
	}
	cls, _ := windows.UTF16PtrFromString("STATIC")
	parent, _, _ := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), 0, 0x00CF0000 /*WS_OVERLAPPEDWINDOW*/, 0, 0, 800, 600, 0, 0, 0, 0)
	if parent == 0 {
		t.Fatal("no parent window")
	}
	defer procDestroyWindow.Call(parent)

	events := make(chan Event, 8)
	h, err := Start(parent, func(e Event) { events <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer h.Shutdown()
	if err := h.Open("t1", Options{Host: "127.0.0.1", Port: 3389, User: `LAB\test`, Password: "test"}, Rect{0, 0, 640, 480}); err != nil {
		t.Fatal(err)
	}
	if err := h.Move("t1", Rect{10, 10, 700, 500}, true); err != nil {
		t.Fatal(err)
	}
	var seen []string
	deadline := time.After(45 * time.Second)
	for {
		select {
		case e := <-events:
			t.Logf("event %+v", e)
			seen = append(seen, e.State)
			if e.State == "closed" {
				if seen[0] != "connecting" {
					t.Errorf("events: %v", seen)
				}
				return
			}
		case <-deadline:
			t.Fatalf("no disconnect, events: %v", seen)
		}
	}
}

// Reads back what the control really took (clipboard, drives, port).
func TestSmokeSettingsTaken(t *testing.T) {
	if os.Getenv("EASYTTY_RDP_SMOKE") == "" {
		t.Skip("EASYTTY_RDP_SMOKE=1 setzen")
	}
	cls, _ := windows.UTF16PtrFromString("STATIC")
	parent, _, _ := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), 0, 0x00CF0000, 0, 0, 800, 600, 0, 0, 0, 0)
	defer procDestroyWindow.Call(parent)
	h, err := Start(parent, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Shutdown()
	err = h.do(func() error {
		v, err := h.create(Rect{0, 0, 640, 480})
		if err != nil {
			return err
		}
		h.views["s"] = v
		if err := h.configure(v, Options{Host: "127.0.0.1", Port: 3390, Drives: true}, Rect{0, 0, 640, 480}); err != nil {
			return err
		}
		adv := h.advanced(v)
		defer adv.Release()
		for _, n := range []string{"RedirectClipboard", "RedirectDrives", "RDPPort"} {
			res, err := oleutil.GetProperty(adv, n)
			if err != nil {
				t.Errorf("%s: %v", n, err)
				continue
			}
			t.Logf("%s = %v", n, res.Value())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
