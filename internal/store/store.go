// Package store persists sessions and folders as a JSON file.
//
// Every mutation builds a new snapshot, writes it atomically and only then
// swaps it in, so a failed write never leaves memory and disk out of sync.
package store

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"easytty/internal/fsutil"
)

const fileVersion = 1

// ErrNotFound is returned when a session or folder id does not exist.
var ErrNotFound = errors.New("nicht gefunden")

// Protector encrypts secrets at rest (DPAPI on Windows).
type Protector interface {
	Protect(plain []byte) ([]byte, error)
	Unprotect(cipher []byte) ([]byte, error)
}

type snapshot struct {
	Version  int               `json:"version"`
	Folders  []Folder          `json:"folders"`
	Sessions []Session         `json:"sessions"`
	Secrets  map[string]string `json:"secrets,omitempty"`
}

func (s snapshot) clone() snapshot {
	out := snapshot{
		Version:  s.Version,
		Folders:  append([]Folder(nil), s.Folders...),
		Sessions: append([]Session(nil), s.Sessions...),
		Secrets:  make(map[string]string, len(s.Secrets)),
	}
	for k, v := range s.Secrets {
		out.Secrets[k] = v
	}
	return out
}

func (s snapshot) sessionIndex(id string) int {
	for i, x := range s.Sessions {
		if x.ID == id {
			return i
		}
	}
	return -1
}

func (s snapshot) folderIndex(id string) int {
	for i, x := range s.Folders {
		if x.ID == id {
			return i
		}
	}
	return -1
}

func (s snapshot) folderExists(id string) bool {
	return id == "" || s.folderIndex(id) >= 0
}

// isDescendant reports whether candidate is folder id or lies below it.
func (s snapshot) isDescendant(candidate, id string) bool {
	seen := map[string]bool{}
	for cur := candidate; cur != "" && !seen[cur]; {
		if cur == id {
			return true
		}
		seen[cur] = true
		i := s.folderIndex(cur)
		if i < 0 {
			return false
		}
		cur = s.Folders[i].ParentID
	}
	return false
}

// Store is a concurrency-safe session database backed by a JSON file.
type Store struct {
	mu        sync.Mutex
	path      string
	data      snapshot
	protector Protector
	now       func() time.Time
}

// Open loads the store at path, creating an empty one if the file is missing.
func Open(path string, p Protector) (*Store, error) {
	st := &Store{path: path, protector: p, now: time.Now}
	st.data = snapshot{Version: fileVersion, Secrets: map[string]string{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Sessions laden: %w", err)
	}
	var snap snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		snap = snapshot{} // nothing of the broken parse may survive
		bak, berr := os.ReadFile(path + ".bak")
		if berr != nil || json.Unmarshal(bak, &snap) != nil {
			return nil, fmt.Errorf("Sessions-Datei beschädigt (%s): %w", path, err)
		}
		// keep the damaged file for inspection, continue with the backup
		_ = os.WriteFile(path+".damaged", raw, 0o600)
	}
	if snap.Version > fileVersion { // saving would drop what this version does not know
		return nil, fmt.Errorf("Sessions-Datei ist neuer (Version %d) als dieses easyTTY – bitte aktualisieren", snap.Version)
	}
	if snap.Secrets == nil {
		snap.Secrets = map[string]string{}
	}
	st.data = snap
	return st, nil
}

// Tree returns all folders and sessions without secrets.
func (s *Store) Tree() Tree {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := Tree{
		Folders:  append([]Folder{}, s.data.Folders...),
		Sessions: make([]Session, 0, len(s.data.Sessions)),
	}
	for _, x := range s.data.Sessions {
		x.HasPassword = s.data.Secrets[x.ID] != ""
		t.Sessions = append(t.Sessions, x)
	}
	return t
}

// Session returns one session by id.
func (s *Store) Session(id string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.data.sessionIndex(id)
	if i < 0 {
		return Session{}, ErrNotFound
	}
	out := s.data.Sessions[i]
	out.HasPassword = s.data.Secrets[id] != ""
	return out, nil
}

// SaveSession creates (empty ID) or updates a session.
func (s *Store) SaveSession(in Session) (Session, error) {
	sess := in.Normalize()
	sess.HasPassword = false
	if err := sess.Validate(); err != nil {
		return Session{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.data.folderExists(sess.FolderID) {
		return Session{}, fmt.Errorf("Ordner: %w", ErrNotFound)
	}
	next := s.data.clone()
	if sess.ID == "" {
		sess.ID = fsutil.NewID()
		next.Sessions = append(next.Sessions, sess)
	} else {
		i := next.sessionIndex(sess.ID)
		if i < 0 {
			return Session{}, ErrNotFound
		}
		sess.LastUsed = next.Sessions[i].LastUsed
		next.Sessions[i] = sess
	}
	if err := next.checkJump(sess); err != nil {
		return Session{}, err
	}
	if err := s.commit(next); err != nil {
		return Session{}, err
	}
	sess.HasPassword = next.Secrets[sess.ID] != ""
	return sess, nil
}

// DuplicateSession copies a session (without its password) next to the original.
func (s *Store) DuplicateSession(id string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.data.sessionIndex(id)
	if i < 0 {
		return Session{}, ErrNotFound
	}
	next := s.data.clone()
	cp := next.Sessions[i]
	cp.ID = fsutil.NewID()
	cp.Name = cp.Name + " (Kopie)"
	cp.LastUsed = 0
	next.Sessions = append(next.Sessions, cp)
	if err := s.commit(next); err != nil {
		return Session{}, err
	}
	return cp, nil
}

// DeleteSession removes a session and its stored password.
func (s *Store) DeleteSession(id string) error { return s.DeleteSessions([]string{id}) }

// DeleteSessions removes several sessions in one write (passwords and
// layout slots included). Unknown ids are an error; nothing is changed then.
func (s *Store) DeleteSessions(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		if s.data.sessionIndex(id) < 0 {
			return ErrNotFound
		}
	}
	next := s.data.clone()
	dropSessions(&next, ids)
	return s.commit(next)
}

// dropSessions removes sessions from a snapshot copy, with their stored
// passwords and their places in folder layouts.
func dropSessions(next *snapshot, ids []string) {
	gone := map[string]bool{}
	for _, id := range ids {
		gone[id] = true
		delete(next.Secrets, id)
	}
	kept := next.Sessions[:0:0]
	for _, x := range next.Sessions {
		if !gone[x.ID] {
			kept = append(kept, x)
		}
	}
	next.Sessions = kept
	for i, x := range next.Sessions {
		if gone[x.JumpID] { // its jump host is gone: connect directly
			next.Sessions[i].JumpID = ""
		}
	}
	for fi, f := range next.Folders {
		if f.Layout == nil || !slices.ContainsFunc(f.Layout.Slots, func(sid string) bool { return gone[sid] }) {
			continue
		}
		l := *f.Layout
		l.Slots = slices.Clone(l.Slots)
		for si, sid := range l.Slots {
			if gone[sid] {
				l.Slots[si] = ""
			}
		}
		next.Folders[fi].Layout = &l
	}
}

// MoveSession puts a session into another folder ("" = root).
func (s *Store) MoveSession(id, folderID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.data.sessionIndex(id)
	if i < 0 {
		return ErrNotFound
	}
	if !s.data.folderExists(folderID) {
		return fmt.Errorf("Ordner: %w", ErrNotFound)
	}
	next := s.data.clone()
	next.Sessions[i].FolderID = folderID
	return s.commit(next)
}

// Touch records that a session was just used.
func (s *Store) Touch(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.data.sessionIndex(id)
	if i < 0 {
		return ErrNotFound
	}
	next := s.data.clone()
	next.Sessions[i].LastUsed = s.now().UnixMilli()
	return s.commit(next)
}

// SaveFolder creates (empty ID) or renames/moves a folder.
func (s *Store) SaveFolder(in Folder) (Folder, error) {
	f := in
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" {
		return Folder{}, errors.New("Ordnername fehlt")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.data.folderExists(f.ParentID) {
		return Folder{}, fmt.Errorf("Elternordner: %w", ErrNotFound)
	}
	next := s.data.clone()
	if f.ID == "" {
		f.ID = fsutil.NewID()
		f.Layout = nil // layouts are set via SaveFolderLayout only
		next.Folders = append(next.Folders, f)
	} else {
		i := next.folderIndex(f.ID)
		if i < 0 {
			return Folder{}, ErrNotFound
		}
		if next.isDescendant(f.ParentID, f.ID) {
			return Folder{}, errors.New("Ordner kann nicht in sich selbst verschoben werden")
		}
		f.Layout = next.Folders[i].Layout // a rename must not drop the layout
		next.Folders[i] = f
	}
	if err := s.commit(next); err != nil {
		return Folder{}, err
	}
	return f, nil
}

// SaveFolderLayout stores the window layout of a folder; nil removes it.
// Slots are padded to the template's tile count; "" = free tile.
func (s *Store) SaveFolderLayout(id string, l *Layout) (Folder, error) {
	var clean *Layout
	if l != nil {
		n, ok := LayoutTemplates[l.Template]
		if !ok {
			return Folder{}, fmt.Errorf("unbekanntes Layout: %q", l.Template)
		}
		if len(l.Slots) > n {
			return Folder{}, fmt.Errorf("Layout %s hat nur %d Fenster", l.Template, n)
		}
		clean = &Layout{Template: l.Template, Slots: make([]string, n)}
		copy(clean.Slots, l.Slots)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.data.folderIndex(id)
	if i < 0 {
		return Folder{}, ErrNotFound
	}
	if clean != nil {
		for _, sid := range clean.Slots {
			if sid != "" && s.data.sessionIndex(sid) < 0 {
				return Folder{}, fmt.Errorf("Session im Layout: %w", ErrNotFound)
			}
		}
	}
	next := s.data.clone()
	next.Folders[i].Layout = clean
	if err := s.commit(next); err != nil {
		return Folder{}, err
	}
	return next.Folders[i], nil
}

// MoveFolder changes a folder's parent ("" = root).
func (s *Store) MoveFolder(id, parentID string) error {
	s.mu.Lock()
	i := s.data.folderIndex(id)
	if i < 0 {
		s.mu.Unlock()
		return ErrNotFound
	}
	f := s.data.Folders[i] // copy taken and saved without a gap a rename could fall into
	f.ParentID = parentID
	s.mu.Unlock()
	_, err := s.SaveFolder(f)
	return err
}

// DeleteFolder removes a folder; its sessions and subfolders move up to its parent.
// DeleteFolderWithSessions removes a folder, all its subfolders and every
// session inside them, in one write.
func (s *Store) DeleteFolderWithSessions(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.folderIndex(id) < 0 {
		return ErrNotFound
	}
	next := s.data.clone()
	doomed := map[string]bool{id: true}
	for grew := true; grew; { // collect subfolders level by level
		grew = false
		for _, f := range next.Folders {
			if doomed[f.ParentID] && !doomed[f.ID] {
				doomed[f.ID], grew = true, true
			}
		}
	}
	var ids []string
	for _, x := range next.Sessions {
		if doomed[x.FolderID] {
			ids = append(ids, x.ID)
		}
	}
	dropSessions(&next, ids)
	folders := next.Folders[:0:0]
	for _, f := range next.Folders {
		if !doomed[f.ID] {
			folders = append(folders, f)
		}
	}
	next.Folders = folders
	return s.commit(next)
}

func (s *Store) DeleteFolder(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.data.folderIndex(id)
	if i < 0 {
		return ErrNotFound
	}
	next := s.data.clone()
	parent := next.Folders[i].ParentID
	next.Folders = append(next.Folders[:i:i], next.Folders[i+1:]...)
	for j := range next.Folders {
		if next.Folders[j].ParentID == id {
			next.Folders[j].ParentID = parent
		}
	}
	for j := range next.Sessions {
		if next.Sessions[j].FolderID == id {
			next.Sessions[j].FolderID = parent
		}
	}
	return s.commit(next)
}

// SetPassword stores an encrypted password for a session; "" removes it.
func (s *Store) SetPassword(id, password string) error {
	var enc string
	if password != "" {
		if s.protector == nil {
			return errors.New("keine Verschlüsselung verfügbar")
		}
		c, err := s.protector.Protect([]byte(password))
		if err != nil {
			return fmt.Errorf("Passwort verschlüsseln: %w", err)
		}
		enc = base64.StdEncoding.EncodeToString(c)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.sessionIndex(id) < 0 {
		return ErrNotFound
	}
	next := s.data.clone()
	if enc == "" {
		delete(next.Secrets, id)
	} else {
		next.Secrets[id] = enc
	}
	return s.commit(next)
}

// Password returns the decrypted stored password ("" if none).
func (s *Store) Password(id string) (string, error) {
	s.mu.Lock()
	enc := s.data.Secrets[id]
	s.mu.Unlock()
	if enc == "" {
		return "", nil
	}
	if s.protector == nil {
		return "", errors.New("keine Verschlüsselung verfügbar")
	}
	c, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", fmt.Errorf("Passwort beschädigt: %w", err)
	}
	p, err := s.protector.Unprotect(c)
	if err != nil {
		return "", fmt.Errorf("Passwort entschlüsseln: %w", err)
	}
	return string(p), nil
}

// Import adds sessions into a (new or existing) root folder with the given name.
// Sessions that fail validation are skipped; the number imported is returned.
func (s *Store) Import(folderName string, sessions []Session) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.data.clone()
	folderID := ""
	for _, f := range next.Folders {
		if f.ParentID == "" && f.Name == folderName {
			folderID = f.ID
			break
		}
	}
	if folderID == "" {
		folderID = fsutil.NewID()
		next.Folders = append(next.Folders, Folder{ID: folderID, Name: folderName})
	}
	seen := map[string]bool{}
	for _, x := range next.Sessions {
		if x.FolderID == folderID {
			seen[importKey(x)] = true
		}
	}
	n := 0
	for _, in := range sessions {
		sess := in.Normalize()
		sess.ID = fsutil.NewID()
		sess.FolderID = folderID
		sess.HasPassword = false
		if sess.Validate() != nil || seen[importKey(sess)] {
			continue // invalid or already imported earlier
		}
		seen[importKey(sess)] = true
		next.Sessions = append(next.Sessions, sess)
		n++
	}
	if n == 0 {
		return 0, nil
	}
	return n, s.commit(next)
}

func importKey(s Session) string {
	return fmt.Sprintf("%s|%s|%s|%s|%d|%s", s.Name, s.Protocol, s.User, s.Host, s.Port, s.SerialPort)
}

// commit writes next atomically and swaps it in. Caller holds s.mu.
func (s *Store) commit(next snapshot) error {
	next.Version = fileVersion
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := fsutil.WriteAtomic(s.path, raw); err != nil {
		return fmt.Errorf("Sessions speichern: %w", err)
	}
	s.data = next
	return nil
}

// MaxJumps is the longest chain of jump hosts in front of a session.
const MaxJumps = 3

// checkJump makes sure the jump host of s is a saved SSH session and the
// chain ends within MaxJumps without coming back to s.
func (d snapshot) checkJump(s Session) error {
	seen := map[string]bool{s.ID: true}
	for id, hops := s.JumpID, 0; id != ""; hops++ {
		if hops == MaxJumps {
			return fmt.Errorf("Höchstens %d Sprung-Hosts hintereinander", MaxJumps)
		}
		if seen[id] {
			return errors.New("Die Sprung-Hosts verweisen im Kreis aufeinander")
		}
		seen[id] = true
		i := d.sessionIndex(id)
		if i < 0 {
			return fmt.Errorf("Sprung-Host: %w", ErrNotFound)
		}
		if d.Sessions[i].Protocol != ProtocolSSH {
			return errors.New("Sprung-Host muss eine SSH-Session sein")
		}
		id = d.Sessions[i].JumpID
	}
	return nil
}
