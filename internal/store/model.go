package store

import (
	"errors"
	"fmt"
	"strings"
)

// Protocols supported by a session.
const (
	ProtocolSSH    = "ssh"
	ProtocolSerial = "serial"
	ProtocolRDP    = "rdp" // opens Windows' Remote Desktop (mstsc), not a terminal tab
)

// Authentication modes for SSH sessions.
const (
	AuthAuto     = "auto"     // agent, default keys, then password
	AuthPassword = "password" // password / keyboard-interactive only
	AuthKey      = "key"      // explicit private key file
	AuthAgent    = "agent"    // ssh-agent only (plus password fallback)
)

const (
	defaultSSHPort  = 22
	defaultBaud     = 9600
	defaultDataBits = 8
	defaultRDPPort  = 3389
	maxPort         = 65535
)

// Session is one saved connection profile.
type Session struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	FolderID    string `json:"folderId"`
	Protocol    string `json:"protocol"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	Auth        string `json:"auth"`
	KeyPath     string `json:"keyPath"`
	HasPassword bool   `json:"hasPassword"`
	SerialPort  string `json:"serialPort"`
	Baud        int    `json:"baud"`
	DataBits    int    `json:"dataBits"`
	Parity      string `json:"parity"`
	StopBits    string `json:"stopBits"`
	KeepAlive   int    `json:"keepAlive"`
	// JumpID is the saved SSH session to connect through ("" = direct).
	JumpID string `json:"jumpId,omitempty"`
	// Forwards are local ports tunnelled through the connection (SSH only).
	Forwards  []Forward `json:"forwards,omitempty"`
	RdpDrives bool      `json:"rdpDrives"` // local drives visible on the remote PC
	// RdpNoClipboard keeps the clipboard local (no copy and paste with the remote PC).
	RdpNoClipboard bool   `json:"rdpNoClipboard"`
	LogPath        string `json:"logPath"`
	Color          string `json:"color"`
	Notes          string `json:"notes"`
	LastUsed       int64  `json:"lastUsed"`
}

// Forward makes 127.0.0.1:Listen on this PC reach Host:Port as seen from
// the SSH server (ssh -L).
type Forward struct {
	Listen int    `json:"listen"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
}

// MaxForwards limits the tunnels of one session.
const MaxForwards = 20

// Folder groups sessions and other folders. An empty ParentID means root.
type Folder struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	ParentID string  `json:"parentId"`
	Layout   *Layout `json:"layout,omitempty"`
}

// Layout is a window arrangement opened with one click: which session goes
// into which tile of a template. Empty slot = tile stays free.
type Layout struct {
	Template string   `json:"template"`
	Slots    []string `json:"slots"`
}

// LayoutTemplates maps template names to their number of tiles.
var LayoutTemplates = map[string]int{
	"1":  1, // one window
	"2v": 2, // side by side
	"2h": 2, // stacked
	"3m": 3, // master left, two stacked right (Hyprland "master")
	"4":  4, // 2×2
	"6":  6, // 3×2
}

// Tree is the UI-facing view of all folders and sessions (never contains secrets).
type Tree struct {
	Folders  []Folder  `json:"folders"`
	Sessions []Session `json:"sessions"`
}

// Normalize returns a copy of s with defaults applied and whitespace trimmed.
func (s Session) Normalize() Session {
	out := s
	out.KeepAlive = min(out.KeepAlive, maxKeepAlive)
	out.Name = strings.TrimSpace(out.Name)
	out.Host = strings.TrimSpace(out.Host)
	out.User = strings.TrimSpace(out.User)
	out.KeyPath = strings.TrimSpace(out.KeyPath)
	out.SerialPort = strings.TrimSpace(out.SerialPort)
	out.JumpID = strings.TrimSpace(out.JumpID)
	out.Forwards = normalizeForwards(out.Forwards)
	if out.Protocol == "" {
		out.Protocol = ProtocolSSH
	}
	switch out.Protocol {
	case ProtocolSSH:
		if out.Port == 0 {
			out.Port = defaultSSHPort
		}
		if out.Auth == "" {
			out.Auth = AuthAuto
		}
		if out.Name == "" {
			out.Name = out.Host
		}
		// serial settings left over from the editor mean nothing for SSH
		out.SerialPort, out.Baud, out.DataBits, out.Parity, out.StopBits = "", 0, 0, "", ""
		out.RdpDrives, out.RdpNoClipboard = false, false
	case ProtocolRDP:
		out.JumpID, out.Forwards = "", nil
		if out.Port == 0 {
			out.Port = defaultRDPPort
		}
		if out.Name == "" {
			out.Name = out.Host
		}
		out.Auth, out.KeyPath, out.KeepAlive = "", "", 0
		out.SerialPort, out.Baud, out.DataBits, out.Parity, out.StopBits = "", 0, 0, "", ""
	case ProtocolSerial:
		if out.Baud == 0 {
			out.Baud = defaultBaud
		}
		if out.DataBits == 0 {
			out.DataBits = defaultDataBits
		}
		if out.Parity == "" {
			out.Parity = "none"
		}
		if out.StopBits == "" {
			out.StopBits = "1"
		}
		if out.Name == "" {
			out.Name = out.SerialPort
		}
		// User (+ stored password) is the console login; SSH-only fields go.
		out.JumpID, out.Forwards = "", nil
		out.Host, out.Port, out.Auth, out.KeyPath, out.KeepAlive = "", 0, "", "", 0
		out.RdpDrives, out.RdpNoClipboard = false, false
	}
	return out
}

// Validate checks a normalized session for obvious mistakes.
func (s Session) Validate() error {
	switch s.Protocol {
	case ProtocolSSH:
		if s.Host == "" {
			return errors.New("Host fehlt")
		}
		if strings.ContainsAny(s.Host, " \t") {
			return errors.New("Host darf keine Leerzeichen enthalten")
		}
		if s.Port < 1 || s.Port > maxPort {
			return errors.New("Port muss zwischen 1 und 65535 liegen")
		}
		if s.JumpID != "" && s.JumpID == s.ID {
			return errors.New("Eine Session kann nicht ihr eigener Sprung-Host sein")
		}
		if err := validateForwards(s.Forwards); err != nil {
			return err
		}
		switch s.Auth {
		case AuthAuto, AuthPassword, AuthAgent:
		case AuthKey:
			if s.KeyPath == "" {
				return errors.New("Schlüsseldatei fehlt")
			}
		default:
			return errors.New("unbekannte Anmeldeart: " + s.Auth)
		}
	case ProtocolRDP:
		if s.Host == "" {
			return errors.New("Host fehlt")
		}
		if strings.ContainsAny(s.Host, " \t;\r\n") {
			return errors.New("Host darf keine Leerzeichen enthalten")
		}
		if s.Port < 1 || s.Port > maxPort {
			return errors.New("Port muss zwischen 1 und 65535 liegen")
		}
		if strings.ContainsAny(s.User, "\r\n") {
			return errors.New("Benutzer ungültig")
		}
	case ProtocolSerial:
		if s.SerialPort == "" {
			return errors.New("COM-Port fehlt")
		}
		if s.Baud <= 0 {
			return errors.New("Baudrate ungültig")
		}
		if s.DataBits < 5 || s.DataBits > 8 {
			return errors.New("Datenbits müssen 5–8 sein")
		}
		switch s.Parity {
		case "none", "odd", "even", "mark", "space":
		default:
			return errors.New("Parität ungültig")
		}
		switch s.StopBits {
		case "1", "1.5", "2":
		default:
			return errors.New("Stoppbits ungültig")
		}
	default:
		return errors.New("unbekanntes Protokoll: " + s.Protocol)
	}
	if s.Name == "" {
		return errors.New("Name fehlt")
	}
	if s.KeepAlive < 0 {
		return errors.New("Keepalive darf nicht negativ sein")
	}
	return nil
}

// maxKeepAlive caps the interval (seconds); an imported file could carry anything.
const maxKeepAlive = 86400

// normalizeForwards trims hosts and returns a new slice (nil when empty), so
// a session never shares its forwards with the copy it came from.
func normalizeForwards(in []Forward) []Forward {
	if len(in) == 0 {
		return nil
	}
	out := make([]Forward, len(in))
	for i, f := range in {
		out[i] = Forward{Listen: f.Listen, Host: strings.TrimSpace(f.Host), Port: f.Port}
	}
	return out
}

func validateForwards(fs []Forward) error {
	if len(fs) > MaxForwards {
		return fmt.Errorf("Höchstens %d Port-Weiterleitungen", MaxForwards)
	}
	used := map[int]bool{}
	for _, f := range fs {
		if f.Listen < 1 || f.Listen > maxPort || f.Port < 1 || f.Port > maxPort {
			return fmt.Errorf("Weiterleitung %d → %s:%d: Ports müssen zwischen 1 und 65535 liegen", f.Listen, f.Host, f.Port)
		}
		if f.Host == "" || strings.ContainsAny(f.Host, " \t\r\n/") {
			return fmt.Errorf("Weiterleitung %d: Zielhost ungültig", f.Listen)
		}
		if used[f.Listen] {
			return fmt.Errorf("Lokaler Port %d ist doppelt vergeben", f.Listen)
		}
		used[f.Listen] = true
	}
	return nil
}
