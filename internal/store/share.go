package store

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"easytty/internal/fsutil"
)

// Sharing: export sessions/folders/settings to a file colleagues can import.
// Exports never contain user names, passwords, key paths or log paths.

// ExportFormat identifies easyTTY share files.
const ExportFormat = "easytty-share"

const exportVersion = 1

// Bundle is the content of a share file.
type Bundle struct {
	Format   string    `json:"format"`
	Version  int       `json:"version"`
	Exported time.Time `json:"exported"`
	Folders  []Folder  `json:"folders"`
	Sessions []Session `json:"sessions"`
	Settings *Settings `json:"settings,omitempty"`
}

// ImportSummary reports what an import did.
type ImportSummary struct {
	Folders         int  `json:"folders"`
	Sessions        int  `json:"sessions"`
	Skipped         int  `json:"skipped"` // already present
	SettingsApplied bool `json:"settingsApplied"`
}

// shareable strips everything personal from a session.
func shareable(s Session) Session {
	out := s.Normalize()
	out.User = ""
	out.KeyPath = ""
	out.LogPath = ""
	out.HasPassword = false
	out.LastUsed = 0
	out.RdpDrives = false // a shared session must not get at the local drives
	if out.Auth == AuthKey {
		out.Auth = AuthAuto // key path is personal; colleagues pick their own
	}
	return out
}

// ShareableSettings drops machine-specific settings before export.
func ShareableSettings(s Settings) Settings {
	out := s
	out.LogDir, out.ConfigDir = "", ""
	out.FirstRunDone = false
	// The token is a secret; AI access is a decision for each PC.
	out.McpEnabled, out.McpToken, out.McpShowAuto, out.McpReadTabs, out.McpPort = false, "", false, false, 0
	return out
}

// Export collects the given folders (with all subfolders and their sessions)
// and single sessions. Empty folderIDs and sessionIDs with all=true exports everything.
func (s *Store) Export(folderIDs, sessionIDs []string, all bool) Bundle {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := Bundle{Format: ExportFormat, Version: exportVersion, Exported: s.now().UTC()}

	folders := map[string]bool{}
	if all {
		for _, f := range s.data.Folders {
			folders[f.ID] = true
		}
	}
	for _, id := range folderIDs {
		for _, f := range s.data.Folders {
			if s.data.isDescendant(f.ID, id) {
				folders[f.ID] = true
			}
		}
	}
	sessions := map[string]bool{}
	for _, id := range sessionIDs {
		sessions[id] = true
	}
	for _, x := range s.data.Sessions {
		if all || folders[x.FolderID] || sessions[x.ID] {
			sessions[x.ID] = true
		}
	}
	for _, f := range s.data.Folders {
		if !folders[f.ID] {
			continue
		}
		cp := f
		if !folders[cp.ParentID] {
			cp.ParentID = "" // exported subtree root
		}
		if cp.Layout != nil { // keep only slots that are part of the export
			l := &Layout{Template: cp.Layout.Template, Slots: make([]string, len(cp.Layout.Slots))}
			for i, sid := range cp.Layout.Slots {
				if sessions[sid] {
					l.Slots[i] = sid
				}
			}
			cp.Layout = l
		}
		b.Folders = append(b.Folders, cp)
	}
	for _, x := range s.data.Sessions {
		if !sessions[x.ID] {
			continue
		}
		cp := shareable(x)
		if !folders[cp.FolderID] {
			cp.FolderID = ""
		}
		b.Sessions = append(b.Sessions, cp)
	}
	return b
}

// ImportBundle merges a share file: folders are matched by name and parent
// (so re-importing an update does not duplicate them), sessions already
// present in the same folder are skipped, IDs and layouts are remapped.
func (s *Store) ImportBundle(b Bundle) (ImportSummary, error) { return s.importBundle(b, shareable) }

// ImportOwn merges sessions from the user's own other tools (SecureCRT):
// like ImportBundle, but user name and key path stay. Passwords never come along.
func (s *Store) ImportOwn(b Bundle) (ImportSummary, error) {
	return s.importBundle(b, func(in Session) Session {
		out := in.Normalize()
		out.HasPassword, out.LastUsed = false, 0
		return out
	})
}

func (s *Store) importBundle(b Bundle, clean func(Session) Session) (ImportSummary, error) {
	if b.Format != ExportFormat {
		return ImportSummary{}, errors.New("keine easyTTY-Exportdatei")
	}
	if b.Version > exportVersion {
		return ImportSummary{}, fmt.Errorf("Exportdatei ist neuer (Version %d) als dieses easyTTY", b.Version)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.data.clone()
	var sum ImportSummary

	folderMap := map[string]string{} // bundle id -> local id
	pending := append([]Folder(nil), b.Folders...)
	for progress := true; len(pending) > 0 && progress; {
		progress = false
		rest := pending[:0]
		for _, f := range pending {
			parent := ""
			if f.ParentID != "" {
				p, ok := folderMap[f.ParentID]
				if !ok {
					rest = append(rest, f)
					continue
				}
				parent = p
			}
			progress = true
			if id := findFolder(next, f.Name, parent); id != "" {
				folderMap[f.ID] = id
				continue
			}
			name := strings.TrimSpace(f.Name)
			if name == "" {
				name = "Import"
			}
			nf := Folder{ID: fsutil.NewID(), Name: name, ParentID: parent}
			next.Folders = append(next.Folders, nf)
			folderMap[f.ID] = nf.ID
			sum.Folders++
		}
		pending = rest
	}
	if len(pending) > 0 {
		return ImportSummary{}, errors.New("Exportdatei beschädigt: Ordner ohne Elternordner")
	}

	sessionMap := map[string]string{}
	for _, in := range b.Sessions {
		sess := clean(in).Normalize()
		sess.FolderID = folderMap[in.FolderID]
		if sess.Validate() != nil {
			sum.Skipped++
			continue
		}
		if id := findSession(next, sess); id != "" {
			sessionMap[in.ID] = id
			sum.Skipped++
			continue
		}
		sess.ID = fsutil.NewID()
		next.Sessions = append(next.Sessions, sess)
		sessionMap[in.ID] = sess.ID
		sum.Sessions++
	}

	// Jump hosts point at sessions of the file: use their new ids, or connect
	// directly when the jump host was not part of the export.
	for i := range next.Sessions {
		if old := next.Sessions[i].JumpID; old != "" && s.data.sessionIndex(next.Sessions[i].ID) < 0 {
			next.Sessions[i].JumpID = sessionMap[old]
		}
	}
	// A file may bring a loop, a chain that is too long or a jump over a
	// non-SSH session: such a session connects directly instead.
	for i := range next.Sessions {
		if s.data.sessionIndex(next.Sessions[i].ID) < 0 && next.checkJump(next.Sessions[i]) != nil {
			next.Sessions[i].JumpID = ""
		}
	}

	for _, f := range b.Folders {
		if f.Layout == nil {
			continue
		}
		i := next.folderIndex(folderMap[f.ID])
		if i < 0 || next.Folders[i].Layout != nil { // never overwrite a local layout
			continue
		}
		n, ok := LayoutTemplates[f.Layout.Template]
		if !ok {
			continue
		}
		l := &Layout{Template: f.Layout.Template, Slots: make([]string, n)}
		for j := 0; j < n && j < len(f.Layout.Slots); j++ {
			l.Slots[j] = sessionMap[f.Layout.Slots[j]]
		}
		next.Folders[i].Layout = l
	}
	if sum.Folders == 0 && sum.Sessions == 0 && !layoutsChanged(s.data, next) {
		return sum, nil
	}
	return sum, s.commit(next)
}

func findFolder(d snapshot, name, parent string) string {
	for _, f := range d.Folders {
		if f.Name == name && f.ParentID == parent {
			return f.ID
		}
	}
	return ""
}

func findSession(d snapshot, s Session) string {
	key := importKey(shareable(s)) // the user name is personal: match without it
	for _, x := range d.Sessions {
		if x.FolderID == s.FolderID && importKey(shareable(x)) == key {
			return x.ID
		}
	}
	return ""
}

func layoutsChanged(a, b snapshot) bool {
	for i := range b.Folders {
		if i >= len(a.Folders) || (a.Folders[i].Layout == nil) != (b.Folders[i].Layout == nil) {
			return true
		}
	}
	return false
}
