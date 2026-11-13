package securecrt

import (
	"strings"
	"testing"
)

func FuzzParseINI(f *testing.F) {
	f.Add([]byte(`S:"Hostname"=sw1` + "\n" + `D:"[SSH2] Port"=00000016` + "\n"))
	f.Add([]byte(`D:"x"=zz`))
	f.Fuzz(func(t *testing.T, raw []byte) { parseINI(raw) })
}

func FuzzLoadXML(f *testing.F) {
	f.Add(`<VanDyke><key name="Sessions"><key name="sw1"><string name="Hostname">h</string><dword name="[SSH2] Port">22</dword></key></key></VanDyke>`)
	f.Add(`<key name="Sessions"><key name="a"><key name="b"></key></key></key>`)
	f.Fuzz(func(t *testing.T, doc string) { _, _ = loadXML(strings.NewReader(doc)) })
}
