//go:build !windows

package rdpview

import "errors"

var errNoRDP = errors.New("RDP im Fenster gibt es nur unter Windows")

// Host is a stub outside Windows.
type Host struct{}

func Start(uintptr, func(Event)) (*Host, error) { return nil, errNoRDP }
func MainWindow() (uintptr, error)              { return 0, errNoRDP }
func (*Host) Open(string, Options, Rect) error  { return errNoRDP }
func (*Host) Move(string, Rect, bool) error     { return errNoRDP }
func (*Host) Fit(string) error                  { return errNoRDP }
func (*Host) Focus(string) error                { return errNoRDP }
func (*Host) Close(string) error                { return errNoRDP }
func (*Host) Shutdown()                         {}
func (*Host) Count() int                        { return 0 }
func (*Host) SetAppKeys(bool)                   {}
