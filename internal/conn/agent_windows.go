//go:build windows

package conn

import (
	"net"
	"os"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
)

const openSSHAgentPipe = `\\.\pipe\openssh-ssh-agent`

// DialAgent connects to the Windows OpenSSH agent (or SSH_AUTH_SOCK if it names a pipe).
func DialAgent() (net.Conn, error) {
	pipe := openSSHAgentPipe
	if sock := os.Getenv("SSH_AUTH_SOCK"); strings.HasPrefix(sock, `\\.\pipe\`) {
		pipe = sock
	}
	timeout := 2 * time.Second
	return winio.DialPipe(pipe, &timeout)
}
