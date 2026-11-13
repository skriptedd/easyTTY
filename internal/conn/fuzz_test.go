package conn

import "testing"

func FuzzParseTarget(f *testing.F) {
	for _, s := range []string{"admin@10.0.0.1:22", "[fe80::1]:2222", "console:6@host", "", "@", ":", "a@b@c:99999"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		tgt, err := ParseTarget(in)
		if err == nil && (tgt.Port < 1 || tgt.Port > 65535) {
			t.Fatalf("%q: port %d", in, tgt.Port)
		}
	})
}
