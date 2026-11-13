// Command devsshd is a tiny local SSH/SFTP server for trying easyTTY out.
//
//	go run ./tools/devsshd            # listens on 127.0.0.1:2222, user test / password test
//
// The shell is fake (a few demo commands); SFTP serves a temp directory.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"easytty/internal/sshtest"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:2222", "listen address")
	user := flag.String("user", "test", "user name")
	pass := flag.String("pass", "test", "password")
	root := flag.String("root", filepath.Join(os.TempDir(), "devsshd-root"), "SFTP root directory")
	flag.Parse()

	if err := os.MkdirAll(filepath.Join(*root, "configs"), 0o755); err != nil {
		log.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(*root, "configs", "core-sw1.cfg"), []byte("hostname core-sw1\n!\ninterface Gi1/0/1\n description uplink\n"), 0o644)
	_ = os.WriteFile(filepath.Join(*root, "README.txt"), []byte("devsshd test root\n"), 0o644)

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if c.User() == *user && string(pw) == *pass {
				return nil, nil
			}
			return nil, fmt.Errorf("denied")
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("devsshd on %s (user %q, password %q), SFTP root %s", *addr, *user, *pass, *root)
	for {
		nc, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go handle(nc, cfg, *root)
	}
}

func handle(nc net.Conn, cfg *ssh.ServerConfig, root string) {
	sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		nc.Close()
		return
	}
	log.Printf("login %s from %s", sc.User(), sc.RemoteAddr())
	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		if nch.ChannelType() == "direct-tcpip" { // port forwards and jump hosts
			go sshtest.DirectTCPIP(nch)
			continue
		}
		if nch.ChannelType() != "session" {
			nch.Reject(ssh.UnknownChannelType, "only sessions")
			continue
		}
		ch, creqs, err := nch.Accept()
		if err != nil {
			continue
		}
		go session(ch, creqs, sc.User(), root)
	}
}

func session(ch ssh.Channel, reqs <-chan *ssh.Request, user, root string) {
	defer ch.Close()
	cols := 80
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			if len(req.Payload) > 4 {
				n := binary.BigEndian.Uint32(req.Payload)
				if off := 4 + int(n); len(req.Payload) >= off+4 {
					cols = int(binary.BigEndian.Uint32(req.Payload[off:]))
				}
			}
			req.Reply(true, nil)
		case "window-change":
			if len(req.Payload) >= 4 {
				cols = int(binary.BigEndian.Uint32(req.Payload))
			}
		case "shell":
			req.Reply(true, nil)
			go func() {
				shell(ch, user, func() int { return cols })
				ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
				ch.Close()
			}()
		case "exec":
			// Answers easyTTY's status probe with plausible, moving demo values.
			req.Reply(true, nil)
			cmd := ""
			if len(req.Payload) > 4 {
				cmd = string(req.Payload[4:])
			}
			if strings.Contains(cmd, "/proc/loadavg") {
				io.WriteString(ch, demoStatus())
			} else {
				fmt.Fprintf(ch, "%s: command not found\n", strings.Fields(cmd + " ?")[0])
			}
			ch.CloseWrite()
			ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
			return
		case "subsystem":
			if len(req.Payload) > 4 && string(req.Payload[4:]) == "sftp" {
				req.Reply(true, nil)
				srv, err := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(root))
				if err == nil {
					_ = srv.Serve()
				}
				return
			}
			req.Reply(false, nil)
		default:
			if req.WantReply {
				req.Reply(true, nil)
			}
		}
	}
}

func shell(ch io.ReadWriter, user string, cols func() int) {
	prompt := fmt.Sprintf("\x1b[32m%s@devsshd\x1b[0m:\x1b[34m~\x1b[0m$ ", user)
	fmt.Fprintf(ch, "Willkommen bei devsshd – Befehle: help, ls, seq N, colors, cols, exit\r\n%s", prompt)
	var line []byte
	var more []string // output lines still waiting behind "--More--"
	buf := make([]byte, 1024)
	for {
		n, err := ch.Read(buf)
		if err != nil {
			return
		}
		for _, b := range buf[:n] {
			if len(more) > 0 { // waiting at "--More--", like a switch
				io.WriteString(ch, "\r          \r")
				if b == ' ' {
					more = page(ch, more)
				} else {
					more = nil // any other key ends the output and is used up
				}
				if len(more) == 0 {
					io.WriteString(ch, prompt)
				}
				continue
			}
			switch b {
			case '\r', '\n':
				io.WriteString(ch, "\r\n")
				cmd := strings.TrimSpace(string(line))
				line = line[:0]
				if out := showOutput(cmd); out != nil {
					if more = page(ch, out); len(more) == 0 {
						io.WriteString(ch, prompt)
					}
					continue
				}
				if !run(ch, cmd, cols()) {
					return
				}
				io.WriteString(ch, prompt)
			case 0x7f, 0x08:
				if len(line) > 0 {
					line = line[:len(line)-1]
					io.WriteString(ch, "\b \b")
				}
			case 0x03:
				line = line[:0]
				io.WriteString(ch, "^C\r\n"+prompt)
			default:
				if b >= 0x20 {
					line = append(line, b)
					ch.Write([]byte{b})
				}
			}
		}
	}
}

const pageLines = 23 // "terminal length 24" of a Cisco switch

// page writes one screen of lines and returns the rest; with a rest left it
// ends with " --More-- " and the next key decides.
func page(w io.Writer, lines []string) []string {
	n := min(pageLines, len(lines))
	for _, l := range lines[:n] {
		io.WriteString(w, l+"\r\n")
	}
	if n < len(lines) {
		io.WriteString(w, " --More-- ")
	}
	return lines[n:]
}

// showOutput answers Cisco show commands for testing paging and config
// backups; nil = no show command.
func showOutput(cmd string) []string {
	switch strings.Join(strings.Fields(cmd), " ") {
	case "show running-config", "show run", "sh run":
		out := []string{"Building configuration...", "", "Current configuration : 4242 bytes", "!", "hostname devsshd", "!"}
		for i := 1; i <= 12; i++ {
			out = append(out, fmt.Sprintf("interface GigabitEthernet1/0/%d", i), fmt.Sprintf(" description Port %d", i), " switchport mode access", "!")
		}
		return append(out, "line vty 0 4", " login local", "!", "end")
	case "show version", "sh ver":
		return []string{"devsshd Software, Version 1.0 (Testserver)", "devsshd uptime is 1 hour"}
	case "show clock", "sh clock":
		return []string{time.Now().Format("15:04:05.000 MEZ Mon Jan 2 2006")}
	case "show ip interface brief", "sh ip int br":
		return []string{
			"Interface              IP-Address      OK? Method Status                Protocol",
			"Vlan1                  10.10.10.2      YES DHCP   up                    up",
			"GigabitEthernet1/0/1   unassigned      YES unset  up                    up",
			"GigabitEthernet1/0/2   unassigned      YES unset  down                  down",
			"GigabitEthernet1/0/7   unassigned      YES unset  err-disabled          down",
		}
	case "show mac address-table", "sh mac add":
		return []string{
			"Vlan    Mac Address       Type        Ports",
			"   1    0050.56a1.2b3c    DYNAMIC     Gi1/0/1",
			"   1    00:1a:2b:3c:4d:5e DYNAMIC     Gi1/0/2",
		}
	}
	return nil
}

func run(w io.Writer, cmd string, cols int) bool {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return true
	}
	switch fields[0] {
	case "exit", "logout":
		return false
	case "help":
		io.WriteString(w, "help, ls, seq N (Durchsatztest), colors, cols, exit\r\n")
	case "ls": // like GNU ls --color: folders bold blue, links bold cyan, sticky dir, script, plain file
		io.WriteString(w, "\x1b[01;34mbin\x1b[0m  \x1b[01;34metc\x1b[0m  \x1b[01;36minitrd.img\x1b[0m  \x1b[01;34mlost+found\x1b[0m  "+
			"\x1b[30;42mtmp\x1b[0m  \x1b[01;32mrun.sh\x1b[0m  README.txt\r\n")
	case "cols":
		fmt.Fprintf(w, "Terminalbreite: %d\r\n", cols)
	case "colors":
		for i := 0; i < 16; i++ {
			fmt.Fprintf(w, "\x1b[48;5;%dm  %2d  \x1b[0m", i, i)
			if i == 7 {
				io.WriteString(w, "\r\n")
			}
		}
		io.WriteString(w, "\r\n")
	case "seq":
		n := 1000
		if len(fields) > 1 {
			if v, err := strconv.Atoi(fields[1]); err == nil {
				n = v
			}
		}
		var sb strings.Builder
		for i := 1; i <= n; i++ {
			sb.WriteString(strconv.Itoa(i))
			sb.WriteString("\r\n")
			if sb.Len() > 32*1024 {
				io.WriteString(w, sb.String())
				sb.Reset()
			}
		}
		io.WriteString(w, sb.String())
	default:
		fmt.Fprintf(w, "%s: command not found\r\n", fields[0])
	}
	return true
}

var started = time.Now()

// demoStatus imitates the Linux status script output with values that move:
// two /proc samples 400 jiffies apart (4 cores × 100), like the real script.
func demoStatus() string {
	t := float64(time.Since(started).Seconds())
	load := 0.6 + 0.5*math.Sin(t/7)
	sshd := int(12 + 8*math.Sin(t/3))  // ticks of 100 → percent of one core
	busy := 60 + int(40*math.Sin(t/9)) // of 400 total jiffies
	return fmt.Sprintf(`==LOAD
%.2f 0.55 0.48 1/312 4242
==CPUS
4
==MEM
MemTotal:        16300000 kB
MemAvailable:    %d kB
SwapTotal:       4000000 kB
SwapFree:        3900000 kB
==UP
%d 0
==DISK
Filesystem     1024-blocks      Used Available Capacity Mounted on
/dev/nvme0n1p2   488000000 211000000 277000000      44%% /
tmpfs              8000000      1000   7999000       1%% /run
/dev/sdb1       3906000000 2900000000 1006000000    75%% /srv/daten
==HOST
devsshd
==OS
devsshd Demo (Windows-Testserver)
==PAGE
4096
==STAT1
cpu  10000 0 5000 80000 100 0 0 0 0 0
==P1
1201 500 100 10000 sshd
880 900 100 30000 nginx
412 300 50 280000 postgres
==STAT2
cpu  %d 0 5000 %d 100 0 0 0 0 0
==P2
1201 %d 100 10000 sshd
880 903 100 30000 nginx
412 301 50 280000 postgres
`, load, 9000000+int(2000000*math.Sin(t/5)), 86400*3+int(t), 10000+busy, 80000+400-busy, 500+sshd)
}
