package automation

import (
	"fmt"
	"unicode"
)

// HasHiddenChars reports control characters (except tab) and invisible
// format characters (bidi overrides, zero-width …). A stray "\r" inside a
// command would run a second command the approval dialog never showed.
func HasHiddenChars(s string) bool {
	for _, r := range s {
		if r == '\t' {
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return true
		}
	}
	return false
}

// CheckCommands rejects commands with hidden characters.
func CheckCommands(cmds []string) error {
	for i, c := range cmds {
		if HasHiddenChars(c) {
			return fmt.Errorf("Befehl %d enthält Steuer- oder unsichtbare Zeichen – nicht erlaubt", i+1)
		}
	}
	return nil
}

// CleanLabel strips hidden characters and caps the length (names shown in dialogs).
func CleanLabel(s string, maxRunes int) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' {
			r = ' '
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		out = append(out, r)
		if len(out) >= maxRunes {
			out = append(out, '…')
			break
		}
	}
	return string(out)
}
