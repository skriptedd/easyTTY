package main

import (
	"errors"

	"easytty/internal/rdp"
	"easytty/internal/store"
)

// ConnectRDP opens a saved RDP session in Windows' Remote Desktop (mstsc),
// in a window of its own; mstsc asks for the password.
func (a *App) ConnectRDP(id string) error {
	s, err := a.store.Session(id)
	if err != nil {
		return err
	}
	if s.Protocol != store.ProtocolRDP {
		return errors.New("keine RDP-Session")
	}
	if err := rdp.Launch(s); err != nil {
		return err
	}
	_ = a.store.Touch(id)
	return nil
}
