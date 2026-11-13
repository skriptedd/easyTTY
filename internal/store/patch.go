package store

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// SessionPatch changes several sessions at once (folder or global level).
// Nil fields stay as they are; set fields apply where the protocol has them:
// User for SSH, serial and RDP; Auth, KeyPath and KeepAlive for SSH only;
// Color and Password for all. An empty Password removes stored passwords.
type SessionPatch struct {
	User      *string `json:"user"`
	Auth      *string `json:"auth"`
	KeyPath   *string `json:"keyPath"`
	KeepAlive *int    `json:"keepAlive"`
	Color     *string `json:"color"`
	Password  *string `json:"password"`
}

func (p SessionPatch) empty() bool {
	return p.User == nil && p.Auth == nil && p.KeyPath == nil && p.KeepAlive == nil && p.Color == nil && p.Password == nil
}

// patchedUser keeps the line of a console server login (admin:6) on SSH
// sessions: a new user replaces only the part before the colon. A new value
// with its own colon wins, and an empty one clears the whole login.
func patchedUser(s Session, user string) string {
	isSSH := s.Protocol == ProtocolSSH || s.Protocol == ""
	if !isSSH || user == "" || strings.Contains(user, ":") {
		return user
	}
	if _, line, found := strings.Cut(s.User, ":"); found {
		return user + ":" + line
	}
	return user
}

// apply returns the patched copy of a session.
func (p SessionPatch) apply(s Session) Session {
	out := s
	if p.User != nil {
		out.User = patchedUser(s, *p.User)
	}
	if p.Color != nil {
		out.Color = *p.Color
	}
	if out.Protocol == ProtocolSSH || out.Protocol == "" {
		if p.Auth != nil {
			out.Auth = *p.Auth
		}
		if p.KeyPath != nil {
			out.KeyPath = *p.KeyPath
		}
		if p.KeepAlive != nil {
			out.KeepAlive = *p.KeepAlive
		}
	}
	return out.Normalize()
}

// PatchSessions applies p to the sessions of a folder – folderID "" is the
// top level, recursive includes all subfolders (so "" + recursive = every
// session). Everything is checked first and written at once; if one session
// would become invalid, nothing changes. Returns the number of sessions.
func (s *Store) PatchSessions(folderID string, recursive bool, p SessionPatch) (int, error) {
	if p.empty() {
		return 0, errors.New("nichts zum Ändern ausgewählt")
	}
	var enc string
	if p.Password != nil && *p.Password != "" {
		if s.protector == nil {
			return 0, errors.New("keine Verschlüsselung verfügbar")
		}
		c, err := s.protector.Protect([]byte(*p.Password))
		if err != nil {
			return 0, fmt.Errorf("Passwort verschlüsseln: %w", err)
		}
		enc = base64.StdEncoding.EncodeToString(c)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.data.folderExists(folderID) {
		return 0, fmt.Errorf("Ordner: %w", ErrNotFound)
	}
	next := s.data.clone()
	n := 0
	for i, x := range next.Sessions {
		if !inScope(next, x.FolderID, folderID, recursive) {
			continue
		}
		patched := p.apply(x)
		if err := patched.Validate(); err != nil {
			return 0, fmt.Errorf("%s: %w", x.Name, err)
		}
		next.Sessions[i] = patched
		if p.Password != nil {
			if enc == "" {
				delete(next.Secrets, x.ID)
			} else {
				next.Secrets[x.ID] = enc
			}
		}
		n++
	}
	if n == 0 {
		return 0, nil
	}
	return n, s.commit(next)
}

// inScope: is a session in folder sessionFolder affected by a change on folderID?
func inScope(d snapshot, sessionFolder, folderID string, recursive bool) bool {
	if sessionFolder == folderID {
		return true
	}
	if !recursive {
		return false
	}
	return folderID == "" || d.isDescendant(sessionFolder, folderID)
}
