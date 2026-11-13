package approval

import "testing"

func TestAllReadOnly(t *testing.T) {
	ok := [][]string{
		{"show version", "show ip int brief", "show run | include hostname"},
		{"display interface brief", "show run | section interface | count"},
		{"show logging | exc LINK"},
	}
	for _, c := range ok {
		if !AllReadOnly(c) {
			t.Errorf("%q should be read-only", c)
		}
	}
	bad := [][]string{
		{},
		{"show version", "conf t"},
		{"show run | redirect tftp://10.0.0.1/x"},
		{"show run | append flash:x"},
		{"show tech | tee flash:x"},
		{"show run | more"},
		{"show version\rreload"},
		{"show version; reload"},
		{"show $(id)"},
		{"show `id`"},
		{"show clock && reboot"},
		{"show run > flash:x"},
		{"ping 10.0.0.1"},
		{"dir flash:"},
		{"more ~/.ssh/id_rsa"},
		{"shutdown"},
		{"showx"},
		{"sh -c rm -rf /tmp/x"},
		{"sh ip int brief"},
		{"dis cur"},
	}
	for _, c := range bad {
		if AllReadOnly(c) {
			t.Errorf("%q must need approval", c)
		}
	}
}
