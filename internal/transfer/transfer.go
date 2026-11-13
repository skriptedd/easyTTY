package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
)

const reportEvery = 150 * time.Millisecond

// Progress reports the state of one transfer job.
type Progress struct {
	JobID     string `json:"jobId"`
	TabID     string `json:"tabId"`
	Kind      string `json:"kind"` // upload | download
	State     string `json:"state"`
	File      string `json:"file"`
	Done      int64  `json:"done"`
	Total     int64  `json:"total"`
	Files     int    `json:"files"`
	FilesDone int    `json:"filesDone"`
	Skipped   int    `json:"skipped"` // links to directories, devices, sockets
	Dest      string `json:"dest,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Progress states.
const (
	StateRunning   = "running"
	StateDone      = "done"
	StateError     = "error"
	StateCancelled = "cancelled"
)

type item struct {
	src, dst string
	size     int64
	mtime    time.Time
	dir      bool
}

type tracker struct {
	mu     sync.Mutex
	p      Progress
	done   atomic.Int64
	last   time.Time
	report func(Progress)
}

func (t *tracker) add(n int) {
	t.done.Add(int64(n))
	t.mu.Lock()
	defer t.mu.Unlock()
	if time.Since(t.last) >= reportEvery {
		t.last = time.Now()
		p := t.p
		p.Done = t.done.Load()
		t.report(p)
	}
}

func (t *tracker) set(f func(*Progress)) Progress {
	t.mu.Lock()
	defer t.mu.Unlock()
	f(&t.p)
	p := t.p
	p.Done = t.done.Load()
	return p
}

func (t *tracker) emit(f func(*Progress)) { t.report(t.set(f)) }

// ctxReader aborts on cancellation and counts bytes. Stat is forwarded so
// pkg/sftp can size its concurrent upload pipeline.
type ctxReader struct {
	ctx context.Context
	f   *os.File
	t   *tracker
}

func (r ctxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.f.Read(p)
	r.t.add(n)
	return n, err
}

func (r ctxReader) Stat() (os.FileInfo, error) { return r.f.Stat() }

type ctxWriter struct {
	ctx context.Context
	w   io.Writer
	t   *tracker
}

func (w ctxWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.w.Write(p)
	w.t.add(n)
	return n, err
}

// What Upload does when a top-level name already exists in the target folder.
const (
	OnExistOverwrite = "overwrite" // replace files, merge into folders
	OnExistRename    = "rename"    // upload as "name (1).ext"
	OnExistSkip      = "skip"      // leave it out
)

// Upload copies local files/directories into remoteDir. onExist decides about
// names that are already there (see OnExist*); "" means overwrite.
func Upload(ctx context.Context, c *sftp.Client, p Progress, localPaths []string, remoteDir, onExist string, report func(Progress)) error {
	p.Kind = "upload"
	roots, names, left, err := remoteNames(c, localPaths, remoteDir, onExist)
	if err != nil {
		report(withError(p, err))
		return err
	}
	items, skipped, err := planUpload(roots, names, remoteDir)
	if err != nil {
		report(withError(p, err))
		return err
	}
	p.Skipped, p.Dest = skipped+left, remoteDir
	return run(ctx, p, items, report, func(it item, t *tracker) error {
		if it.dir {
			return c.MkdirAll(it.dst)
		}
		return uploadFile(ctx, c, it, t)
	})
}

// Download copies remote files/directories into localDir.
func Download(ctx context.Context, c *sftp.Client, p Progress, remotePaths []string, localDir string, report func(Progress)) error {
	p.Kind = "download"
	items, skipped, err := planDownload(ctx, c, remotePaths, localDir)
	if err != nil {
		report(withError(p, err))
		return err
	}
	p.Skipped, p.Dest = skipped, localDir
	return run(ctx, p, items, report, func(it item, t *tracker) error {
		if it.dir {
			return os.MkdirAll(it.dst, 0o755)
		}
		return downloadFile(ctx, c, it, t)
	})
}

func withError(p Progress, err error) Progress {
	p.State = StateError
	p.Error = err.Error()
	return p
}

func run(ctx context.Context, p Progress, items []item, report func(Progress), do func(item, *tracker) error) error {
	t := &tracker{p: p, report: report}
	t.set(func(p *Progress) {
		p.State = StateRunning
		for _, it := range items {
			if !it.dir {
				p.Total += it.size
				p.Files++
			}
		}
	})
	t.emit(func(*Progress) {})
	for _, it := range items {
		if !it.dir {
			t.emit(func(p *Progress) { p.File = it.src })
		}
		if err := do(it, t); err != nil {
			if ctx.Err() != nil {
				t.emit(func(p *Progress) { p.State = StateCancelled })
				return ctx.Err()
			}
			err = fmt.Errorf("%s: %w", it.src, err)
			t.emit(func(p *Progress) { p.State = StateError; p.Error = err.Error() })
			return err
		}
		if !it.dir {
			t.set(func(p *Progress) { p.FilesDone++ })
		}
	}
	t.emit(func(p *Progress) { p.State = StateDone; p.File = "" })
	return nil
}

// remoteNames picks the remote name of every top-level path and drops the
// ones to skip. left counts the skipped ones.
func remoteNames(c remoteStat, localPaths []string, remoteDir, onExist string) (roots, names []string, left int, err error) {
	switch onExist {
	case "", OnExistOverwrite, OnExistRename, OnExistSkip:
	default:
		return nil, nil, 0, fmt.Errorf("unbekannte Auswahl für vorhandene Dateien: %q", onExist)
	}
	taken := map[string]bool{} // names planned in this batch
	exists := func(name string) bool {
		if taken[name] {
			return true
		}
		_, err := c.Lstat(path.Join(remoteDir, name))
		return err == nil
	}
	for _, lp := range localPaths {
		root := filepath.Clean(lp)
		name := filepath.Base(root)
		if exists(name) {
			switch onExist {
			case OnExistSkip:
				left++
				continue
			case OnExistRename:
				name = freeRemoteName(name, exists)
			}
		}
		taken[name] = true
		roots, names = append(roots, root), append(names, name)
	}
	return roots, names, left, nil
}

// remoteStat is the part of the SFTP client remoteNames needs.
type remoteStat interface {
	Lstat(p string) (os.FileInfo, error)
}

// freeRemoteName returns "name (n).ext" for the first n that is not taken.
func freeRemoteName(name string, exists func(string) bool) string {
	ext := path.Ext(name)
	if ext == name { // ".profile" has no extension, it is the name
		ext = ""
	}
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		if cand := fmt.Sprintf("%s (%d)%s", stem, i, ext); !exists(cand) {
			return cand
		}
	}
}

// planUpload walks roots[i] and maps it to remoteDir/names[i].
func planUpload(roots, names []string, remoteDir string) ([]item, int, error) {
	var items []item
	skipped := 0
	for i, root := range roots {
		top := path.Join(remoteDir, names[i])
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			dst := top
			if rel != "." {
				dst = path.Join(top, filepath.ToSlash(rel))
			}
			if d.IsDir() {
				items = append(items, item{src: p, dst: dst, dir: true})
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if d.Type()&os.ModeSymlink != 0 {
				// Follow links to files; linked directories are skipped (loops).
				if t, err := os.Stat(p); err == nil && t.Mode().IsRegular() {
					info = t
				} else {
					skipped++
					return nil
				}
			} else if !d.Type().IsRegular() {
				skipped++
				return nil
			}
			items = append(items, item{src: p, dst: dst, size: info.Size(), mtime: info.ModTime()})
			return nil
		})
		if err != nil {
			return nil, 0, err
		}
	}
	return items, skipped, nil
}

// planDownload maps remote paths to local targets. Existing local names are
// never overwritten: a clashing top-level item becomes "name (1)" etc.
// Symlinked files are followed; symlinked directories below the top level are
// skipped (loop protection) and counted.
func planDownload(ctx context.Context, c *sftp.Client, remotePaths []string, localDir string) ([]item, int, error) {
	var items []item
	skipped := 0
	reserved := map[string]bool{} // targets planned in this batch (not on disk yet)
	for _, rp := range remotePaths {
		root := path.Clean(rp)
		name := path.Base(root)
		if name == "/" || name == "." || name == ".." {
			name = "root"
		}
		walkRoot := root
		if fi, err := c.Lstat(root); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			if t, err := c.Stat(root); err == nil && t.IsDir() {
				if real, err := c.RealPath(root); err == nil {
					walkRoot = real
				}
			}
		}
		dstRoot, err := safeLocalPath(localDir, name)
		if err != nil {
			return nil, 0, err
		}
		dstRoot = uniqueLocal(dstRoot, reserved)
		reserved[strings.ToLower(dstRoot)] = true

		w := c.Walk(walkRoot)
		for w.Step() {
			if err := ctx.Err(); err != nil {
				return nil, 0, err
			}
			if err := w.Err(); err != nil {
				if w.Path() == walkRoot {
					return nil, 0, err
				}
				skipped++ // one unreadable folder must not stop the whole download
				continue
			}
			rel, ok := relUnder(walkRoot, w.Path())
			if !ok {
				return nil, 0, errors.New("Server lieferte unerwarteten Pfad: " + w.Path())
			}
			dst := dstRoot
			if rel != "" {
				if dst, err = safeLocalPath(dstRoot, rel); err != nil {
					return nil, 0, err
				}
			}
			fi := w.Stat()
			if fi.Mode()&os.ModeSymlink != 0 {
				t, err := c.Stat(w.Path())
				if err != nil || !t.Mode().IsRegular() {
					skipped++
					continue
				}
				fi = t
			}
			switch {
			case fi.IsDir():
				items = append(items, item{src: w.Path(), dst: dst, dir: true})
			case fi.Mode().IsRegular():
				if rel != "" {
					dst = uniqueLocal(dst, reserved) // Linux: Readme and readme, Windows: one file
					reserved[strings.ToLower(dst)] = true
				}
				items = append(items, item{src: w.Path(), dst: dst, size: fi.Size(), mtime: fi.ModTime()})
			default:
				skipped++
			}
		}
	}
	return items, skipped, nil
}

// relUnder returns p relative to root ("" for root itself); ok=false if p is outside.
func relUnder(root, p string) (string, bool) {
	if p == root {
		return "", true
	}
	prefix := strings.TrimSuffix(root, "/") + "/"
	if !strings.HasPrefix(p, prefix) {
		return "", false
	}
	return p[len(prefix):], true
}

// uniqueLocal returns p, or "name (n).ext" if p already exists.
func uniqueLocal(p string, reserved map[string]bool) string {
	free := func(c string) bool {
		_, err := os.Lstat(c)
		return errors.Is(err, os.ErrNotExist) && !reserved[strings.ToLower(c)]
	}
	if free(p) {
		return p
	}
	ext := filepath.Ext(p)
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		ext = ""
	}
	stem := strings.TrimSuffix(p, ext)
	for i := 1; ; i++ {
		cand := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if free(cand) {
			return cand
		}
	}
}

// winReserved are device names Windows will not use as file names.
var winReserved = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true}

// badWinName rejects remote names Windows would misread: "a:b" (alternate
// data stream), device names like CON/COM1 and trailing dots or spaces.
func badWinName(rel string) bool {
	for _, seg := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		if strings.ContainsAny(seg, ":*?\"<>|") || strings.TrimRight(seg, ". ") != seg {
			return true
		}
		base := strings.ToUpper(strings.SplitN(seg, ".", 2)[0])
		if winReserved[base] || len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
			return true
		}
	}
	return false
}

// safeLocalPath joins rel under dir and refuses names escaping it (e.g. "..\\")
// or names Windows cannot store faithfully.
func safeLocalPath(dir, rel string) (string, error) {
	if badWinName(rel) {
		return "", errors.New("Dateiname unter Windows nicht möglich: " + rel)
	}
	root := filepath.Clean(dir)
	p := filepath.Join(root, filepath.FromSlash(rel))
	r, err := filepath.Rel(root, p)
	if err != nil || r == ".." || filepath.IsAbs(r) || len(r) >= 3 && r[:3] == ".."+string(filepath.Separator) {
		return "", errors.New("unsicherer Dateiname: " + rel)
	}
	return p, nil
}

func uploadFile(ctx context.Context, c *sftp.Client, it item, t *tracker) error {
	lf, err := os.Open(it.src)
	if err != nil {
		return err
	}
	defer lf.Close()
	// Written next to the target and swapped in at the end: a cancelled or broken
	// upload leaves the existing remote file untouched.
	tmp := it.dst + ".easytty-part"
	rf, err := c.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	_, err = rf.ReadFrom(ctxReader{ctx: ctx, f: lf, t: t})
	if cerr := rf.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = swapRemote(c, tmp, it.dst)
	}
	if err != nil {
		_ = c.Remove(tmp)
		return err
	}
	_ = c.Chtimes(it.dst, it.mtime, it.mtime) // best effort
	return nil
}

func downloadFile(ctx context.Context, c *sftp.Client, it item, t *tracker) error {
	rf, err := c.Open(it.src)
	if err != nil {
		return err
	}
	defer rf.Close()
	if err := os.MkdirAll(filepath.Dir(it.dst), 0o755); err != nil {
		return err
	}
	// A unique temp name: an existing "name.part" of the user stays untouched.
	lf, err := os.CreateTemp(filepath.Dir(it.dst), filepath.Base(it.dst)+".*.part")
	if err != nil {
		return err
	}
	tmp := lf.Name()
	_, err = rf.WriteTo(ctxWriter{ctx: ctx, w: lf, t: t})
	if cerr := lf.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, it.dst); err != nil {
		os.Remove(tmp)
		return err
	}
	_ = os.Chtimes(it.dst, it.mtime, it.mtime)
	return nil
}

// remoteMover is the part of the SFTP client that swapRemote needs.
type remoteMover interface {
	PosixRename(oldname, newname string) error
	Rename(oldname, newname string) error
	Lstat(p string) (os.FileInfo, error)
	Remove(p string) error
}

const remoteBackupSuffix = ".easytty-old"

// swapRemote moves tmp over dst. Servers without the posix-rename extension
// refuse to rename onto an existing file. The old file is then parked under
// a backup name and comes back if the new one cannot take its place.
func swapRemote(c remoteMover, tmp, dst string) error {
	if err := c.PosixRename(tmp, dst); err == nil {
		return nil
	}
	if _, err := c.Lstat(dst); err != nil {
		return c.Rename(tmp, dst)
	}
	bak := dst + remoteBackupSuffix
	_ = c.Remove(bak)
	if err := c.Rename(dst, bak); err != nil {
		return err
	}
	if err := c.Rename(tmp, dst); err != nil {
		if back := c.Rename(bak, dst); back != nil {
			return fmt.Errorf("%w (die bisherige Datei liegt unter %s)", err, bak)
		}
		return err
	}
	_ = c.Remove(bak)
	return nil
}
