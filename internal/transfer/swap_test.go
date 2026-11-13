package transfer

import (
	"errors"
	"os"
	"testing"
)

// plainServer is an SFTP server without posix-rename: Rename refuses an
// existing target. failMove makes the move of that one source fail.
type plainServer struct {
	files    map[string]string
	failMove string
}

func (s *plainServer) PosixRename(_, _ string) error { return errors.New("unsupported") }

func (s *plainServer) Rename(from, to string) error {
	if _, taken := s.files[to]; taken || from == s.failMove {
		return errors.New("rename failed")
	}
	content, ok := s.files[from]
	if !ok {
		return os.ErrNotExist
	}
	delete(s.files, from)
	s.files[to] = content
	return nil
}

func (s *plainServer) Lstat(p string) (os.FileInfo, error) {
	if _, ok := s.files[p]; !ok {
		return nil, os.ErrNotExist
	}
	return nil, nil
}

func (s *plainServer) Remove(p string) error {
	delete(s.files, p)
	return nil
}

func TestSwapRemoteReplacesExistingFile(t *testing.T) {
	s := &plainServer{files: map[string]string{"/cfg": "old", "/cfg.part": "new", "/cfg" + remoteBackupSuffix: "stale"}}

	err := swapRemote(s, "/cfg.part", "/cfg")

	if err != nil {
		t.Fatal(err)
	}
	if len(s.files) != 1 || s.files["/cfg"] != "new" {
		t.Errorf("files after swap: %v", s.files)
	}
}

func TestSwapRemoteKeepsOldFileWhenNewOneCannotMove(t *testing.T) {
	s := &plainServer{files: map[string]string{"/cfg": "old", "/cfg.part": "new"}, failMove: "/cfg.part"}

	err := swapRemote(s, "/cfg.part", "/cfg")

	if err == nil {
		t.Fatal("swap reported success")
	}
	if s.files["/cfg"] != "old" {
		t.Errorf("old file lost: %v", s.files)
	}
}

func TestSwapRemoteWithoutExistingTarget(t *testing.T) {
	s := &plainServer{files: map[string]string{"/cfg.part": "new"}}

	if err := swapRemote(s, "/cfg.part", "/cfg"); err != nil {
		t.Fatal(err)
	}
	if s.files["/cfg"] != "new" {
		t.Errorf("files after swap: %v", s.files)
	}
}
