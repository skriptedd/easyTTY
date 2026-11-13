// Package securecrt reads SecureCRT sessions – the Config\Sessions folder of
// .ini files or an XML export ("Tools → Export Settings") – into easyTTY
// sessions and folders. Passwords are never imported.
package securecrt

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"easytty/internal/store"
)

// Result is what an import found.
type Result struct {
	Folders  []store.Folder  // IDs are local to the result, ParentID links them
	Sessions []store.Session // FolderID refers to Folders
	Skipped  []string        // "name (Telnet)" …
}

// values are the settings of one session (name → raw value).
type values map[string]string

// DefaultDir returns SecureCRT's session folder if it exists: the configured
// "Config Path" (passed in, from the registry) or %APPDATA%\VanDyke\Config.
func DefaultDir(configPath string) (string, bool) {
	var cands []string
	if configPath != "" {
		cands = append(cands, filepath.Join(configPath, "Sessions"))
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		cands = append(cands, filepath.Join(appData, "VanDyke", "Config", "Sessions"))
	}
	for _, c := range cands {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c, true
		}
	}
	return "", false
}

// Load reads a Sessions folder or an XML export file.
func Load(path string) (Result, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return Result{}, err
	}
	if fi.IsDir() {
		return loadDir(path)
	}
	if fi.Size() > maxFileBytes {
		return Result{}, fmt.Errorf("%s ist zu groß für einen SecureCRT-Export", filepath.Base(path))
	}
	f, err := os.Open(path)
	if err != nil {
		return Result{}, err
	}
	defer f.Close()
	return loadXML(f)
}

type builder struct {
	res     Result
	folders map[string]string // "A/B" → folder id
}

func newBuilder() *builder { return &builder{folders: map[string]string{}} }

// folder returns the id for a folder path, creating parents as needed.
func (b *builder) folder(path []string) string {
	parent := ""
	for i := range path {
		key := strings.Join(path[:i+1], "/")
		id, ok := b.folders[key]
		if !ok {
			id = fmt.Sprintf("f%d", len(b.folders)+1)
			b.folders[key] = id
			b.res.Folders = append(b.res.Folders, store.Folder{ID: id, Name: path[i], ParentID: parent})
		}
		parent = id
	}
	return parent
}

// add converts one session's settings; hex=true for .ini dword values.
func (b *builder) add(path []string, name string, v values, hex bool) {
	num := func(key string, def int) int {
		raw, ok := v[key]
		if !ok || raw == "" {
			return def
		}
		base := 10
		if hex {
			base = 16
		}
		n, err := strconv.ParseInt(strings.TrimSpace(raw), base, 64)
		if err != nil {
			return def
		}
		return int(n)
	}
	proto := strings.ToLower(v["Protocol Name"])
	s := store.Session{ID: fmt.Sprintf("s%d", len(b.res.Sessions)+1), Name: name, FolderID: b.folder(path)}
	switch {
	case strings.HasPrefix(proto, "ssh"):
		s.Protocol = store.ProtocolSSH
		s.Host = strings.TrimSpace(v["Hostname"])
		s.User = strings.TrimSpace(v["Username"])
		s.Port = num("[SSH2] Port", 0)
		if proto == "ssh1" {
			s.Port = num("[SSH1] Port", 0)
		}
		if key := strings.TrimSpace(strings.SplitN(v["Identity Filename V2"], "::", 2)[0]); key != "" && !isNetworkPath(key) {
			s.KeyPath, s.Auth = key, store.AuthKey
		}
	case proto == "rdp":
		s.Protocol = store.ProtocolRDP
		s.Host = strings.TrimSpace(v["Hostname"])
		s.User = strings.TrimSpace(v["Username"])
	case proto == "serial":
		s.Protocol = store.ProtocolSerial
		s.SerialPort = strings.TrimSpace(v["Com Port"])
		s.Baud = num("Baud Rate", 9600)
		s.DataBits = num("Data Bits", 8)
		s.Parity = [...]string{"none", "odd", "even", "mark", "space"}[min(max(num("Parity", 0), 0), 4)]
		s.StopBits = [...]string{"1", "1.5", "2"}[min(max(num("Stop Bits", 0), 0), 2)]
	default:
		label := v["Protocol Name"]
		if label == "" {
			label = "ohne Protokoll"
		}
		b.res.Skipped = append(b.res.Skipped, fmt.Sprintf("%s (%s)", name, label))
		return
	}
	b.res.Sessions = append(b.res.Sessions, s)
}

/* ---------- Config\Sessions folder with .ini files ---------- */

func loadDir(root string) (Result, error) {
	b := newBuilder()
	var files []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".ini") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	sort.Strings(files)
	for _, p := range files {
		rel, _ := filepath.Rel(root, p)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		name := strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(p))
		folder := parts[:len(parts)-1]
		// __FolderData__ describes a folder; Default is SecureCRT's template session.
		if strings.EqualFold(name, "__FolderData__") || (len(folder) == 0 && strings.EqualFold(name, "Default")) {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return Result{}, err
		}
		b.add(folder, name, parseINI(raw), true)
	}
	return b.res, nil
}

// parseINI reads SecureCRT's 'S:"Name"=value' / 'D:"Name"=0000001a' lines;
// binary ('B:') and multi-line ('Z:') values with their continuation lines are skipped.
func parseINI(raw []byte) values {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	v := values{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if len(line) < 5 || line[1] != ':' || line[2] != '"' {
			continue
		}
		kind := line[0]
		end := strings.Index(line[3:], `"=`)
		if end < 0 {
			continue
		}
		key, val := line[3:3+end], line[3+end+2:]
		if kind == 'S' || kind == 'D' {
			v[key] = val
		}
	}
	return v
}

/* ---------- XML export ---------- */

type xmlKey struct {
	Name    string     `xml:"name,attr"`
	Keys    []xmlKey   `xml:"key"`
	Strings []xmlValue `xml:"string"`
	Dwords  []xmlValue `xml:"dword"`
}

type xmlValue struct {
	Name  string `xml:"name,attr"`
	Value string `xml:",chardata"`
}

func loadXML(r io.Reader) (Result, error) {
	var doc struct {
		Keys []xmlKey `xml:"key"`
	}
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return Result{}, fmt.Errorf("keine SecureCRT-XML-Datei: %w", err)
	}
	b := newBuilder()
	found := false
	for _, k := range doc.Keys {
		if strings.EqualFold(k.Name, "Sessions") {
			found = true
			for _, c := range k.Keys {
				walkXML(b, nil, c)
			}
		}
	}
	if !found {
		return Result{}, fmt.Errorf("keine Sessions in der XML-Datei (SecureCRT: Tools → Export Settings, mit Sessions)")
	}
	return b.res, nil
}

// walkXML: a key with values is a session, a key with only subkeys a folder.
func walkXML(b *builder, path []string, k xmlKey) {
	if len(k.Strings) > 0 || len(k.Dwords) > 0 {
		if strings.EqualFold(k.Name, "__FolderData__") || (len(path) == 0 && strings.EqualFold(k.Name, "Default")) {
			return
		}
		v := values{}
		for _, s := range k.Strings {
			v[s.Name] = s.Value
		}
		for _, d := range k.Dwords {
			v[d.Name] = d.Value
		}
		b.add(path, k.Name, v, false)
		return
	}
	sub := append(append([]string(nil), path...), k.Name)
	for _, c := range k.Keys {
		walkXML(b, sub, c)
	}
}

// maxFileBytes limits one export file; real ones are far smaller.
const maxFileBytes = 32 << 20

// isNetworkPath reports UNC paths. A key file on a foreign share would make
// Windows log in there with the user's account as soon as the session connects.
func isNetworkPath(p string) bool {
	return strings.HasPrefix(p, `\`) || strings.HasPrefix(p, "//")
}
