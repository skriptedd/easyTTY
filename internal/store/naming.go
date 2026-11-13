package store

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Naming scheme options for session names (Settings.NameCase / NameSpaces).
const (
	CaseKeep       = ""           // as typed
	CaseLower      = "lower"      // core switch 1
	CaseUpper      = "upper"      // CORE SWITCH 1
	CaseCapitalize = "capitalize" // Core switch 1 (first letter only)
	CaseTitle      = "title"      // Core Switch 1 (every word)

	SpacesKeep       = ""  // keep spaces
	SpacesUnderscore = "_" // core_switch_1
	SpacesDash       = "-" // core-switch-1
)

// ValidNaming reports whether case/spaces are known options.
func ValidNaming(nameCase, spaces string) bool {
	switch nameCase {
	case CaseKeep, CaseLower, CaseUpper, CaseCapitalize, CaseTitle:
	default:
		return false
	}
	switch spaces {
	case SpacesKeep, SpacesUnderscore, SpacesDash:
		return true
	}
	return false
}

// FormatName applies the configured naming scheme to a session name.
// Runs of whitespace collapse to one separator; leading/trailing ones go.
func (s Settings) FormatName(name string) string {
	words := strings.Fields(name)
	if len(words) == 0 {
		return ""
	}
	out := strings.Join(words, " ")
	switch s.NameCase {
	case CaseLower:
		out = strings.ToLower(out)
	case CaseUpper:
		out = strings.ToUpper(out)
	case CaseCapitalize:
		out = upperFirst(strings.ToLower(out))
	case CaseTitle:
		out = titleWords(out)
	}
	if s.NameSpaces != SpacesKeep {
		out = strings.ReplaceAll(out, " ", s.NameSpaces)
	}
	return out
}

// titleWords capitalises every word; "_" and "-" also start a new word, so
// "test_switch" becomes "Test_Switch" just like "test switch".
func titleWords(s string) string {
	var b strings.Builder
	start := true
	for _, r := range strings.ToLower(s) {
		if start {
			r = unicode.ToUpper(r)
		}
		start = r == ' ' || r == '_' || r == '-'
		b.WriteRune(r)
	}
	return b.String()
}

func upperFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}
