package terminal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var unsafeChars = strings.NewReplacer(`\`, "_", "/", "_", ":", "_", "*", "_", "?", "_", `"`, "_", "<", "_", ">", "_", "|", "_")

// safeName makes a name usable as part of a Windows path: no reserved
// characters, no trailing dot or space, no device name like CON or NUL.
func safeName(s string) string {
	s = strings.TrimRight(unsafeChars.Replace(s), ". ")
	if s == "" {
		return "_"
	}
	stem, _, _ := strings.Cut(strings.ToUpper(s), ".")
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return "_" + s
	}
	return s
}

// ExpandLogPath fills {session}, {host}, {date} and {time} placeholders.
func ExpandLogPath(pattern, session, host string, now time.Time) string {
	r := strings.NewReplacer(
		"{session}", safeName(session),
		"{host}", safeName(host),
		"{date}", now.Format("2006-01-02"),
		"{time}", now.Format("150405"),
	)
	return r.Replace(pattern)
}

// OpenLog creates the log file (and its directory) for appending.
func OpenLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("Log-Ordner: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("Log-Datei: %w", err)
	}
	return f, nil
}
