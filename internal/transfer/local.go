package transfer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// LocalRoot is the pseudo path for "Dieser PC" (the drive list on Windows).
const LocalRoot = ""

// ListLocal reads a local directory for the PC pane. LocalRoot lists drives
// on Windows (and "/" elsewhere).
func ListLocal(dir string) (Listing, error) {
	if dir == LocalRoot {
		return Listing{Path: LocalRoot, Entries: drives()}, nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Listing{}, err
	}
	des, err := os.ReadDir(abs)
	if err != nil {
		var pe *os.PathError
		if errors.As(err, &pe) { // already names the path once
			return Listing{}, fmt.Errorf("%s: %w", abs, pe.Err)
		}
		return Listing{}, err
	}
	entries := make([]Entry, 0, len(des))
	for _, de := range des {
		fi, err := de.Info()
		if err != nil {
			continue // vanished or unreadable entry
		}
		e := Entry{
			Name:    de.Name(),
			Path:    filepath.Join(abs, de.Name()),
			Size:    fi.Size(),
			Mode:    fi.Mode().String(),
			ModTime: fi.ModTime().UnixMilli(),
			IsDir:   fi.IsDir(),
			IsLink:  de.Type()&os.ModeSymlink != 0,
		}
		if e.IsLink {
			if t, err := os.Stat(e.Path); err == nil {
				e.IsDir = t.IsDir()
				if !e.IsDir {
					e.Size = t.Size()
				}
			}
		}
		entries = append(entries, e)
	}
	sortEntries(entries)
	return Listing{Path: abs, Entries: entries}, nil
}

// LocalParent returns the parent folder, or LocalRoot above a drive root.
func LocalParent(dir string) string {
	if dir == LocalRoot {
		return LocalRoot
	}
	clean := filepath.Clean(dir)
	parent := filepath.Dir(clean)
	if parent == clean { // C:\ or /
		if runtime.GOOS == "windows" {
			return LocalRoot
		}
		return "/"
	}
	return parent
}

func drives() []Entry {
	if runtime.GOOS != "windows" {
		return []Entry{{Name: "/", Path: "/", IsDir: true}}
	}
	// Only assigned letters: asking for a dead network drive can block for seconds.
	var out []Entry
	mask := driveMask()
	for i := range 26 {
		if mask&(1<<i) == 0 {
			continue
		}
		name := string(rune('A'+i)) + ":"
		out = append(out, Entry{Name: name, Path: name + `\`, IsDir: true})
	}
	return out
}

func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}
