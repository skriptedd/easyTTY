package fwserver

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func exitStatus(ch ssh.Channel, code uint32) {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, code)
	_, _ = ch.SendRequest("exit-status", false, b)
}

func serveSession(ch ssh.Channel, reqs <-chan *ssh.Request, root, client string, log func(Event)) {
	defer ch.Close()
	for req := range reqs {
		switch req.Type {
		case "pty-req", "env":
			req.Reply(true, nil)
		case "exec":
			cmd := ""
			if len(req.Payload) > 4 {
				cmd = string(req.Payload[4:])
			}
			name, ok := scpSourcePath(cmd)
			if !ok {
				req.Reply(true, nil)
				fmt.Fprintf(ch.Stderr(), "easyTTY-Firmwareserver: nur \"scp -f <datei>\" (Download) erlaubt\n")
				exitStatus(ch, 1)
				log(Event{Proto: "SCP", Client: client, State: "abgelehnt", Detail: cmd})
				return
			}
			req.Reply(true, nil)
			code := uint32(0)
			if err := scpSend(ch, root, name, client, log); err != nil {
				code = 1
			}
			exitStatus(ch, code)
			return
		case "subsystem":
			if len(req.Payload) > 4 && string(req.Payload[4:]) == "sftp" {
				req.Reply(true, nil)
				h := roHandler{root: root, client: client, log: log}
				srv := sftp.NewRequestServer(ch, sftp.Handlers{FileGet: h, FilePut: h, FileCmd: h, FileList: h})
				_ = srv.Serve()
				_ = srv.Close()
				return
			}
			req.Reply(false, nil)
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

// scpSourcePath accepts "scp -f file" with optional -v/-p/-d/-q and "--".
func scpSourcePath(cmd string) (string, bool) {
	f := strings.Fields(cmd)
	if len(f) < 3 || f[0] != "scp" {
		return "", false
	}
	source := false
	i := 1
	for ; i < len(f) && strings.HasPrefix(f[i], "-"); i++ {
		if f[i] == "--" {
			i++
			break
		}
		for _, c := range f[i][1:] {
			switch c {
			case 'f':
				source = true
			case 'v', 'p', 'd', 'q':
			default:
				return "", false // -t (upload), -r … not allowed
			}
		}
	}
	if !source || i != len(f)-1 {
		return "", false
	}
	return strings.Trim(f[i], `"'`), true
}

// scpSend implements the source side of the SCP protocol for one file.
func scpSend(ch ssh.Channel, root, name, client string, log func(Event)) error {
	r := bufio.NewReader(ch)
	ack := func() error {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		if b != 0 {
			msg, _ := r.ReadString('\n')
			return fmt.Errorf("Gerät meldet: %s", strings.TrimSpace(msg))
		}
		return nil
	}
	ev := Event{Proto: "SCP", Client: client, File: name, State: "fehler"}
	if err := ack(); err != nil { // sink signals "ready"
		ev.Detail = err.Error()
		log(ev)
		return err
	}
	f, fi, err := openFile(root, name)
	if err != nil {
		fmt.Fprintf(ch, "\x01scp: %s: %s\n", name, reason(err))
		ev.Detail = reason(err)
		log(ev)
		return err
	}
	defer f.Close()
	ev.Total, ev.State = fi.Size(), "läuft"
	log(ev)
	if _, err := fmt.Fprintf(ch, "C0644 %d %s\n", fi.Size(), filepath.Base(fi.Name())); err != nil {
		return err
	}
	if err := ack(); err != nil {
		ev.State, ev.Detail = "fehler", err.Error()
		log(ev)
		return err
	}
	n, err := io.Copy(progressWriter{w: ch, p: newProgress(ev, log)}, f)
	ev.Done = n
	if err == nil {
		_, err = ch.Write([]byte{0})
	}
	if err == nil {
		err = ack()
	}
	if err != nil {
		ev.State, ev.Detail = "fehler", err.Error()
		log(ev)
		return err
	}
	ev.State = "fertig"
	log(ev)
	return nil
}

/* ---------- read-only, jailed SFTP ---------- */

type roHandler struct {
	root   string
	client string
	log    func(Event)
}

func (h roHandler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	f, fi, err := openFile(h.root, r.Filepath)
	if err != nil {
		h.log(Event{Proto: "SFTP", Client: h.client, File: r.Filepath, State: "fehler", Detail: reason(err)})
		return nil, sftp.ErrSSHFxNoSuchFile
	}
	ev := Event{Proto: "SFTP", Client: h.client, File: r.Filepath, Total: fi.Size(), State: "läuft"}
	h.log(ev)
	return &loggedFile{File: f, ev: ev, log: h.log, prog: newProgress(ev, h.log)}, nil
}

func (h roHandler) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	h.log(Event{Proto: "SFTP", Client: h.client, File: r.Filepath, State: "abgelehnt", Detail: "Schreibversuch"})
	return nil, sftp.ErrSSHFxPermissionDenied
}

func (h roHandler) Filecmd(r *sftp.Request) error {
	h.log(Event{Proto: "SFTP", Client: h.client, File: r.Filepath, State: "abgelehnt", Detail: r.Method})
	return sftp.ErrSSHFxPermissionDenied
}

func (h roHandler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	p, err := resolve(h.root, r.Filepath)
	if err != nil {
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	switch r.Method {
	case "List":
		des, err := os.ReadDir(p)
		if err != nil {
			return nil, sftp.ErrSSHFxNoSuchFile
		}
		out := make(listerAt, 0, len(des))
		for _, d := range des {
			// Only names that Get would hand out (no dot folders, no links leading outside).
			if _, err := resolve(h.root, path.Join(r.Filepath, d.Name())); err != nil {
				continue
			}
			if fi, err := d.Info(); err == nil {
				out = append(out, fi)
			}
		}
		return out, nil
	case "Stat":
		fi, err := os.Stat(p)
		if err != nil {
			return nil, sftp.ErrSSHFxNoSuchFile
		}
		return listerAt{fi}, nil
	}
	return nil, sftp.ErrSSHFxOpUnsupported
}

type listerAt []os.FileInfo

func (l listerAt) ListAt(out []os.FileInfo, off int64) (int, error) {
	if off >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(out, l[off:])
	if n < len(out) {
		return n, io.EOF
	}
	return n, nil
}

// loggedFile reports "fertig" with the bytes served when the client closes it.
type loggedFile struct {
	*os.File
	ev   Event
	log  func(Event)
	prog *progress
}

func (l *loggedFile) ReadAt(p []byte, off int64) (int, error) {
	n, err := l.File.ReadAt(p, off)
	l.prog.add(int64(n))
	return n, err
}

func (l *loggedFile) Close() error {
	l.ev.Done = l.prog.done()
	l.ev.State = endState(l.ev.Done, l.ev.Total)
	l.log(l.ev)
	return l.File.Close()
}
