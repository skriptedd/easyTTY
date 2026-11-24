//go:build !windows

package conn

import "errors"

// LocalConn is only available on Windows.
type LocalConn struct{}

// OpenLocal needs Windows' pseudo console.
func OpenLocal(LocalConfig) (*LocalConn, error) {
	return nil, errors.New("lokale Shells gibt es nur unter Windows")
}

func (*LocalConn) Read([]byte) (int, error)  { return 0, errors.New("closed") }
func (*LocalConn) Write([]byte) (int, error) { return 0, errors.New("closed") }
func (*LocalConn) Resize(int, int) error     { return nil }
func (*LocalConn) Close() error              { return nil }
