// Package putty converts saved PuTTY sessions into easyTTY sessions.
package putty

import (
	"net/url"
	"strings"

	"easytty/internal/store"
)

// Values are the registry values of one PuTTY session (string or uint64).
type Values map[string]any

func (v Values) str(key string) string {
	s, _ := v[key].(string)
	return strings.TrimSpace(s)
}

func (v Values) num(key string) int {
	n, _ := v[key].(uint64)
	return int(n)
}

var parity = map[int]string{0: "none", 1: "odd", 2: "even", 3: "mark", 4: "space"}

var stopBits = map[int]string{2: "1", 3: "1.5", 4: "2"}

// DecodeName undoes PuTTY's %XX escaping of session names.
func DecodeName(raw string) string {
	if n, err := url.PathUnescape(raw); err == nil {
		return n
	}
	return raw
}

// Convert maps one PuTTY session; ok=false for unsupported ones (telnet, raw, defaults).
func Convert(rawName string, v Values) (store.Session, bool) {
	name := DecodeName(rawName)
	if name == "Default Settings" {
		return store.Session{}, false
	}
	s := store.Session{Name: name}
	switch v.str("Protocol") {
	case "ssh", "":
		host := v.str("HostName")
		if at := strings.LastIndex(host, "@"); at >= 0 {
			s.User, host = host[:at], host[at+1:]
		}
		if host == "" {
			return store.Session{}, false
		}
		s.Protocol = store.ProtocolSSH
		s.Host = host
		s.Port = v.num("PortNumber")
		if u := v.str("UserName"); u != "" {
			s.User = u
		}
		s.Auth = store.AuthAuto
		if key := v.str("PublicKeyFile"); key != "" {
			if strings.HasSuffix(strings.ToLower(key), ".ppk") {
				s.Notes = "PuTTY-Schlüssel (.ppk) nicht direkt nutzbar – mit PuTTYgen nach OpenSSH exportieren: " + key
			} else {
				s.Auth = store.AuthKey
				s.KeyPath = key
			}
		}
		if ka := v.num("PingIntervalSecs"); ka > 0 {
			s.KeepAlive = ka
		}
	case "serial":
		s.Protocol = store.ProtocolSerial
		s.SerialPort = v.str("SerialLine")
		s.Baud = v.num("SerialSpeed")
		s.DataBits = v.num("SerialDataBits")
		s.Parity = parity[v.num("SerialParity")]
		s.StopBits = stopBits[v.num("SerialStopHalfbits")]
	default:
		return store.Session{}, false
	}
	return s, true
}
