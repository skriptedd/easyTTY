package status

import "testing"

func FuzzParseLinux(f *testing.F) {
	f.Add("==HOST\nnas01\n==OS\nx\n==PAGE\n4096\n==STAT1\ncpu 1 2 3\n==P1\n1 2 3 4 a\n")
	f.Add("==")
	f.Fuzz(func(t *testing.T, out string) { ParseLinux(out) })
}

func FuzzParseCisco(f *testing.F) {
	f.Add("CPU utilization for five seconds: 5%/0%; one minute: 6%; five minutes: 7%\n 1 2 3 4 5 6 7 8 proc\n", "Processor 100 50 50\n", "sw1 uptime is 1 week\n")
	f.Fuzz(func(t *testing.T, cpu, mem, up string) { ParseCisco(cpu, mem, up) })
}
