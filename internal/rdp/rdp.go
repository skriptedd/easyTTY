// Package rdp opens RDP sessions in Windows' own Remote Desktop client
// (mstsc.exe) in a window of its own, through a temporary .rdp file without a
// password (mstsc asks). RDP inside an easyTTY tab is internal/rdpview.
package rdp

import (
	"fmt"
	"strings"

	"easytty/internal/store"
)

// File returns the content of an .rdp file for a session.
func File(s store.Session) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\r\n", args...) }
	addr := s.Host
	if strings.Contains(addr, ":") && !strings.HasPrefix(addr, "[") { // IPv6
		addr = "[" + addr + "]"
	}
	if s.Port != 0 && s.Port != 3389 {
		addr = fmt.Sprintf("%s:%d", addr, s.Port)
	}
	line("full address:s:%s", addr)
	if s.User != "" {
		line("username:s:%s", s.User)
	}
	line("screen mode id:i:2") // full screen: the windowed case is the easyTTY tab
	if s.RdpNoClipboard {
		line("redirectclipboard:i:0")
	} else {
		line("redirectclipboard:i:1")
	}
	if s.RdpDrives {
		line("drivestoredirect:s:*")
	}
	line("autoreconnection enabled:i:1")
	return b.String()
}
