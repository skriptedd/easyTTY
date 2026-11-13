// Package rdpview shows RDP sessions inside the easyTTY window. It hosts
// Windows' own Remote Desktop ActiveX control (mstscax.dll, the engine behind
// mstsc) in a child window that the UI places over a tab – like SecureCRT.
package rdpview

import "strings"

// Rect is a position in the main window's client area, in physical pixels.
type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Options are the connection settings of one view.
type Options struct {
	Host     string
	Port     int
	User     string // "DOMAIN\user", "user@domain" or "user"
	Password string // "" = Windows asks
	Drives   bool   // local drives visible remotely
	// NoClipboard keeps the clipboard local: neither the control nor easyTTY's
	// own sync copies anything across.
	NoClipboard bool
}

// Event reports a state change of a view.
type Event struct {
	ID      string `json:"id"`
	State   string `json:"state"` // connecting, connected, closed, error – or "key": an easyTTY shortcut (Message "w" …) typed in the session
	Message string `json:"message"`
}

// splitUser separates "DOMAIN\user" into domain and user; other forms stay whole.
func splitUser(u string) (domain, user string) {
	if i := strings.Index(u, `\`); i > 0 {
		return u[:i], u[i+1:]
	}
	return "", u
}

// desktopSize clamps a view size to what an RDP session accepts
// (width a multiple of 4, height even, 200…8192).
func desktopSize(w, h int) (int, int) {
	clamp := func(v int) int { return min(max(v, 200), 8192) }
	return clamp(w) &^ 3, clamp(h) &^ 1
}
