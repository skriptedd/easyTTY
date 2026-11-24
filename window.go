package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// The window comes back where it was: maximised, or as a window at its last
// place and size. A place that is no longer on any screen (monitor
// unplugged, resolution changed) or a window larger than the screen is
// never restored as is: the window is centred or maximised instead, so it
// can always be seen.

const windowFile = "window.json"

// windowState is the last place of the main window, in screen pixels.
type windowState struct {
	X   int  `json:"x"`
	Y   int  `json:"y"`
	W   int  `json:"w"`
	H   int  `json:"h"`
	Max bool `json:"max"`
}

// rect is a screen rectangle (right and bottom exclusive).
type rect struct{ Left, Top, Right, Bottom int }

func (r rect) w() int { return r.Right - r.Left }
func (r rect) h() int { return r.Bottom - r.Top }

// minWindow is the smallest believable window; anything smaller is a broken file.
const minWindow = 200

func (s windowState) valid() bool { return s.W >= minWindow && s.H >= minWindow }

func loadWindowState(dir string) (windowState, bool) {
	b, err := os.ReadFile(filepath.Join(dir, windowFile))
	if err != nil {
		return windowState{}, false
	}
	var s windowState
	if json.Unmarshal(b, &s) != nil || !s.valid() {
		return windowState{}, false
	}
	return s, true
}

func saveWindowState(dir string, s windowState) error {
	if !s.valid() {
		return nil // a window being destroyed reports nonsense; keep the last good one
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, windowFile), b, 0o600)
}

// fitWindow places win on a screen with work area work: unchanged when it
// fits, otherwise shrunk and pushed inside so the title bar can be reached.
// maximise reports a window at least as big as the screen both ways; it is
// better shown maximised.
func fitWindow(win, work rect) (out rect, maximise bool) {
	if win.w() >= work.w() && win.h() >= work.h() {
		return win, true
	}
	if win.Left >= work.Left && win.Top >= work.Top && win.Right <= work.Right && win.Bottom <= work.Bottom {
		return win, false
	}
	w, h := min(win.w(), work.w()), min(win.h(), work.h())
	left := min(max(win.Left, work.Left), work.Right-w)
	top := min(max(win.Top, work.Top), work.Bottom-h)
	return rect{left, top, left + w, top + h}, false
}

// centred is win's size in the middle of work (shrunk to fit).
func centred(win, work rect) rect {
	w, h := min(win.w(), work.w()), min(win.h(), work.h())
	left := work.Left + (work.w()-w)/2
	top := work.Top + (work.h()-h)/2
	return rect{left, top, left + w, top + h}
}
