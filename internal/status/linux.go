package status

import (
	"path"
	"sort"
	"strconv"
	"strings"
)

// procSed turns /proc/<pid>/stat lines into "pid utime stime rss name"
// (the name may contain spaces and brackets, so it is matched up to the
// last ") <state> ").
const procSed = `'s/^\([0-9]*\) (\(.*\)) [A-Za-z] \([0-9-]* \)\{10\}\([0-9]*\) \([0-9]*\) \([0-9-]* \)\{8\}\([0-9]*\).*/\1 \4 \5 \7 \2/p'`

// LinuxScript prints marked sections; it only reads /proc and standard tools.
// CPU is measured from two samples one second apart (like top), not from
// ps, whose %CPU is the average over a process's whole lifetime.
const LinuxScript = `echo ==LOAD; cat /proc/loadavg; ` +
	`echo ==CPUS; nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo; ` +
	`echo ==MEM; grep -E '^(MemTotal|MemAvailable|SwapTotal|SwapFree):' /proc/meminfo; ` +
	`echo ==UP; cat /proc/uptime; ` +
	`echo ==DISK; df -P -k 2>/dev/null; ` +
	`echo ==HOST; hostname; ` +
	`echo ==OS; (. /etc/os-release 2>/dev/null; echo "$PRETTY_NAME"); ` +
	`echo ==PAGE; getconf PAGESIZE 2>/dev/null || echo 4096; ` +
	`echo ==STAT1; head -n 1 /proc/stat; echo ==P1; cat /proc/[0-9]*/stat 2>/dev/null | sed -n ` + procSed + `; ` +
	`sleep 1; ` +
	`echo ==STAT2; head -n 1 /proc/stat; echo ==P2; cat /proc/[0-9]*/stat 2>/dev/null | sed -n ` + procSed

// ParseLinux parses LinuxScript output; ok=false if it is not Linux output.
func ParseLinux(out string) (Snapshot, bool) {
	sec := sections(out)
	load, ok := sec["LOAD"]
	if !ok || len(load) == 0 {
		return Snapshot{}, false
	}
	f := strings.Fields(load[0])
	if len(f) < 3 {
		return Snapshot{}, false
	}
	s := Snapshot{Kind: KindLinux, CPUUsage: -1}
	for _, v := range f[:3] {
		x, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return Snapshot{}, false
		}
		s.Load = append(s.Load, x)
	}
	if c := sec["CPUS"]; len(c) > 0 {
		s.CPUs, _ = strconv.Atoi(strings.TrimSpace(c[0]))
	}
	mem := map[string]int64{}
	for _, l := range sec["MEM"] {
		ff := strings.Fields(l)
		if len(ff) >= 2 {
			kb, _ := strconv.ParseInt(ff[1], 10, 64)
			mem[strings.TrimSuffix(ff[0], ":")] = kb * 1024
		}
	}
	s.MemTotal = mem["MemTotal"]
	if avail, ok := mem["MemAvailable"]; ok && s.MemTotal > 0 {
		s.MemUsed = s.MemTotal - avail
	}
	s.SwapTotal = mem["SwapTotal"]
	s.SwapUsed = mem["SwapTotal"] - mem["SwapFree"]
	if u := sec["UP"]; len(u) > 0 {
		if ff := strings.Fields(u[0]); len(ff) > 0 {
			if secs, err := strconv.ParseFloat(ff[0], 64); err == nil {
				s.UptimeSec = int64(secs)
			}
		}
	}
	s.Disks = parseDisks(sec["DISK"])
	if h := sec["HOST"]; len(h) > 0 {
		s.Host = strings.TrimSpace(h[0])
	}
	if o := sec["OS"]; len(o) > 0 {
		s.OS = strings.TrimSpace(o[0])
	}
	page := int64(4096)
	if p := sec["PAGE"]; len(p) > 0 {
		if n, err := strconv.ParseInt(strings.TrimSpace(p[0]), 10, 64); err == nil && n > 0 {
			page = n
		}
	}
	busy1, total1 := cpuTicks(sec["STAT1"])
	busy2, total2 := cpuTicks(sec["STAT2"])
	if dt := total2 - total1; dt > 0 {
		s.CPUUsage = round1(float64(busy2-busy1) / float64(dt) * 100)
		s.Procs = topProcs(sec["P1"], sec["P2"], float64(dt), page, s.MemTotal)
	}
	return s, true
}

// cpuTicks sums the "cpu" line of /proc/stat: busy and total jiffies.
func cpuTicks(lines []string) (busy, total int64) {
	if len(lines) == 0 {
		return 0, 0
	}
	ff := strings.Fields(lines[0])
	if len(ff) < 5 || ff[0] != "cpu" {
		return 0, 0
	}
	for i, v := range ff[1:min(len(ff), 9)] { // user nice system idle iowait irq softirq steal
		n, _ := strconv.ParseInt(v, 10, 64)
		total += n
		if i != 3 && i != 4 {
			busy += n
		}
	}
	return busy, total
}

type procSample struct {
	ticks int64
	rss   int64
	name  string
}

func parseProcs(lines []string) map[string]procSample {
	m := make(map[string]procSample, len(lines))
	for _, l := range lines {
		ff := strings.SplitN(strings.TrimSpace(l), " ", 5)
		if len(ff) < 5 {
			continue
		}
		u, err1 := strconv.ParseInt(ff[1], 10, 64)
		st, err2 := strconv.ParseInt(ff[2], 10, 64)
		rss, _ := strconv.ParseInt(ff[3], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		m[ff[0]] = procSample{ticks: u + st, rss: rss, name: ff[4]}
	}
	return m
}

// topProcs ranks processes by CPU used between the two samples. total is the
// number of jiffies all cores together had, so a process's share is of the
// whole machine (like Windows Task Manager, never above 100 %); the sum of
// all processes matches the CPU bar.
func topProcs(p1, p2 []string, total float64, page, memTotal int64) []Proc {
	before, after := parseProcs(p1), parseProcs(p2)
	out := make([]Proc, 0, len(after))
	for pid, a := range after {
		b, ok := before[pid]
		if !ok || total <= 0 {
			continue // started during the measurement (our own sed/cat/sleep)
		}
		p := Proc{PID: pid, Name: a.name, CPU: round1(float64(a.ticks-b.ticks) / total * 100)}
		if memTotal > 0 {
			p.Mem = round1(float64(a.rss*page) / float64(memTotal) * 100)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CPU != out[j].CPU {
			return out[i].CPU > out[j].CPU
		}
		if out[i].Mem != out[j].Mem {
			return out[i].Mem > out[j].Mem
		}
		return out[i].PID < out[j].PID
	})
	if len(out) > maxProcs {
		out = out[:maxProcs]
	}
	return out
}

func round1(x float64) float64 { return float64(int64(x*10+0.5)) / 10 }

// pseudoFS are df sources that are not real storage.
var pseudoFS = map[string]bool{"tmpfs": true, "devtmpfs": true, "overlay": true, "shm": true, "none": true,
	"udev": true, "rootfs": true, "efivarfs": true, "cgroup": true, "cgroup2": true, "proc": true, "sysfs": true}

var skipMountPrefixes = []string{"/proc", "/sys", "/dev", "/run", "/var/lib/docker", "/snap", "/boot/efi", "/etc"}

const maxDisks = 4

// parseDisks keeps real filesystems: "/" first, then the largest. On Unraid
// the array (/mnt/user) replaces the single /mnt/diskN mounts.
func parseDisks(lines []string) []Disk {
	var all []Disk
	seen := map[string]int{}
	hasUser := false
	for _, l := range lines {
		ff := strings.Fields(l)
		if len(ff) < 6 || ff[0] == "Filesystem" {
			continue
		}
		total, err1 := strconv.ParseInt(ff[1], 10, 64)
		used, err2 := strconv.ParseInt(ff[2], 10, 64)
		mount := strings.Join(ff[5:], " ")
		if err1 != nil || err2 != nil || total == 0 || pseudoFS[ff[0]] || skipMount(mount) {
			continue
		}
		hasUser = hasUser || mount == "/mnt/user"
		key := ff[0] + "|" + ff[1]
		if i, dup := seen[key]; dup { // bind mount of the same filesystem
			if mount == "/mnt/user" {
				all[i].Mount = mount // Unraid: name the array by its usual path
			}
			continue
		}
		seen[key] = len(all)
		all = append(all, Disk{Mount: mount, Total: total * 1024, Used: used * 1024})
	}
	out := all[:0]
	for _, d := range all {
		if hasUser && (d.Mount == "/mnt/user0" || path.Dir(d.Mount) == "/mnt" && strings.HasPrefix(path.Base(d.Mount), "disk")) {
			continue
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Mount == "/") != (out[j].Mount == "/") {
			return out[i].Mount == "/"
		}
		return out[i].Total > out[j].Total
	})
	if len(out) > maxDisks {
		out = out[:maxDisks]
	}
	return out
}

func skipMount(m string) bool {
	for _, p := range skipMountPrefixes {
		if m == p || strings.HasPrefix(m, p+"/") {
			return true
		}
	}
	return false
}
