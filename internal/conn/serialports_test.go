package conn

import (
	"testing"

	"go.bug.st/serial/enumerator"
)

func TestDescribePorts(t *testing.T) {
	got := DescribePorts([]*enumerator.PortDetails{
		{Name: "COM10", Product: "Prolific USB-to-Serial Comm Port (COM10)", IsUSB: true, VID: "067b"},
		{Name: "COM6", Product: "USB Serial Port (COM6)", IsUSB: true, VID: "0403"},
		{Name: "COM4", Product: "Serielles USB-Gerät (COM4)", IsUSB: true, VID: "28DE"},
		{Name: "COM1", Product: "Kommunikationsanschluss (COM1)"},
		{Name: "COM7", Product: "Mystery (COM7)", IsUSB: true, VID: "ABCD"},
		nil,
		{Name: ""},
	})
	want := []SerialPortInfo{
		{Name: "COM1", Description: "Kommunikationsanschluss"},
		{Name: "COM4", Description: "Serielles USB-Gerät", Vendor: "Valve", USB: true},
		{Name: "COM6", Description: "USB Serial Port", Vendor: "FTDI", USB: true, Console: true},
		{Name: "COM7", Description: "Mystery", Vendor: "USB ABCD", USB: true},
		{Name: "COM10", Description: "Prolific USB-to-Serial Comm Port", Vendor: "Prolific", USB: true, Console: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d ports: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("port %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestPortNumberOrdersNonCOMLast(t *testing.T) {
	if !(portNumber("COM2") < portNumber("COM10")) || portNumber("/dev/ttyUSB0") <= portNumber("COM99") || portNumber("COMX") <= portNumber("COM1") {
		t.Fatal("port ordering wrong")
	}
}

func TestSerialPortInfosRuns(t *testing.T) {
	if _, err := SerialPortInfos(); err != nil {
		t.Logf("enumeration unavailable here: %v", err)
	}
}
