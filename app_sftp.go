package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"

	"github.com/pkg/sftp"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"easytty/internal/conn"
	"easytty/internal/fsutil"
	"easytty/internal/transfer"
)

const sftpConcurrency = 64

var (
	errNoSFTP   = errors.New("SFTP nur bei SSH-Verbindungen verfügbar")
	errNoStatus = errors.New("Server-Status gibt es nur bei SSH-Verbindungen (an der seriellen Konsole würde die Abfrage mittippen)")
)

type sftpCloser struct {
	a     *App
	tabID string
	c     *sftp.Client
}

func (s sftpCloser) Close() error {
	s.a.mu.Lock()
	if s.a.sftp[s.tabID] == s.c {
		delete(s.a.sftp, s.tabID)
	}
	s.a.mu.Unlock()
	return s.c.Close()
}

// sftpFor returns (and lazily opens) the SFTP client for a tab.
func (a *App) sftpFor(tabID string) (*sftp.Client, error) {
	a.mu.Lock()
	c := a.sftp[tabID]
	a.mu.Unlock()
	if c != nil {
		return c, nil
	}
	tc, err := a.terms.Conn(tabID)
	if err != nil {
		return nil, err
	}
	sc, ok := tc.(*conn.SSHConn)
	if !ok {
		return nil, errNoSFTP
	}
	c, err = sftp.NewClient(sc.Client(),
		sftp.UseConcurrentWrites(true),
		sftp.UseConcurrentReads(true),
		sftp.MaxConcurrentRequestsPerFile(sftpConcurrency),
	)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	if existing := a.sftp[tabID]; existing != nil { // lost a race
		a.mu.Unlock()
		c.Close()
		return existing, nil
	}
	a.sftp[tabID] = c
	a.mu.Unlock()
	if err := a.terms.CloseWith(tabID, sftpCloser{a: a, tabID: tabID, c: c}); err != nil {
		sftpCloser{a: a, tabID: tabID, c: c}.Close()
		return nil, err
	}
	return c, nil
}

// SftpList lists a remote directory ("" = home).
func (a *App) SftpList(tabID, dir string) (transfer.Listing, error) {
	c, err := a.sftpFor(tabID)
	if err != nil {
		return transfer.Listing{}, err
	}
	return transfer.List(c, dir)
}

// SftpMkdir creates a directory inside dir.
func (a *App) SftpMkdir(tabID, dir, name string) error {
	c, err := a.sftpFor(tabID)
	if err != nil {
		return err
	}
	return transfer.Mkdir(c, path.Join(dir, name))
}

// SftpRename renames a remote entry within the same directory or to a full path.
func (a *App) SftpRename(tabID, from, to string) error {
	c, err := a.sftpFor(tabID)
	if err != nil {
		return err
	}
	return transfer.Rename(c, from, to)
}

// SftpRemove deletes remote files/directories recursively.
func (a *App) SftpRemove(tabID string, paths []string) error {
	c, err := a.sftpFor(tabID)
	if err != nil {
		return err
	}
	return transfer.Remove(c, paths)
}

// SftpUpload uploads local paths into remoteDir; returns the job id. onExist:
// overwrite, rename or skip for names that are already there.
func (a *App) SftpUpload(tabID, remoteDir string, localPaths []string, onExist string) (string, error) {
	if len(localPaths) == 0 {
		return "", nil
	}
	c, err := a.sftpFor(tabID)
	if err != nil {
		return "", err
	}
	return a.startJob(tabID, func(ctx context.Context, p transfer.Progress) error {
		return transfer.Upload(ctx, c, p, localPaths, remoteDir, onExist, a.reportProgress)
	}), nil
}

// PickUploadFiles lets the user choose local files to upload.
func (a *App) PickUploadFiles() ([]string, error) {
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{Title: "Dateien hochladen"})
}

// PickUploadFolder lets the user choose a local folder to upload recursively.
func (a *App) PickUploadFolder() ([]string, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Ordner hochladen"})
	if err != nil || dir == "" {
		return nil, err
	}
	return []string{dir}, nil
}

// SftpDownload asks for a target folder and downloads remote paths into it.
func (a *App) SftpDownload(tabID string, remotePaths []string) (string, error) {
	if len(remotePaths) == 0 {
		return "", nil
	}
	if _, err := a.sftpFor(tabID); err != nil {
		return "", err // fail before showing the folder dialog
	}
	a.mu.Lock()
	last := a.lastDown
	a.mu.Unlock()
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Zielordner für Download", DefaultDirectory: last, CanCreateDirectories: true,
	})
	if err != nil || dir == "" {
		return "", err
	}
	return a.SftpDownloadTo(tabID, remotePaths, dir)
}

// SftpDownloadTo downloads remote paths straight into localDir (the PC pane's
// folder). Existing local files are never overwritten (see transfer.Download).
func (a *App) SftpDownloadTo(tabID string, remotePaths []string, localDir string) (string, error) {
	if len(remotePaths) == 0 {
		return "", nil
	}
	if localDir == transfer.LocalRoot {
		return "", errors.New("Bitte unten einen Ordner öffnen, nicht die Laufwerksübersicht")
	}
	if fi, err := os.Stat(localDir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("Zielordner nicht gefunden: %s", localDir)
	}
	c, err := a.sftpFor(tabID)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.lastDown = localDir
	a.mu.Unlock()
	return a.startJob(tabID, func(ctx context.Context, p transfer.Progress) error {
		return transfer.Download(ctx, c, p, remotePaths, localDir, a.reportProgress)
	}), nil
}

// LocalList lists a folder on this PC ("" = drive overview).
func (a *App) LocalList(dir string) (transfer.Listing, error) { return transfer.ListLocal(dir) }

// LocalParent returns the parent of a local folder ("" above a drive root).
func (a *App) LocalParent(dir string) string { return transfer.LocalParent(dir) }

// LocalHome is the PC pane's start folder: the last download folder.
func (a *App) LocalHome() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastDown
}

// RevealLocal shows a local file or folder in Explorer.
func (a *App) RevealLocal(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return exec.Command(explorerExe(), "/select,", path).Start()
}

// CancelTransfer aborts a running job.
func (a *App) CancelTransfer(jobID string) {
	a.mu.Lock()
	cancel := a.jobs[jobID]
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) startJob(tabID string, run func(context.Context, transfer.Progress) error) string {
	id := fsutil.NewID()
	ctx, cancel := context.WithCancel(a.ctx)
	a.mu.Lock()
	a.jobs[id] = cancel
	a.mu.Unlock()
	go func() {
		defer func() {
			if r := recover(); r != nil { // a hostile server must not take the app down
				a.reportProgress(transfer.Progress{JobID: id, TabID: tabID, State: transfer.StateError, Error: fmt.Sprint("interner Fehler: ", r)})
			}
			a.mu.Lock()
			delete(a.jobs, id)
			a.mu.Unlock()
			cancel()
		}()
		_ = run(ctx, transfer.Progress{JobID: id, TabID: tabID}) // errors are reported via progress
	}()
	return id
}

func (a *App) reportProgress(p transfer.Progress) { a.emit("sftp:progress", p) }
