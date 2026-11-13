package fwserver

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type eventLog struct {
	mu sync.Mutex
	ev []Event
}

func (l *eventLog) add(e Event) {
	l.mu.Lock()
	l.ev = append(l.ev, e)
	l.mu.Unlock()
}

// wait returns the first event matching proto+state (polling briefly).
func (l *eventLog) wait(t *testing.T, proto, state string) Event {
	t.Helper()
	for i := 0; i < 100; i++ {
		l.mu.Lock()
		for _, e := range l.ev {
			if e.Proto == proto && e.State == state {
				l.mu.Unlock()
				return e
			}
		}
		l.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("kein Event %s/%s in %+v", proto, state, l.ev)
	return Event{}
}

func firmwareDir(t *testing.T) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	img := bytes.Repeat([]byte("c2960-lanbasek9-mz.152-7.E9.bin|"), 3000) // ~96 KB, several blocks
	if err := os.WriteFile(filepath.Join(dir, "ios.bin"), img, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "small.txt"), []byte("hallo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "secret.txt"), []byte("geheim"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, img
}

func freePort(t *testing.T, network string) int {
	t.Helper()
	if network == "udp" {
		c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		return c.LocalAddr().(*net.UDPAddr).Port
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func startAll(t *testing.T) (*Server, Info, *eventLog, []byte) {
	t.Helper()
	dir, img := firmwareDir(t)
	log := &eventLog{}
	s := New(log.add)
	// Free ports can be taken by another process (CI jobs share the runner)
	// between probing and binding: try again with fresh ones.
	var info Info
	var err error
	for try := 0; try < 5; try++ {
		tcp := freeTCPPorts(t, 2) // held together, so HTTP and SSH never get the same one
		info, err = s.Start(Config{Dir: dir, BindIP: "127.0.0.1", TFTPPort: freePort(t, "udp"), HTTPPort: tcp[0], SSHPort: tcp[1]})
		if err == nil || !errors.Is(err, syscall.EADDRINUSE) {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	return s, info, log, img
}

// freeTCPPorts returns n distinct free ports (all probed at the same time).
func freeTCPPorts(t *testing.T, n int) []int {
	t.Helper()
	ports := make([]int, 0, n)
	for i := 0; i < n; i++ {
		ln, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		ports = append(ports, ln.Addr().(*net.TCPAddr).Port)
	}
	return ports
}

/* ---------- paths ---------- */

func TestResolveStaysInside(t *testing.T) {
	dir, _ := firmwareDir(t)
	root, err := filepath.EvalSymlinks(dir) // resolve answers with long names, the temp dir may be 8.3 (RUNNER~1)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ios.bin", "/ios.bin", "sub/small.txt", `sub\small.txt`, "../ios.bin", "/../../ios.bin"} {
		p, err := resolve(dir, name)
		if err != nil {
			t.Errorf("%q: %v", name, err)
			continue
		}
		rel, _ := filepath.Rel(root, p)
		if strings.HasPrefix(rel, "..") {
			t.Errorf("%q → %s liegt außerhalb", name, p)
		}
	}
	for _, name := range []string{`C:\Windows\win.ini`, "ios.bin:stream", "c:/x"} {
		if _, err := resolve(dir, name); !errors.Is(err, ErrOutside) {
			t.Errorf("%q: erwartet ErrOutside, bekam %v", name, err)
		}
	}
	if _, _, err := openFile(dir, "../secret.txt"); err == nil {
		t.Error("secret.txt außerhalb sollte nicht lesbar sein")
	}
	if _, _, err := openFile(dir, "sub"); err == nil {
		t.Error("Ordner darf nicht als Datei geöffnet werden")
	}
}

func TestListFilesWithMD5(t *testing.T) {
	dir, _ := firmwareDir(t)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, ".git", "x"), []byte("x"), 0o644)
	files, err := ListFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Name != "ios.bin" || files[1].Name != "sub/small.txt" {
		t.Fatalf("files = %+v", files)
	}
	if files[1].MD5 != "598d4c200461b81522a3328565c25f7c" { // md5("hallo")
		t.Errorf("md5 = %s", files[1].MD5)
	}
}

func TestStartValidates(t *testing.T) {
	s := New(func(Event) {})
	if _, err := s.Start(Config{Dir: filepath.Join(t.TempDir(), "fehlt"), TFTPPort: 1}); err == nil {
		t.Error("fehlender Ordner sollte scheitern")
	}
	if _, err := s.Start(Config{Dir: t.TempDir()}); err == nil {
		t.Error("ohne Protokoll sollte scheitern")
	}
	if _, err := s.Start(Config{Dir: t.TempDir(), BindIP: "nope", HTTPPort: 1}); err == nil {
		t.Error("ungültige IP sollte scheitern")
	}
	if s.Info().Running {
		t.Error("darf nicht laufen")
	}
}

/* ---------- TFTP ---------- */

func tftpGet(t *testing.T, port int, name string, opts ...string) ([]byte, map[string]string, error) {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	req := []byte{0, opRRQ}
	for _, f := range append([]string{name, "octet"}, opts...) {
		req = append(append(req, f...), 0)
	}
	if _, err := c.WriteToUDP(req, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}); err != nil {
		t.Fatal(err)
	}
	block := defaultBlock
	var out bytes.Buffer
	oack := map[string]string{}
	buf := make([]byte, 70000)
	for {
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			return nil, nil, err
		}
		ack := func(num uint16) {
			a := []byte{0, opACK, 0, 0}
			binary.BigEndian.PutUint16(a[2:], num)
			_, _ = c.WriteToUDP(a, from)
		}
		switch binary.BigEndian.Uint16(buf) {
		case opERROR:
			return nil, nil, fmt.Errorf("tftp error %d: %s", binary.BigEndian.Uint16(buf[2:]), strings.TrimRight(string(buf[4:n]), "\x00"))
		case opOACK:
			f := bytes.Split(bytes.TrimRight(buf[2:n], "\x00"), []byte{0})
			for i := 0; i+1 < len(f); i += 2 {
				oack[string(f[i])] = string(f[i+1])
			}
			if v, ok := oack["blksize"]; ok {
				block, _ = strconv.Atoi(v)
			}
			ack(0)
		case opDATA:
			out.Write(buf[4:n])
			ack(binary.BigEndian.Uint16(buf[2:]))
			if n-4 < block {
				return out.Bytes(), oack, nil
			}
		}
	}
}

func TestTFTPDownload(t *testing.T) {
	_, info, log, img := startAll(t)
	got, _, err := tftpGet(t, info.Config.TFTPPort, "ios.bin")
	if err != nil || !bytes.Equal(got, img) {
		t.Fatalf("plain: err=%v len=%d want %d", err, len(got), len(img))
	}
	got, oack, err := tftpGet(t, info.Config.TFTPPort, "/ios.bin", "blksize", "8192", "tsize", "0")
	if err != nil || !bytes.Equal(got, img) {
		t.Fatalf("blksize: err=%v len=%d", err, len(got))
	}
	if oack["blksize"] != "8192" || oack["tsize"] != strconv.Itoa(len(img)) {
		t.Errorf("oack = %v", oack)
	}
	e := log.wait(t, "TFTP", "fertig")
	if e.Done != int64(len(img)) || e.Client != "127.0.0.1" {
		t.Errorf("event = %+v", e)
	}
}

func TestTFTPRejectsTraversalAndWrites(t *testing.T) {
	_, info, log, _ := startAll(t)
	if _, _, err := tftpGet(t, info.Config.TFTPPort, "../secret.txt"); err == nil || !strings.Contains(err.Error(), "nicht gefunden") {
		t.Errorf("traversal: %v", err)
	}
	if _, _, err := tftpGet(t, info.Config.TFTPPort, "fehlt.bin"); err == nil {
		t.Error("fehlende Datei sollte Fehler liefern")
	}
	c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: info.Config.TFTPPort})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = c.Write(append([]byte{0, opWRQ}, "x.bin\x00octet\x00"...))
	buf := make([]byte, 512)
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := c.Read(buf)
	if err != nil || binary.BigEndian.Uint16(buf) != opERROR {
		t.Fatalf("WRQ antwort: %v %x", err, buf[:n])
	}
	log.wait(t, "TFTP", "abgelehnt")
}

/* ---------- HTTP ---------- */

func TestHTTPDownloadReadOnly(t *testing.T) {
	_, info, log, img := startAll(t)
	base := "http://127.0.0.1:" + strconv.Itoa(info.Config.HTTPPort)
	resp, err := http.Get(base + "/ios.bin")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Equal(body, img) {
		t.Fatalf("GET: %d len=%d", resp.StatusCode, len(body))
	}
	if e := log.wait(t, "HTTP", "fertig"); e.Done != int64(len(img)) {
		t.Errorf("event = %+v", e)
	}
	resp, err = http.Get(base + "/../secret.txt")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("traversal: %d", resp.StatusCode)
	}
	resp, err = http.Post(base+"/neu.bin", "application/octet-stream", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", resp.StatusCode)
	}
}

/* ---------- SCP / SFTP ---------- */

func dialSSH(t *testing.T, info Info, pw string) (*ssh.Client, error) {
	t.Helper()
	return ssh.Dial("tcp", "127.0.0.1:"+strconv.Itoa(info.Config.SSHPort), &ssh.ClientConfig{
		User: info.User, Auth: []ssh.AuthMethod{ssh.Password(pw)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 3 * time.Second,
	})
}

// scpFetch plays the sink side ("scp -t" on the device).
func scpFetch(t *testing.T, c *ssh.Client, name string) ([]byte, string, error) {
	t.Helper()
	sess, err := c.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	in, _ := sess.StdinPipe()
	outPipe, _ := sess.StdoutPipe()
	out := bufio.NewReader(outPipe)
	if err := sess.Start("scp -f " + name); err != nil {
		t.Fatal(err)
	}
	_, _ = in.Write([]byte{0})
	hdr, err := out.ReadString('\n')
	if err != nil {
		return nil, "", err
	}
	if hdr[0] != 'C' {
		return nil, "", errors.New(strings.TrimSpace(hdr[1:]))
	}
	f := strings.Fields(hdr)
	size, _ := strconv.Atoi(f[1])
	_, _ = in.Write([]byte{0})
	data := make([]byte, size)
	if _, err := io.ReadFull(out, data); err != nil {
		return nil, "", err
	}
	if b, _ := out.ReadByte(); b != 0 {
		return nil, "", errors.New("kein Abschluss-Null")
	}
	_, _ = in.Write([]byte{0})
	in.Close()
	return data, f[2], sess.Wait()
}

func TestSCPDownload(t *testing.T) {
	_, info, log, img := startAll(t)
	if _, err := dialSSH(t, info, "falsch"); err == nil {
		t.Fatal("falsches Passwort akzeptiert")
	}
	log.wait(t, "SCP/SFTP", "abgelehnt")
	c, err := dialSSH(t, info, info.Password)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	data, base, err := scpFetch(t, c, "/ios.bin")
	if err != nil || !bytes.Equal(data, img) || base != "ios.bin" {
		t.Fatalf("scp: err=%v len=%d base=%q", err, len(data), base)
	}
	if e := log.wait(t, "SCP", "fertig"); e.Done != int64(len(img)) {
		t.Errorf("event = %+v", e)
	}
	if _, _, err := scpFetch(t, c, "../secret.txt"); err == nil {
		t.Error("traversal per scp erlaubt")
	}
	sess, _ := c.NewSession()
	err = sess.Run("scp -t /upload.bin")
	sess.Close()
	var exit *ssh.ExitError
	if !errors.As(err, &exit) || exit.ExitStatus() != 1 {
		t.Errorf("scp -t sollte abgelehnt werden: %v", err)
	}
}

func TestSFTPReadOnly(t *testing.T) {
	_, info, log, img := startAll(t)
	c, err := dialSSH(t, info, info.Password)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	sc, err := sftp.NewClient(c)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.Close()
	f, err := sc.Open("/ios.bin")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil || !bytes.Equal(data, img) {
		t.Fatalf("sftp read: err=%v len=%d", err, len(data))
	}
	log.wait(t, "SFTP", "fertig")
	entries, err := sc.ReadDir("/")
	if err != nil || len(entries) != 2 {
		t.Errorf("ReadDir: %v %d", err, len(entries))
	}
	if _, err := sc.Stat("/sub/small.txt"); err != nil {
		t.Errorf("Stat: %v", err)
	}
	if _, err := sc.Open("/../secret.txt"); err == nil {
		t.Error("traversal per sftp erlaubt")
	}
	if _, err := sc.Create("/neu.bin"); err == nil {
		t.Error("Schreiben erlaubt")
	}
	if err := sc.Remove("/ios.bin"); err == nil {
		t.Error("Löschen erlaubt")
	}
	if err := sc.Mkdir("/x"); err == nil {
		t.Error("Mkdir erlaubt")
	}
	log.wait(t, "SFTP", "abgelehnt")
}

func TestSCPSourcePath(t *testing.T) {
	cases := map[string]string{
		"scp -f flash.bin":      "flash.bin",
		"scp -v -f /a/b.bin":    "/a/b.bin",
		"scp -pf -- 'x y'":      "",
		"scp -f -- c.bin":       "c.bin",
		"scp -t x.bin":          "",
		"scp -r -f dir":         "",
		"rm -rf /":              "",
		"scp -f a.bin b.bin":    "",
		"scp -f \"quoted.bin\"": "quoted.bin",
	}
	for cmd, want := range cases {
		got, ok := scpSourcePath(cmd)
		if (want == "") == ok || got != want {
			t.Errorf("%q → %q,%v want %q", cmd, got, ok, want)
		}
	}
}

func TestStopClosesPorts(t *testing.T) {
	s, info, _, _ := startAll(t)
	s.Stop()
	if s.Info().Running {
		t.Fatal("läuft noch")
	}
	if _, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(info.Config.HTTPPort), time.Second); err == nil {
		t.Error("HTTP-Port noch offen")
	}
	// restart on the same ports must work
	if _, err := s.Start(info.Config); err != nil {
		t.Errorf("Neustart: %v", err)
	}
}

func TestLocalAddressesNoLoopback(t *testing.T) {
	for _, a := range LocalAddresses() {
		if ip := net.ParseIP(a.IP); ip == nil || ip.IsLoopback() || ip.To4() == nil {
			t.Errorf("unerwartet: %+v", a)
		}
	}
}

func TestReasonHidesPaths(t *testing.T) {
	dir, _ := firmwareDir(t)
	_, _, err := openFile(dir, "fehlt.bin")
	if got := reason(err); got != "Datei nicht gefunden" {
		t.Errorf("reason = %q", got)
	}
	_, _, err = openFile(dir, "sub")
	if got := reason(err); strings.Contains(got, dir) || got != "ist ein Ordner, keine Datei" {
		t.Errorf("reason = %q", got)
	}
}

func TestResolveRejectsDotAndDevices(t *testing.T) {
	dir, _ := firmwareDir(t)
	for _, name := range []string{".git/config", "sub/.hidden", "CON", "nul.txt", "sub/COM1", "lpt9.bin", "AUX "} {
		if _, err := resolve(dir, name); !errors.Is(err, ErrOutside) {
			t.Errorf("%q: erwartet ErrOutside, bekam %v", name, err)
		}
	}
	for _, name := range []string{"console.bin", "com10.bin", "context.txt"} {
		if reservedName(name) {
			t.Errorf("%q fälschlich als Gerät erkannt", name)
		}
	}
}

func TestStartRejectsDriveRoot(t *testing.T) {
	root := filepath.VolumeName(t.TempDir()) + string(filepath.Separator)
	if _, err := New(func(Event) {}).Start(Config{Dir: root, HTTPPort: 1}); err == nil {
		t.Errorf("Laufwerk %s sollte abgelehnt werden", root)
	}
}

func TestStopDropsOpenSSHSessions(t *testing.T) {
	s, info, _, _ := startAll(t)
	c, err := dialSSH(t, info, info.Password)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	sc, err := sftp.NewClient(c)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.Close()
	s.Stop()
	time.Sleep(100 * time.Millisecond)
	if _, err := sc.Stat("/ios.bin"); err == nil {
		t.Error("SFTP-Sitzung lebt nach Stop weiter")
	}
}

func TestTFTPOnePerPeerAndTimeoutClamp(t *testing.T) {
	dir, _ := firmwareDir(t)
	s, err := startTFTP("127.0.0.1:0", dir, "", func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40001}
	b := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40002}
	if ok, _ := s.claim(a); !ok {
		t.Fatal("claim a")
	}
	if ok, _ := s.claim(b); !ok {
		t.Fatal("two transfers per device must be allowed (IOS: size probe + download)")
	}
	if ok, dup := s.claim(a); ok || !dup {
		t.Error("same client port again must be a silent duplicate")
	}
	port := s.conn.LocalAddr().(*net.UDPAddr).Port
	if _, _, err := tftpGet(t, port, "ios.bin"); err == nil || !strings.Contains(err.Error(), "ausgelastet") {
		t.Errorf("third transfer from the same device: %v", err)
	}
	s.release(a)
	s.release(b)
	_, oack, err := tftpGet(t, port, "ios.bin", "timeout", "200", "tsize", "0")
	if err != nil || oack["timeout"] != "" {
		t.Errorf("timeout 200 sollte ignoriert werden: %v %v", err, oack)
	}
}

func TestHTTPIndexListsFiles(t *testing.T) {
	_, info, log, _ := startAll(t)
	base := "http://127.0.0.1:" + strconv.Itoa(info.Config.HTTPPort)
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `href="/sub/small.txt"`) || !strings.Contains(string(body), "598d4c200461b81522a3328565c25f7c") {
		t.Errorf("index: %d %s", resp.StatusCode, body)
	}
	resp, _ = http.Get(base + "/favicon.ico")
	resp.Body.Close()
	time.Sleep(50 * time.Millisecond)
	log.mu.Lock()
	defer log.mu.Unlock()
	for _, e := range log.ev {
		if e.File == "/favicon.ico" || e.File == "/" {
			t.Errorf("unnötiger Log-Eintrag: %+v", e)
		}
	}
}

func TestSCPWithRSAOnlyClient(t *testing.T) {
	_, info, _, img := startAll(t)
	c, err := ssh.Dial("tcp", "127.0.0.1:"+strconv.Itoa(info.Config.SSHPort), &ssh.ClientConfig{
		User: info.User, Auth: []ssh.AuthMethod{ssh.Password(info.Password)},
		HostKeyCallback:   ssh.InsecureIgnoreHostKey(),
		HostKeyAlgorithms: []string{"rsa-sha2-512", "rsa-sha2-256", "ssh-rsa"}, // like IOS 15.2
		Timeout:           3 * time.Second,
	})
	if err != nil {
		t.Fatalf("RSA-only client: %v", err)
	}
	defer c.Close()
	if data, _, err := scpFetch(t, c, "ios.bin"); err != nil || !bytes.Equal(data, img) {
		t.Fatalf("scp via RSA host key: %v", err)
	}
}

func TestTFTPSizeProbeIsInfo(t *testing.T) {
	_, info, log, _ := startAll(t)
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	req := append([]byte{0, opRRQ}, "ios.bin\x00octet\x00tsize\x000\x00"...)
	_, _ = c.WriteToUDP(req, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: info.Config.TFTPPort})
	buf := make([]byte, 512)
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, from, err := c.ReadFromUDP(buf)
	if err != nil || binary.BigEndian.Uint16(buf) != opOACK || !bytes.Contains(buf[:n], []byte("tsize")) {
		t.Fatalf("expected OACK with tsize: %v %q", err, buf[:n])
	}
	// like IOS: abort after learning the size
	_, _ = c.WriteToUDP(append([]byte{0, opERROR, 0, 0}, "Session terminated\x00"...), from)
	e := log.wait(t, "TFTP", "info")
	if !strings.Contains(e.Detail, "Dateigröße") {
		t.Errorf("event = %+v", e)
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	for _, x := range log.ev {
		if x.Proto == "TFTP" && x.State == "fehler" {
			t.Errorf("size probe logged as error: %+v", x)
		}
	}
}

func TestProgressReportsWhileRunning(t *testing.T) {
	var got []Event
	p := newProgress(Event{Proto: "SCP", File: "x.bin", Total: 100, State: "läuft"}, func(e Event) { got = append(got, e) })
	p.add(10)
	if len(got) != 0 {
		t.Fatal("reported before a second passed")
	}
	p.last = time.Now().Add(-2 * time.Second)
	p.add(15)
	if len(got) != 1 || got[0].Done != 25 || got[0].State != "läuft" || p.done() != 25 {
		t.Fatalf("progress events: %+v", got)
	}
}

func TestFinishedEventCarriesMD5(t *testing.T) {
	_, info, log, _ := startAll(t)
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(info.Config.HTTPPort) + "/sub/small.txt")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if e := log.wait(t, "HTTP", "fertig"); e.MD5 != "598d4c200461b81522a3328565c25f7c" {
		t.Errorf("md5 = %q", e.MD5)
	}
}

func TestTFTPProbeThenDownloadLikeIOS(t *testing.T) {
	_, info, log, img := startAll(t)
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	req := append([]byte{0, opRRQ}, "ios.bin\x00octet\x00tsize\x000\x00"...)
	_, _ = c.WriteToUDP(req, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: info.Config.TFTPPort})
	buf := make([]byte, 512)
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, from, err := c.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.WriteToUDP(append([]byte{0, opERROR, 0, 0}, "Session terminated\x00"...), from)
	c.Close()
	// immediately the real download, from a new client port
	got, _, err := tftpGet(t, info.Config.TFTPPort, "ios.bin", "tsize", "0", "blksize", "1468")
	if err != nil || !bytes.Equal(got, img) {
		t.Fatalf("download after probe: %v (%d bytes)", err, len(got))
	}
	if e := log.wait(t, "TFTP", "fertig"); e.MD5 == "" || e.Done != int64(len(img)) {
		t.Errorf("fertig event: %+v", e)
	}
}

func TestEndState(t *testing.T) {
	if endState(100, 100) != "fertig" || endState(40, 100) != "abgebrochen" || endState(0, 0) != "fertig" {
		t.Error("endState")
	}
}

func TestTFTPLimitsRequestsPerDevice(t *testing.T) {
	dir, _ := firmwareDir(t)
	s, err := startTFTP("127.0.0.1:0", dir, "", func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	clock := time.Unix(1_800_000_000, 0)
	s.now = func() time.Time { return clock }
	dev, other := net.IPv4(10, 0, 0, 1), net.IPv4(10, 0, 0, 2)

	for i := 0; i < maxRequests; i++ {
		if !s.allow(dev) {
			t.Fatalf("request %d refused", i+1)
		}
	}

	if s.allow(dev) {
		t.Error("request above the limit allowed")
	}
	if !s.allow(other) {
		t.Error("another device was limited too")
	}
	clock = clock.Add(requestWindow)
	if !s.allow(dev) {
		t.Error("limit not lifted after the window")
	}
}

func TestTFTPCapsBlockSize(t *testing.T) {
	dir, _ := firmwareDir(t)
	s, err := startTFTP("127.0.0.1:0", dir, "", func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	port := s.conn.LocalAddr().(*net.UDPAddr).Port

	_, oack, err := tftpGet(t, port, "ios.bin", "blksize", "65464")

	if err != nil {
		t.Fatal(err)
	}
	if oack["blksize"] != strconv.Itoa(maxBlock) {
		t.Errorf("blksize %q, want %d", oack["blksize"], maxBlock)
	}
}
