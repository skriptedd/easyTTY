package fwserver

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tftpPut writes data like a switch does with "copy running-config tftp:".
func tftpPut(t *testing.T, port int, name, mode string, data []byte, opts ...string) error {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	req := []byte{0, opWRQ}
	for _, f := range append([]string{name, mode}, opts...) {
		req = append(append(req, f...), 0)
	}
	if _, err := c.WriteToUDP(req, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}); err != nil {
		t.Fatal(err)
	}
	block := defaultBlock
	buf := make([]byte, 2048)
	var peer *net.UDPAddr
	read := func() (uint16, []byte, error) {
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			return 0, nil, err
		}
		peer = from
		return binary.BigEndian.Uint16(buf), buf[2:n], nil
	}
	op, body, err := read()
	if err != nil {
		return err
	}
	switch op {
	case opERROR:
		return fmt.Errorf("tftp error: %s", strings.TrimRight(string(body[2:]), "\x00"))
	case opOACK:
		f := bytes.Split(bytes.TrimRight(body, "\x00"), []byte{0})
		for i := 0; i+1 < len(f); i += 2 {
			if string(f[i]) == "blksize" {
				fmt.Sscan(string(f[i+1]), &block)
			}
		}
	}
	for num, off := uint16(1), 0; ; num++ {
		end := min(off+block, len(data))
		pkt := []byte{0, opDATA, 0, 0}
		binary.BigEndian.PutUint16(pkt[2:], num)
		pkt = append(pkt, data[off:end]...)
		if _, err := c.WriteToUDP(pkt, peer); err != nil {
			return err
		}
		op, body, err := read()
		if err != nil {
			return err
		}
		if op == opERROR {
			return fmt.Errorf("tftp error: %s", strings.TrimRight(string(body[2:]), "\x00"))
		}
		if op != opACK || binary.BigEndian.Uint16(body) != num {
			return fmt.Errorf("unexpected reply %d %v", op, body)
		}
		if end-off < block {
			return nil
		}
		off = end
	}
}

// readUpload waits until the server has stored name (it renames the file
// right after the last acknowledgement).
func readUpload(t *testing.T, dir, name string) string {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			return string(b)
		}
	}
	t.Fatalf("%s not stored", name)
	return ""
}

func startUploads(t *testing.T) (port int, served, uploads string, log *eventLog) {
	t.Helper()
	served, _ = firmwareDir(t)
	uploads = t.TempDir()
	log = &eventLog{}
	s, err := startTFTP("127.0.0.1:0", served, uploads, log.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s.conn.LocalAddr().(*net.UDPAddr).Port, served, uploads, log
}

func TestTFTPUploadStoresRunningConfig(t *testing.T) {
	port, _, uploads, log := startUploads(t)
	cfg := bytes.Repeat([]byte("interface Gi1/0/1\n description Uplink\n!\n"), 200) // several blocks

	err := tftpPut(t, port, "sw1-confg", "octet", cfg, "blksize", "1024", "tsize", fmt.Sprint(len(cfg)))

	if err != nil {
		t.Fatal(err)
	}
	e := log.wait(t, "TFTP", "fertig")
	if got := readUpload(t, uploads, "sw1-confg"); got != string(cfg) {
		t.Fatalf("stored %d bytes, want %d", len(got), len(cfg))
	}
	if !e.Upload || e.Done != int64(len(cfg)) {
		t.Errorf("event %+v", e)
	}
	if left, _ := filepath.Glob(filepath.Join(uploads, ".easytty-upload-*")); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}

func TestTFTPUploadNeverOverwrites(t *testing.T) {
	port, _, uploads, _ := startUploads(t)

	if err := tftpPut(t, port, "sw1-confg", "octet", []byte("erste")); err != nil {
		t.Fatal(err)
	}
	first := readUpload(t, uploads, "sw1-confg")
	if err := tftpPut(t, port, "sw1-confg", "octet", []byte("zweite")); err != nil {
		t.Fatal(err)
	}
	second := readUpload(t, uploads, "sw1-confg (1)")

	if first != "erste" || second != "zweite" {
		t.Errorf("files %q and %q", first, second)
	}
}

func TestTFTPUploadNameStaysInFolder(t *testing.T) {
	port, served, uploads, _ := startUploads(t)

	if err := tftpPut(t, port, `../../evil\..\boot.ini`, "octet", []byte("x")); err != nil {
		t.Fatal(err)
	}

	readUpload(t, uploads, "boot.ini.txt")
	if _, err := os.Stat(filepath.Join(filepath.Dir(served), "boot.ini.txt")); err == nil {
		t.Error("upload left the upload folder")
	}
	for _, bad := range []string{".ssh", "CON", "..", ""} {
		if err := tftpPut(t, port, bad, "octet", []byte("x")); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
}

func TestTFTPUploadNetascii(t *testing.T) {
	port, _, uploads, _ := startUploads(t)

	if err := tftpPut(t, port, "r1.cfg", "netascii", []byte("hostname r1\r\n!\r\n")); err != nil {
		t.Fatal(err)
	}

	if got := readUpload(t, uploads, "r1.cfg"); got != "hostname r1\n!\n" {
		t.Errorf("stored %q", got)
	}
}

func TestTFTPUploadRefusesTooBig(t *testing.T) {
	port, _, _, _ := startUploads(t)

	err := tftpPut(t, port, "big.bin", "octet", []byte("x"), "tsize", fmt.Sprint(maxUpload+1))

	if err == nil || !strings.Contains(err.Error(), "zu groß") {
		t.Errorf("err = %v", err)
	}
}

func TestStartChecksUploadFolder(t *testing.T) {
	dir, _ := firmwareDir(t)
	inside := filepath.Join(dir, "configs")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := map[string]Config{
		"inside served": {Dir: dir, TFTPPort: freePort(t, "udp"), UploadDir: inside},
		"missing":       {Dir: dir, TFTPPort: freePort(t, "udp"), UploadDir: filepath.Join(t.TempDir(), "nope")},
		"no tftp":       {Dir: dir, HTTPPort: 1, UploadDir: t.TempDir()},
	}
	for name, cfg := range cases {
		s := New(func(Event) {})
		if _, err := s.Start(cfg); err == nil {
			s.Stop()
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestUploadNameNeverKeepsRunnableEndings(t *testing.T) {
	for in, want := range map[string]string{
		"sw1-confg":        "sw1-confg",
		"sw1.cfg":          "sw1.cfg",
		"core.CFG":         "core.CFG",
		"run.bat":          "run.bat.txt",
		"evil.lnk":         "evil.lnk.txt",
		"setup.exe":        "setup.exe.txt",
		"a.b.ps1":          "a.b.ps1.txt",
		"10.0.0.1-confg":   "10.0.0.1-confg.txt",
		`dir\sub/sw2.conf`: "sw2.conf",
	} {
		got, err := uploadName(in)
		if err != nil || got != want {
			t.Errorf("uploadName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}
