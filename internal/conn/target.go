// Package conn opens terminal connections (SSH and serial).
package conn

import (
	"errors"
	"strconv"
	"strings"
)

// Target is a parsed quick-connect address.
type Target struct {
	User string `json:"user"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

// ParseTarget parses "user@host:port" style input. Supports bracketed IPv6
// ("[::1]:2222"), bare IPv6 ("fe80::1"), an optional "ssh://" prefix and
// "host -p 2222".
func ParseTarget(input string) (Target, error) {
	s := strings.TrimSpace(input)
	s = strings.TrimPrefix(s, "ssh://")
	t := Target{Port: 22}

	if fields := strings.Fields(s); len(fields) == 3 && fields[1] == "-p" {
		p, err := parsePort(fields[2])
		if err != nil {
			return Target{}, err
		}
		s, t.Port = fields[0], p
	}
	if strings.ContainsAny(s, " \t") {
		return Target{}, errors.New("Adresse darf keine Leerzeichen enthalten")
	}
	if at := strings.LastIndex(s, "@"); at >= 0 {
		t.User, s = s[:at], s[at+1:]
	}

	switch {
	case strings.HasPrefix(s, "["):
		end := strings.Index(s, "]")
		if end < 0 {
			return Target{}, errors.New("schließende ] fehlt")
		}
		t.Host = s[1:end]
		rest := s[end+1:]
		if rest != "" {
			if !strings.HasPrefix(rest, ":") {
				return Target{}, errors.New("ungültige Adresse")
			}
			p, err := parsePort(rest[1:])
			if err != nil {
				return Target{}, err
			}
			t.Port = p
		}
	case strings.Count(s, ":") == 1:
		host, port, _ := strings.Cut(s, ":")
		p, err := parsePort(port)
		if err != nil {
			return Target{}, err
		}
		t.Host, t.Port = host, p
	default:
		t.Host = s
	}
	if t.Host == "" {
		return Target{}, errors.New("Host fehlt")
	}
	return t, nil
}

func parsePort(s string) (int, error) {
	p, err := strconv.Atoi(s)
	if err != nil || p < 1 || p > 65535 {
		return 0, errors.New("ungültiger Port: " + s)
	}
	return p, nil
}
