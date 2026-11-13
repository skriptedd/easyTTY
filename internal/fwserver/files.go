// Package fwserver serves a firmware folder read-only to network devices via
// TFTP, HTTP and SCP/SFTP, so switches can pull images with "copy … flash:".
package fwserver

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ErrOutside is returned for paths that would leave the served folder.
var ErrOutside = errors.New("Pfad außerhalb des Firmware-Ordners")

// resolve maps a client-supplied name ("/c2960.bin", "sub\\x.bin") to a
// file inside root. Symlinks are followed and must stay inside root too.
func resolve(root, name string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootAbs = real
	}
	clean := filepath.Clean("/" + strings.ReplaceAll(name, "\\", "/"))
	if strings.Contains(clean, ":") { // C:\…, ADS streams
		return "", ErrOutside
	}
	for _, seg := range strings.Split(filepath.ToSlash(clean), "/") {
		if strings.HasPrefix(seg, ".") || reservedName(seg) { // .git, CON, COM1 …
			return "", ErrOutside
		}
	}
	p := filepath.Join(rootAbs, filepath.FromSlash(clean))
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", ErrOutside
	}
	return p, nil
}

// reservedName reports Windows device names (CON, NUL, COM1, LPT1 …), which
// os.Open would treat as devices and could block on.
func reservedName(seg string) bool {
	base := strings.ToUpper(strings.TrimRight(strings.SplitN(seg, ".", 2)[0], " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	return len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9'
}

// openFile opens a regular file inside root for reading.
func openFile(root, name string) (*os.File, os.FileInfo, error) {
	p, err := resolve(root, name)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if !fi.Mode().IsRegular() {
		f.Close()
		return nil, nil, errNotFile
	}
	return f, fi, nil
}

// reason turns a file error into a short log text without local paths.
func reason(err error) string {
	switch {
	case errors.Is(err, ErrOutside):
		return ErrOutside.Error()
	case errors.Is(err, os.ErrNotExist):
		return "Datei nicht gefunden"
	case errors.Is(err, os.ErrPermission):
		return "keine Leseberechtigung"
	case errors.Is(err, errNotFile):
		return "ist ein Ordner, keine Datei"
	}
	return "nicht lesbar"
}

var errNotFile = errors.New("keine Datei")

// File is one servable image.
type File struct {
	Name    string `json:"name"` // relative, with forward slashes
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
	MD5     string `json:"md5"`
}

type md5Key struct {
	path string
	size int64
	mod  int64
}

var (
	md5Mu    sync.Mutex
	md5Cache = map[md5Key]string{}
)

// fileMD5 hashes a file once per path/size/mtime (images are large).
func fileMD5(p string, fi os.FileInfo) (string, error) {
	k := md5Key{p, fi.Size(), fi.ModTime().UnixNano()}
	md5Mu.Lock()
	sum, ok := md5Cache[k]
	md5Mu.Unlock()
	if ok {
		return sum, nil
	}
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	sum = hex.EncodeToString(h.Sum(nil))
	md5Mu.Lock()
	md5Cache[k] = sum
	md5Mu.Unlock()
	return sum, nil
}

const maxListed = 500

// ListFiles lists regular files below root (recursively) with MD5 sums,
// which devices check with "verify /md5".
func ListFiles(root string) ([]File, error) {
	var out []File
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || len(out) >= maxListed {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		sum, _ := fileMD5(p, fi)
		out = append(out, File{Name: filepath.ToSlash(rel), Size: fi.Size(), ModTime: fi.ModTime().UnixMilli(), MD5: sum})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, err
}

// Address is a local IPv4 address devices can reach.
type Address struct {
	IP        string `json:"ip"`
	Interface string `json:"interface"`
}

// LocalAddresses lists non-loopback IPv4 addresses of interfaces that are up.
func LocalAddresses() []Address {
	var out []Address
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				if ip4 := ipn.IP.To4(); ip4 != nil && !ip4.IsLinkLocalUnicast() {
					out = append(out, Address{IP: ip4.String(), Interface: ifc.Name})
				}
			}
		}
	}
	return out
}
