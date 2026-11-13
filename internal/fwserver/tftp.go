package fwserver

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Read-only TFTP server (RFC 1350) with the blksize/tsize/timeout options
// (RFC 2347–2349): bigger blocks make firmware copies much faster.

const (
	opRRQ   = 1
	opWRQ   = 2
	opDATA  = 3
	opACK   = 4
	opERROR = 5
	opOACK  = 6

	defaultBlock = 512
	// maxBlock is the largest block Cisco devices ask for (ip tftp blocksize
	// 8192). The 64 KB allowed by the RFC would turn a spoofed request into a
	// much larger reflected reply.
	maxBlock     = 8192
	tftpRetries  = 5
	maxTransfers = 16 // concurrent downloads in total
	maxPerDevice = 2  // per device IP: IOS asks for the size, aborts and asks again at once
	maxTimeout   = 5  // seconds; larger client "timeout" options are ignored

	// Requests per device IP and window; more are dropped without a reply or a
	// log entry (spoofed floods must neither bounce nor bury the real events).
	maxRequests    = 30
	requestWindow  = 10 * time.Second
	maxTrackedPeer = 1024
)

var tftpTimeout = 2 * time.Second

// errNoAck: the device stopped acknowledging.
var errNoAck = errors.New("Zeitüberschreitung, keine Bestätigung vom Gerät")

// deviceError is an ERROR packet sent by the device.
type deviceError string

func (e deviceError) Error() string { return "Gerät hat abgebrochen: " + string(e) }

type tftpServer struct {
	conn *net.UDPConn
	root string
	// uploadDir takes files devices write ("" = uploads off).
	uploadDir string
	uploadMu  sync.Mutex // serialises picking the final name of uploads
	log       func(Event)
	ip        net.IP // local address transfers answer from

	mu     sync.Mutex
	active map[string]*net.UDPConn // peer ip:port (the client's transfer ID) -> socket
	closed bool
	seen   map[string]window // requests per peer IP
	now    func() time.Time
}

type window struct {
	start time.Time
	n     int
}

// allow counts a request from ip and reports whether it may be answered.
func (s *tftpServer) allow(ip net.IP) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	key := ip.String()
	w, ok := s.seen[key]
	if !ok || now.Sub(w.start) >= requestWindow {
		if len(s.seen) >= maxTrackedPeer {
			s.seen = map[string]window{} // forget everyone rather than grow without end
		}
		w = window{start: now}
	}
	w.n++
	s.seen[key] = w
	return w.n <= maxRequests
}

func startTFTP(addr, root, uploadDir string, log func(Event)) (*tftpServer, error) {
	ua, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return nil, err
	}
	c, err := net.ListenUDP("udp4", ua)
	if err != nil {
		return nil, fmt.Errorf("TFTP %s: %w", addr, err)
	}
	s := &tftpServer{conn: c, root: root, uploadDir: uploadDir, log: log, ip: ua.IP, active: map[string]*net.UDPConn{}, seen: map[string]window{}, now: time.Now}
	go s.serve()
	return s, nil
}

// Close stops the listener and aborts running transfers.
func (s *tftpServer) Close() error {
	s.mu.Lock()
	s.closed = true
	for _, c := range s.active {
		if c != nil {
			c.Close()
		}
	}
	s.mu.Unlock()
	return s.conn.Close()
}

// claim reserves a transfer slot for peer before anything is opened.
// dup = this exact client (ip:port) already has a transfer (a retransmitted
// RRQ, to be ignored silently).
func (s *tftpServer) claim(peer *net.UDPAddr) (ok, dup bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.active[peer.String()]; exists {
		return false, true
	}
	if s.closed || len(s.active) >= maxTransfers {
		return false, false
	}
	same := 0
	for k := range s.active {
		if host, _, _ := net.SplitHostPort(k); host == peer.IP.String() {
			same++
		}
	}
	if same >= maxPerDevice {
		return false, false
	}
	s.active[peer.String()] = nil
	return true, false
}

// setConn attaches the transfer socket to a claimed slot (Close aborts it).
func (s *tftpServer) setConn(peer *net.UDPAddr, c *net.UDPConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.active[peer.String()] = c
	return true
}

func (s *tftpServer) release(peer *net.UDPAddr) {
	s.mu.Lock()
	delete(s.active, peer.String())
	s.mu.Unlock()
}

func (s *tftpServer) serve() {
	buf := make([]byte, 2048)
	for {
		n, peer, err := s.conn.ReadFromUDP(buf)
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			continue // e.g. ICMP "unreachable" from a client that left: keep serving
		}
		pkt := append([]byte(nil), buf[:n]...)
		go s.handle(pkt, peer)
	}
}

func (s *tftpServer) handle(pkt []byte, peer *net.UDPAddr) {
	if len(pkt) < 4 {
		return
	}
	if !s.allow(peer.IP) {
		return
	}
	op := binary.BigEndian.Uint16(pkt)
	if op == opWRQ && s.uploadDir == "" {
		s.replyError(peer, 2, "easyTTY: nur Lesen (Uploads sind aus)")
		s.log(Event{Proto: "TFTP", Client: peer.IP.String(), State: "abgelehnt", Detail: "Upload, aber Uploads sind ausgeschaltet"})
		return
	}
	if op != opRRQ && op != opWRQ {
		return
	}
	fields := bytes.Split(pkt[2:], []byte{0})
	if len(fields) < 2 {
		return
	}
	name, mode := string(fields[0]), strings.ToLower(string(fields[1]))
	opts := map[string]string{}
	for i := 2; i+1 < len(fields); i += 2 {
		opts[strings.ToLower(string(fields[i]))] = string(fields[i+1])
	}
	if mode != "octet" && mode != "netascii" {
		s.replyError(peer, 4, "nur octet")
		s.log(Event{Proto: "TFTP", Client: peer.IP.String(), File: name, State: "abgelehnt", Detail: "Modus " + mode + " nicht unterstützt"})
		return
	}
	if op == opWRQ {
		s.receive(name, mode, opts, peer)
		return
	}
	ok, dup := s.claim(peer)
	if dup {
		return // retransmitted request of a running transfer
	}
	if !ok {
		s.replyError(peer, 0, "easyTTY: Server ausgelastet, gleich nochmal versuchen")
		return
	}
	f, fi, err := openFile(s.root, name)
	if err != nil {
		s.release(peer)
		code := uint16(1)
		if errors.Is(err, ErrOutside) {
			code = 2
		}
		s.replyError(peer, code, "Datei nicht gefunden")
		s.log(Event{Proto: "TFTP", Client: peer.IP.String(), File: name, State: "fehler", Detail: reason(err)})
		return
	}
	defer f.Close()
	s.transfer(f, fi, name, opts, peer)
}

func (s *tftpServer) replyError(peer *net.UDPAddr, code uint16, msg string) {
	p := make([]byte, 4, 5+len(msg))
	binary.BigEndian.PutUint16(p, opERROR)
	binary.BigEndian.PutUint16(p[2:], code)
	p = append(append(p, msg...), 0)
	_, _ = s.conn.WriteToUDP(p, peer)
}

// transfer sends the file from a fresh port (the transfer ID) to peer.
func (s *tftpServer) transfer(f *os.File, fi os.FileInfo, name string, opts map[string]string, peer *net.UDPAddr) {
	released := false
	release := func() {
		if !released {
			released = true
			s.release(peer)
		}
	}
	defer release()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: s.ip})
	if err != nil {
		s.log(Event{Proto: "TFTP", Client: peer.IP.String(), File: name, State: "fehler", Detail: "kein UDP-Port frei: " + err.Error()})
		return
	}
	defer c.Close()
	if !s.setConn(peer, c) {
		return // server stopping
	}

	block := defaultBlock
	timeout := tftpTimeout
	var oack []string
	if v, ok := opts["blksize"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 8 {
			block = min(n, maxBlock)
			oack = append(oack, "blksize", strconv.Itoa(block))
		}
	}
	if _, ok := opts["tsize"]; ok {
		oack = append(oack, "tsize", strconv.FormatInt(fi.Size(), 10))
	}
	if v, ok := opts["timeout"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= maxTimeout {
			timeout = time.Duration(n) * time.Second
			oack = append(oack, "timeout", v)
		}
	}
	ev := Event{Proto: "TFTP", Client: peer.IP.String(), File: name, Total: fi.Size(), State: "läuft"}
	s.log(ev)
	start := time.Now()

	send := func(pkt []byte, expectBlock uint16) error {
		ack := make([]byte, 1024)
		for try := 0; try < tftpRetries; try++ {
			if _, err := c.WriteToUDP(pkt, peer); err != nil {
				return err
			}
			deadline := time.Now().Add(timeout)
			for {
				_ = c.SetReadDeadline(deadline)
				n, from, err := c.ReadFromUDP(ack)
				if err != nil {
					break // timeout: resend
				}
				if !from.IP.Equal(peer.IP) || from.Port != peer.Port || n < 4 {
					continue
				}
				switch binary.BigEndian.Uint16(ack) {
				case opACK:
					if binary.BigEndian.Uint16(ack[2:]) == expectBlock {
						return nil
					}
				case opERROR:
					msg := strings.TrimRight(string(ack[4:n]), "\x00")
					if len(msg) > 120 {
						msg = msg[:120]
					}
					return deviceError(msg)
				}
			}
		}
		return errNoAck
	}

	fail := func(err error) {
		ev.State, ev.Detail = "fehler", err.Error()
		s.log(ev)
	}
	if len(oack) > 0 {
		p := []byte{0, opOACK}
		for _, o := range oack {
			p = append(append(p, o...), 0)
		}
		if err := send(p, 0); err != nil {
			var dev deviceError
			if _, probe := opts["tsize"]; probe && errors.As(err, &dev) {
				// Cisco IOS and others ask for the size first, then abort and
				// start the real download right away.
				ev.State, ev.Detail = "info", "Gerät hat nur die Dateigröße abgefragt – der eigentliche Download folgt"
				release()
				s.log(ev)
				return
			}
			fail(err)
			return
		}
	}

	data := make([]byte, 4+block)
	binary.BigEndian.PutUint16(data, opDATA)
	var num uint16
	var sent int64
	lastReport := time.Now()
	for {
		n, err := io.ReadFull(f, data[4:])
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			fail(err)
			return
		}
		num++ // wraps at 65535 → 0 (block rollover, accepted by IOS and most clients)
		binary.BigEndian.PutUint16(data[2:], num)
		if err := send(data[:4+n], num); err != nil {
			if n < block && errors.Is(err, errNoAck) && sent+int64(n) == fi.Size() {
				// Only the ACK of the last block is missing: the device has the
				// whole file (often it just closed the transfer already).
				sent += int64(n)
				ev.Done, ev.State, ev.Detail = sent, "fertig", "letzte Bestätigung fehlt – Datei vollständig gesendet"
				release()
				s.log(ev)
				return
			}
			fail(err)
			return
		}
		sent += int64(n)
		if time.Since(lastReport) > time.Second {
			lastReport = time.Now()
			ev.Done = sent
			s.log(ev)
		}
		if n < block {
			break
		}
	}
	ev.Done, ev.State = sent, "fertig"
	ev.Detail = fmt.Sprintf("%.1f s", time.Since(start).Seconds())
	release() // before logging (MD5 may take a moment): the device may ask again at once
	s.log(ev)
}
