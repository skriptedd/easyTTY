// Package transfer implements the SFTP file browser and file transfers.
package transfer

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/pkg/sftp"
)

// Entry is one remote directory item.
type Entry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Mode    string `json:"mode"`
	ModTime int64  `json:"modTime"` // unix ms
	IsDir   bool   `json:"isDir"`
	IsLink  bool   `json:"isLink"`
}

// Listing is a directory with its canonical path.
type Listing struct {
	Path    string  `json:"path"`
	Entries []Entry `json:"entries"`
}

// List reads a remote directory; directories first, then names case-insensitively.
func List(c *sftp.Client, dir string) (Listing, error) {
	if dir == "" {
		dir = "."
	}
	real, err := c.RealPath(dir)
	if err != nil {
		return Listing{}, fmt.Errorf("%s: %w", dir, err)
	}
	infos, err := c.ReadDir(real)
	if err != nil {
		return Listing{}, fmt.Errorf("%s: %w", real, err)
	}
	entries := make([]Entry, 0, len(infos))
	for _, fi := range infos {
		e := Entry{
			Name:    fi.Name(),
			Path:    path.Join(real, fi.Name()),
			Size:    fi.Size(),
			Mode:    fi.Mode().String(),
			ModTime: fi.ModTime().UnixMilli(),
			IsDir:   fi.IsDir(),
			IsLink:  fi.Mode()&os.ModeSymlink != 0,
		}
		if e.IsLink {
			if target, err := c.Stat(e.Path); err == nil {
				e.IsDir = target.IsDir()
				if !e.IsDir {
					e.Size = target.Size()
				}
			}
		}
		entries = append(entries, e)
	}
	sortEntries(entries)
	return Listing{Path: real, Entries: entries}, nil
}

// Mkdir creates a remote directory.
func Mkdir(c *sftp.Client, p string) error {
	if err := c.Mkdir(p); err != nil {
		return fmt.Errorf("Ordner anlegen %s: %w", p, err)
	}
	return nil
}

// Rename moves a remote file or directory.
func Rename(c *sftp.Client, from, to string) error {
	if err := c.Rename(from, to); err != nil {
		return fmt.Errorf("Umbenennen %s: %w", from, err)
	}
	return nil
}

// Remove deletes remote files and directories (recursively).
func Remove(c *sftp.Client, paths []string) error {
	for _, raw := range paths {
		p := path.Clean(raw)
		if raw == "" || p == "/" || p == "." || p == ".." || strings.HasSuffix(p, "/..") {
			return fmt.Errorf("Löschen von %q verweigert", raw)
		}
		fi, err := c.Lstat(p)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if fi.IsDir() {
			err = c.RemoveAll(p)
		} else {
			err = c.Remove(p)
		}
		if err != nil {
			return fmt.Errorf("Löschen %s: %w", p, err)
		}
	}
	return nil
}
