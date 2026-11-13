package main

import (
	"errors"

	"easytty/internal/rdpview"
	"easytty/internal/store"
)

// rdpHost starts the RDP host thread on first use.
func (a *App) rdpHost() (*rdpview.Host, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.rdp != nil {
		return a.rdp, nil
	}
	parent, err := rdpview.MainWindow()
	if err != nil {
		return nil, err
	}
	h, err := rdpview.Start(parent, func(e rdpview.Event) {
		if e.State == "key" {
			a.emit("rdp:key", e)
			return
		}
		a.emit("rdp:state", e)
	})
	if err != nil {
		return nil, err
	}
	h.SetAppKeys(a.GetSettings().AltShortcuts)
	a.rdp = h
	return h, nil
}

// RDPOpen connects a saved RDP session inside the tab area r (physical pixels).
func (a *App) RDPOpen(tabID, sessionID string, r rdpview.Rect) error {
	s, err := a.store.Session(sessionID)
	if err != nil {
		return err
	}
	if s.Protocol != store.ProtocolRDP {
		return errors.New("keine RDP-Session")
	}
	h, err := a.rdpHost()
	if err != nil {
		return err
	}
	pw, _ := a.store.Password(s.ID)
	if err := h.Open(tabID, rdpview.Options{Host: s.Host, Port: s.Port, User: s.User, Password: pw, Drives: s.RdpDrives, NoClipboard: s.RdpNoClipboard}, r); err != nil {
		return err
	}
	_ = a.store.Touch(s.ID)
	return nil
}

// runningRDP returns the RDP host if it was started (nil otherwise).
func (a *App) runningRDP() *rdpview.Host {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rdp
}

// RDPMove places (or hides) the RDP view of a tab.
func (a *App) RDPMove(tabID string, r rdpview.Rect, visible bool) error {
	if h := a.runningRDP(); h != nil {
		return h.Move(tabID, r, visible)
	}
	return nil
}

// RDPFit lets the remote desktop take the tab's size after a resize.
func (a *App) RDPFit(tabID string) error {
	if h := a.runningRDP(); h != nil {
		return h.Fit(tabID)
	}
	return nil
}

// RDPFocus gives the keyboard to the RDP view of a tab.
func (a *App) RDPFocus(tabID string) error {
	if h := a.runningRDP(); h != nil {
		return h.Focus(tabID)
	}
	return nil
}

// RDPClose disconnects and removes the RDP view of a tab.
func (a *App) RDPClose(tabID string) error {
	if h := a.runningRDP(); h != nil {
		return h.Close(tabID)
	}
	return nil
}
