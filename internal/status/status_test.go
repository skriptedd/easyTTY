package status

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"easytty/internal/sshtest"
)

const linuxOut = `==LOAD
3.54 3.42 3.42 2/431 12345
==CPUS
4
==MEM
MemTotal:        8000000 kB
MemAvailable:    6000000 kB
SwapTotal:       2000000 kB
SwapFree:        1500000 kB
==UP
123456.78 400000.00
==DISK
Filesystem     1024-blocks      Used Available Capacity Mounted on
rootfs             7900000    386000   7514000       5% /
tmpfs               131072      1000    130072       1% /run
/dev/md1p1      3906000000 2000000000 1906000000    52% /mnt/disk1
/dev/md2p1      3906000000 1000000000 2906000000    26% /mnt/disk2
/dev/nvme0n1p1   976000000  400000000  576000000    41% /mnt/cache
shfs            7812000000 3000000000 4812000000    39% /mnt/user
shfs            7812000000 3000000000 4812000000    39% /mnt/user0
/dev/sda1         30000000   1000000  29000000       3% /boot
/dev/loop2        20000000  12000000   8000000      60% /var/lib/docker
==HOST
nas01
==OS
Unraid OS 7
==PAGE
4096
==STAT1
cpu  1000 0 500 8000 100 0 0 0 0 0
==P1
1234 5000 1000 250000 qemu-system-x86
987 300 100 20000 Plex Media Server
555 10 0 100 sleepy
==STAT2
cpu  1150 0 550 8180 120 0 0 0 0 0
==P2
1234 5040 1010 250000 qemu-system-x86
987 310 100 20000 Plex Media Server
555 10 0 100 sleepy
4242 0 0 300 ps
`

const ciscoCPU = `CPU utilization for five seconds: 5%/1%; one minute: 7%; five minutes: 6%
 PID Runtime(ms)     Invoked      uSecs   5Sec   1Min   5Min TTY Process
 164     1234567      890123       1387  2.39%  1.10%  1.02%   0 Hulc LED Process
  87      456789      123456       3700  0.47%  0.30%  0.29%   0 PLFM-MGR IPC pro
`

const ciscoMem = `Processor Pool Total:  329154620 Used:  156403952 Free:  172750668
 lsmpi_io Pool Total:    6295128 Used:    6294296 Free:        832
`

const ciscoUp = "FOC2023Z4V7 uptime is 2 hours, 13 minutes\r\n"

func TestParseLinux(t *testing.T) {
	s, ok := ParseLinux(linuxOut)
	if !ok {
		t.Fatal("not recognised")
	}
	if s.Kind != KindLinux || s.Host != "nas01" || s.OS != "Unraid OS 7" || s.CPUs != 4 {
		t.Fatalf("basics: %+v", s)
	}
	if len(s.Load) != 3 || s.Load[0] != 3.54 {
		t.Fatalf("load: %v", s.Load)
	}
	if s.MemTotal != 8000000*1024 || s.MemUsed != 2000000*1024 || s.SwapUsed != 500000*1024 {
		t.Fatalf("mem: %+v", s)
	}
	if s.UptimeSec != 123456 {
		t.Fatalf("uptime: %+v", s)
	}
	// /proc/stat: 400 jiffies passed (100 per core), 200 of them busy.
	if s.CPUUsage != 50 {
		t.Errorf("cpu usage = %v", s.CPUUsage)
	}
	// qemu: 50 of 400 jiffies of the whole machine = 12.5 %; "ps" started during the sample → not listed.
	if len(s.Procs) != 3 || s.Procs[0].Name != "qemu-system-x86" || s.Procs[0].CPU != 12.5 || s.Procs[1].Name != "Plex Media Server" || s.Procs[1].CPU != 2.5 {
		t.Fatalf("procs: %+v", s.Procs)
	}
	if s.Procs[0].Mem != 12.5 { // 250000 pages * 4 KiB of 8000000 KiB
		t.Errorf("mem%% = %v", s.Procs[0].Mem)
	}
	var mounts []string
	for _, d := range s.Disks {
		mounts = append(mounts, d.Mount)
	}
	if strings.Join(mounts, ",") != "/mnt/user,/mnt/cache,/boot" {
		t.Errorf("disks: %v", mounts)
	}
	if _, ok := ParseLinux("% Invalid input detected at '^' marker."); ok {
		t.Fatal("Cisco error output must not parse as Linux")
	}
	if _, ok := ParseLinux("==LOAD\nnot numbers here\n"); ok {
		t.Fatal("garbage must not parse")
	}
}

func TestParseCisco(t *testing.T) {
	s, ok := ParseCisco(ciscoCPU, ciscoMem, ciscoUp)
	if !ok {
		t.Fatal("not recognised")
	}
	if len(s.CPU) != 3 || s.CPU[0] != 5 || s.CPU[1] != 7 || s.CPU[2] != 6 {
		t.Fatalf("cpu: %v", s.CPU)
	}
	if s.MemTotal != 329154620 || s.MemUsed != 156403952 {
		t.Fatalf("mem: %+v", s)
	}
	if s.Host != "FOC2023Z4V7" || s.Uptime != "2 hours, 13 minutes" {
		t.Fatalf("uptime: %+v", s)
	}
	if len(s.Procs) != 2 || s.Procs[0].Name != "Hulc LED Process" || s.Procs[0].CPU != 2.39 {
		t.Fatalf("procs: %+v", s.Procs)
	}
	if _, ok := ParseCisco("bash: show: command not found", "", ""); ok {
		t.Fatal("non-IOS output must not parse")
	}
}

func fakeRun(outputs map[string]string, fail map[string]bool) Execer {
	return func(_ context.Context, cmd string) (string, error) {
		if fail[cmd] {
			return "", errors.New("exec refused")
		}
		return outputs[cmd], nil
	}
}

func TestCollectDetectsKind(t *testing.T) {
	lin := fakeRun(map[string]string{LinuxScript: linuxOut}, nil)
	s, err := Collect(context.Background(), lin, "")
	if err != nil || s.Kind != KindLinux || s.Taken == 0 {
		t.Fatalf("linux: %+v %v", s, err)
	}
	ios := fakeRun(map[string]string{
		LinuxScript: "% Invalid input detected at '^' marker.", CiscoCPU: ciscoCPU, CiscoMemory: ciscoMem, CiscoUptime: ciscoUp,
	}, nil)
	s, err = Collect(context.Background(), ios, "")
	if err != nil || s.Kind != KindCisco {
		t.Fatalf("cisco: %+v %v", s, err)
	}
	if _, err := Collect(context.Background(), ios, KindLinux); err == nil {
		t.Fatal("forced linux on a Cisco must fail")
	}
	none := fakeRun(map[string]string{}, map[string]bool{CiscoCPU: true})
	if _, err := Collect(context.Background(), none, ""); err == nil || !strings.Contains(err.Error(), "weder Linux") {
		t.Fatalf("unknown device: %v", err)
	}
	if _, err := Collect(context.Background(), fakeRun(map[string]string{CiscoCPU: "nonsense"}, nil), KindCisco); err == nil {
		t.Fatal("unparsable IOS output must fail")
	}
}

func TestLimitedBuffer(t *testing.T) {
	var b limitedBuffer
	big := strings.Repeat("x", maxOutput+100)
	if n, _ := b.Write([]byte(big)); n != len(big) || b.Len() != maxOutput {
		t.Fatalf("n=%d len=%d", n, b.Len())
	}
}

func TestSSHExecAgainstServer(t *testing.T) {
	srv := sshtest.Start(t, "u", "pw", nil)
	srv.Exec = func(cmd string) string {
		if cmd == LinuxScript {
			return linuxOut
		}
		return "unknown"
	}
	client, err := ssh.Dial("tcp", srv.Addr, &ssh.ClientConfig{
		User: "u", Auth: []ssh.AuthMethod{ssh.Password("pw")}, HostKeyCallback: ssh.FixedHostKey(srv.HostKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	s, err := Collect(context.Background(), SSHExec(client), "")
	if err != nil || s.Host != "nas01" {
		t.Fatalf("got %+v %v", s, err)
	}
}

func TestParseDisksUnraidOrder(t *testing.T) {
	d := parseDisks([]string{
		"Filesystem     1024-blocks       Used  Available Capacity Mounted on",
		"rootfs             7936728     386488    7550240       5% /",
		"/dev/sda1         30285824    2475456   27810368       9% /boot",
		"/dev/md1p1      3905110812 2592696516 1312414296      67% /mnt/disk1",
		"/dev/sdc1        244197560  128426956  114339972      53% /mnt/cache",
		"shfs           15620443248 7309308692 8311134556      47% /mnt/user0",
		"shfs           15620443248 7309308692 8311134556      47% /mnt/user",
		"/dev/loop3         1048576       7176     925208       1% /etc/libvirt",
	})
	var m []string
	for _, x := range d {
		m = append(m, x.Mount)
	}
	if strings.Join(m, ",") != "/mnt/user,/mnt/cache,/boot" {
		t.Errorf("disks: %v", m)
	}
}
