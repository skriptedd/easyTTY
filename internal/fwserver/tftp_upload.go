package fwserver

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Uploads: with an upload folder set, devices may write files to the server,
// mainly "copy running-config tftp://<pc>/sw1-confg". Every upload gets its
// own file (never overwritten), the folder is not served for reading, and a
// file may be at most maxUpload big.

const (
	maxUpload     = 64 << 20
	maxUploadName = 120
)

var errUploadTooBig = fmt.Errorf("größer als %d MB", maxUpload>>20)

// uploadName turns the name a device wants to write into a harmless file
// name: only the last path part, no hidden files, no device names.
func uploadName(name string) (string, error) {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune("._-()+", r):
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	clean := strings.TrimRight(b.String(), ". ")
	if clean == "" || strings.HasPrefix(clean, ".") || reservedName(clean) {
		return "", ErrOutside
	}
	if len(clean) > maxUploadName {
		clean = clean[:maxUploadName]
	}
	// Anyone in the network may upload: a file must never arrive as something
	// a double click would run (.bat, .lnk, .exe …).
	if !plainUploadExt[strings.ToLower(filepath.Ext(clean))] {
		clean += ".txt"
	}
	return clean, nil
}

// plainUploadExt are the endings an upload may keep: configs, text, images.
var plainUploadExt = map[string]bool{
	"": true, ".cfg": true, ".conf": true, ".config": true, ".txt": true, ".log": true,
	".bak": true, ".bin": true, ".tar": true, ".tgz": true, ".gz": true,
}

// freeUploadPath returns dir/name, or "name (1).ext" … if that exists.
func freeUploadPath(dir, name string) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	p := filepath.Join(dir, name)
	for i := 1; ; i++ {
		if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
}

// fromNetascii undoes TFTP's netascii line endings (CR LF → LF, CR NUL → CR).
func fromNetascii(b []byte) []byte {
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(b, []byte("\r\x00"), []byte("\r"))
}

// receive handles a write request from peer.
func (s *tftpServer) receive(name, mode string, opts map[string]string, peer *net.UDPAddr) {
	ev := Event{Proto: "TFTP", Client: peer.IP.String(), File: name, Upload: true}
	clean, err := uploadName(name)
	if err != nil {
		s.replyError(peer, 2, "easyTTY: Dateiname nicht erlaubt")
		ev.State, ev.Detail = "abgelehnt", "Upload mit ungültigem Namen"
		s.log(ev)
		return
	}
	ev.File = clean
	if v, ok := opts["tsize"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > maxUpload {
			s.replyError(peer, 3, "easyTTY: Datei zu groß")
			ev.State, ev.Detail = "abgelehnt", "Upload "+errUploadTooBig.Error()
			s.log(ev)
			return
		}
	}
	ok, dup := s.claim(peer)
	if dup {
		return
	}
	if !ok {
		s.replyError(peer, 0, "easyTTY: Server ausgelastet, gleich nochmal versuchen")
		return
	}
	defer s.release(peer)
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: s.ip})
	if err != nil {
		ev.State, ev.Detail = "fehler", "kein UDP-Port frei: "+err.Error()
		s.log(ev)
		return
	}
	defer c.Close()
	if !s.setConn(peer, c) {
		return
	}
	tmp, err := os.CreateTemp(s.uploadDir, ".easytty-upload-*.part")
	if err != nil {
		s.replyError(peer, 2, "easyTTY: Upload-Ordner nicht beschreibbar")
		ev.State, ev.Detail = "fehler", reason(err)
		s.log(ev)
		return
	}
	defer os.Remove(tmp.Name()) // gone after the rename; cleans up after an error
	ev.State = "läuft"
	s.log(ev)
	start := time.Now()
	got, err := s.receiveData(c, tmp, opts, peer, func(n int64) { ev.Done = n; s.log(ev) })
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil && mode == "netascii" {
		err = convertNetascii(tmp.Name())
	}
	if err != nil {
		ev.State, ev.Done, ev.Detail = "fehler", got, reason(err)
		s.log(ev)
		return
	}
	final, err := s.place(tmp.Name(), clean)
	if err != nil {
		ev.State, ev.Detail = "fehler", reason(err)
		s.log(ev)
		return
	}
	// File stays the requested name (it identifies the transfer); Detail names the stored file.
	ev.Done, ev.State = got, "fertig"
	ev.Detail = fmt.Sprintf("gespeichert als %s, %.1f s", filepath.Base(final), time.Since(start).Seconds())
	s.log(ev)
}

// place moves a finished upload to a free name. One at a time, so two
// uploads of the same name can never pick the same file.
func (s *tftpServer) place(tmp, name string) (string, error) {
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	final := freeUploadPath(s.uploadDir, name)
	return final, os.Rename(tmp, final)
}

func convertNetascii(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, fromNetascii(raw), 0o600)
}

// receiveData answers the request, then takes DATA blocks and acknowledges
// each one until a short block ends the file. progress gets the byte count
// about once a second.
func (s *tftpServer) receiveData(c *net.UDPConn, w *os.File, opts map[string]string, peer *net.UDPAddr, progress func(int64)) (int64, error) {
	block, timeout := defaultBlock, tftpTimeout
	var oack []string
	if v, ok := opts["blksize"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 8 {
			block = min(n, maxBlock)
			oack = append(oack, "blksize", strconv.Itoa(block))
		}
	}
	if v, ok := opts["timeout"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= maxTimeout {
			timeout = time.Duration(n) * time.Second
			oack = append(oack, "timeout", v)
		}
	}
	if v, ok := opts["tsize"]; ok {
		oack = append(oack, "tsize", v)
	}
	reply := ackPacket(0)
	if len(oack) > 0 {
		reply = []byte{0, opOACK}
		for _, o := range oack {
			reply = append(append(reply, o...), 0)
		}
	}
	if _, err := c.WriteToUDP(reply, peer); err != nil {
		return 0, err
	}
	buf := make([]byte, 4+block+1)
	expect := uint16(1)
	var got int64
	lastReport := time.Now()
	for retries := 0; ; {
		_ = c.SetReadDeadline(time.Now().Add(timeout))
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			var ne net.Error
			if !errors.As(err, &ne) || !ne.Timeout() {
				return got, err
			}
			if retries++; retries > tftpRetries {
				return got, errors.New("Zeitüberschreitung, das Gerät sendet nicht weiter")
			}
			if _, err := c.WriteToUDP(reply, peer); err != nil {
				return got, err
			}
			continue
		}
		if !from.IP.Equal(peer.IP) || from.Port != peer.Port || n < 4 {
			continue
		}
		switch binary.BigEndian.Uint16(buf) {
		case opERROR:
			return got, deviceError(strings.TrimRight(string(buf[4:n]), "\x00"))
		case opDATA:
		default:
			continue
		}
		num := binary.BigEndian.Uint16(buf[2:])
		if num != expect {
			if num == expect-1 { // our ACK got lost: say it again
				_, _ = c.WriteToUDP(reply, peer)
			}
			continue
		}
		data := buf[4:n]
		if got+int64(len(data)) > maxUpload {
			s.replyErrorOn(c, peer, 3, "easyTTY: Datei zu groß")
			return got, errUploadTooBig
		}
		if _, err := w.Write(data); err != nil {
			s.replyErrorOn(c, peer, 3, "easyTTY: Schreiben fehlgeschlagen")
			return got, err
		}
		got += int64(len(data))
		reply = ackPacket(num)
		if _, err := c.WriteToUDP(reply, peer); err != nil {
			return got, err
		}
		retries = 0
		expect++
		if len(data) < block {
			return got, nil
		}
		if time.Since(lastReport) > time.Second {
			lastReport = time.Now()
			progress(got)
		}
	}
}

func ackPacket(num uint16) []byte {
	p := make([]byte, 4)
	binary.BigEndian.PutUint16(p, opACK)
	binary.BigEndian.PutUint16(p[2:], num)
	return p
}

// replyErrorOn sends an ERROR packet from the transfer socket.
func (s *tftpServer) replyErrorOn(c *net.UDPConn, peer *net.UDPAddr, code uint16, msg string) {
	p := make([]byte, 4, 5+len(msg))
	binary.BigEndian.PutUint16(p, opERROR)
	binary.BigEndian.PutUint16(p[2:], code)
	p = append(append(p, msg...), 0)
	_, _ = c.WriteToUDP(p, peer)
}
