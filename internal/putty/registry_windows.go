//go:build windows

package putty

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"

	"easytty/internal/store"
)

const sessionsKey = `Software\SimonTatham\PuTTY\Sessions`

// LoadSessions reads all PuTTY sessions of the current user.
// It returns the converted sessions and the number of skipped ones.
func LoadSessions() ([]store.Session, int, error) {
	root, err := registry.OpenKey(registry.CURRENT_USER, sessionsKey, registry.ENUMERATE_SUB_KEYS)
	if errors.Is(err, registry.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("PuTTY-Registry öffnen: %w", err)
	}
	defer root.Close()
	names, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return nil, 0, err
	}
	var out []store.Session
	skipped := 0
	for _, name := range names {
		v, err := readValues(sessionsKey + `\` + name)
		if err != nil {
			skipped++
			continue
		}
		if s, ok := Convert(name, v); ok {
			out = append(out, s)
		} else if DecodeName(name) != "Default Settings" {
			skipped++
		}
	}
	return out, skipped, nil
}

func readValues(path string) (Values, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer k.Close()
	v := Values{}
	for _, name := range []string{"HostName", "UserName", "Protocol", "PublicKeyFile", "SerialLine"} {
		if s, _, err := k.GetStringValue(name); err == nil {
			v[name] = s
		}
	}
	for _, name := range []string{"PortNumber", "SerialSpeed", "SerialDataBits", "SerialParity", "SerialStopHalfbits", "PingIntervalSecs"} {
		if n, _, err := k.GetIntegerValue(name); err == nil {
			v[name] = n
		}
	}
	return v, nil
}
