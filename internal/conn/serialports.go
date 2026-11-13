package conn

import (
	"regexp"
	"sort"
	"strings"

	"go.bug.st/serial/enumerator"
)

// SerialPortInfo describes a COM port for the port picker.
type SerialPortInfo struct {
	Name        string `json:"name"`        // COM6
	Description string `json:"description"` // USB Serial Port
	Vendor      string `json:"vendor"`      // FTDI
	USB         bool   `json:"usb"`
	Console     bool   `json:"console"` // typical console cable (USB-serial bridge or Cisco USB console)
}

// consoleVendors are USB vendor IDs of serial bridge chips used in console cables.
var consoleVendors = map[string]string{
	"0403": "FTDI",
	"067B": "Prolific",
	"10C4": "Silicon Labs",
	"1A86": "WCH (CH340)",
	"05A6": "Cisco",
	"04E2": "Exar",
	"0557": "ATEN",
}

// otherVendors helps to recognise devices that are definitely not a console cable.
var otherVendors = map[string]string{
	"28DE": "Valve",
	"2341": "Arduino",
	"1546": "u-blox",
	"239A": "Adafruit",
	"2E8A": "Raspberry Pi",
	"045E": "Microsoft",
	"046D": "Logitech",
}

var comSuffix = regexp.MustCompile(`\s*\(COM\d+\)\s*$`)

// DescribePorts turns enumerator details into picker entries, sorted by port number.
func DescribePorts(details []*enumerator.PortDetails) []SerialPortInfo {
	out := make([]SerialPortInfo, 0, len(details))
	for _, d := range details {
		if d == nil || d.Name == "" {
			continue
		}
		vid := strings.ToUpper(d.VID)
		info := SerialPortInfo{
			Name:        d.Name,
			Description: strings.TrimSpace(comSuffix.ReplaceAllString(d.Product, "")),
			USB:         d.IsUSB,
		}
		if v, ok := consoleVendors[vid]; ok {
			info.Vendor, info.Console = v, true
		} else if v, ok := otherVendors[vid]; ok {
			info.Vendor = v
		} else if vid != "" {
			info.Vendor = "USB " + vid
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := portNumber(out[i].Name), portNumber(out[j].Name); a != b {
			return a < b
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// portNumber sorts COM2 before COM10; non-COM names go last in name order.
func portNumber(name string) int {
	n := 0
	if !strings.HasPrefix(strings.ToUpper(name), "COM") {
		return 1 << 30
	}
	for _, c := range name[3:] {
		if c < '0' || c > '9' {
			return 1 << 30
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// SerialPortInfos lists COM ports with descriptions; falls back to bare names.
func SerialPortInfos() ([]SerialPortInfo, error) {
	details, err := enumerator.GetDetailedPortsList()
	if err == nil {
		return DescribePorts(details), nil
	}
	names, err := SerialPorts()
	if err != nil {
		return nil, err
	}
	out := make([]SerialPortInfo, len(names))
	for i, n := range names {
		out[i] = SerialPortInfo{Name: n}
	}
	return out, nil
}
