package conn

import (
	"fmt"
	"sort"
	"time"

	"go.bug.st/serial"
)

// SerialConfig describes a serial (COM) console.
type SerialConfig struct {
	Port     string
	Baud     int
	DataBits int
	Parity   string // none | odd | even | mark | space
	StopBits string // 1 | 1.5 | 2
}

// SerialConn is a serial console connection.
type SerialConn struct {
	port serial.Port
}

var parities = map[string]serial.Parity{
	"none": serial.NoParity, "odd": serial.OddParity, "even": serial.EvenParity,
	"mark": serial.MarkParity, "space": serial.SpaceParity,
}

var stopBits = map[string]serial.StopBits{
	"1": serial.OneStopBit, "1.5": serial.OnePointFiveStopBits, "2": serial.TwoStopBits,
}

// SerialMode converts the config into a go.bug.st/serial mode.
func (c SerialConfig) SerialMode() (*serial.Mode, error) {
	p, ok := parities[c.Parity]
	if !ok {
		return nil, fmt.Errorf("Parität ungültig: %q", c.Parity)
	}
	sb, ok := stopBits[c.StopBits]
	if !ok {
		return nil, fmt.Errorf("Stoppbits ungültig: %q", c.StopBits)
	}
	if c.Baud <= 0 || c.DataBits < 5 || c.DataBits > 8 {
		return nil, fmt.Errorf("Baudrate/Datenbits ungültig")
	}
	return &serial.Mode{BaudRate: c.Baud, DataBits: c.DataBits, Parity: p, StopBits: sb}, nil
}

// OpenSerial opens a COM port.
func OpenSerial(cfg SerialConfig) (*SerialConn, error) {
	mode, err := cfg.SerialMode()
	if err != nil {
		return nil, err
	}
	p, err := serial.Open(cfg.Port, mode)
	if err != nil {
		return nil, fmt.Errorf("%s öffnen: %w", cfg.Port, err)
	}
	return &SerialConn{port: p}, nil
}

// SerialPorts lists available COM ports.
func SerialPorts() ([]string, error) {
	ports, err := serial.GetPortsList()
	if err != nil {
		return nil, err
	}
	sort.Strings(ports)
	return ports, nil
}

func (c *SerialConn) Read(p []byte) (int, error)  { return c.port.Read(p) }
func (c *SerialConn) Write(p []byte) (int, error) { return c.port.Write(p) }
func (c *SerialConn) Close() error                { return c.port.Close() }

// Resize is a no-op for serial lines.
func (c *SerialConn) Resize(cols, rows int) error { return nil }

// SendBreak sends a break signal (e.g. Cisco ROMMON).
func (c *SerialConn) SendBreak() error { return c.port.Break(breakMillis * time.Millisecond) }
