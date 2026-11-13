//go:build !windows

package conn

import (
	"errors"
	"net"
	"os"
)

// DialAgent connects to the agent named by SSH_AUTH_SOCK.
func DialAgent() (net.Conn, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, errors.New("SSH_AUTH_SOCK nicht gesetzt")
	}
	return net.Dial("unix", sock)
}
