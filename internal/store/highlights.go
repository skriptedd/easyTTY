package store

import "strings"

// Highlight colours matching words or patterns in terminal output, e.g. IP
// addresses, MACs or "err-disabled". The UI applies the rules; the backend
// only keeps them tidy.
type Highlight struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`    // "words" (comma list, whole words, any case) or "regex" (JavaScript syntax)
	Pattern string `json:"pattern"` // the words or the expression
	Color   string `json:"color"`   // one of HighlightColors
	Off     bool   `json:"off"`     // switched off, kept in the list
}

// Highlight kinds.
const (
	HighlightWords = "words"
	HighlightRegex = "regex"
)

// HighlightColors are the colours a rule may use; the terminal theme decides
// the exact shade, so they fit dark and light.
var HighlightColors = []string{"red", "green", "yellow", "blue", "magenta", "cyan"}

const (
	maxHighlights       = 50
	maxHighlightName    = 40  // runes
	maxHighlightPattern = 500 // bytes; long expressions get slow on every line
)

// DefaultHighlights are the rules of a fresh installation.
func DefaultHighlights() []Highlight {
	return []Highlight{
		{Name: "IP-Adressen", Kind: HighlightRegex, Color: "cyan",
			// No digit or dot right before or after: a version like 15.2.7.4.1 is no IP.
			Pattern: `(?<![\d.])(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}(?:\/\d{1,2})?(?!\.?\d)`},
		{Name: "MAC-Adressen", Kind: HighlightRegex, Color: "magenta",
			Pattern: `\b(?:[0-9a-fA-F]{2}[:-]){5}[0-9a-fA-F]{2}\b|\b[0-9a-fA-F]{4}\.[0-9a-fA-F]{4}\.[0-9a-fA-F]{4}\b`},
		{Name: "Fehler", Kind: HighlightWords, Color: "red",
			Pattern: "down, err-disabled, notconnect, error, failed, denied, invalid"},
		{Name: "In Ordnung", Kind: HighlightWords, Color: "green",
			Pattern: "up, connected"},
	}
}

// cleanHighlights trims and drops broken rules. nil (never set) stays nil so
// the defaults apply; an empty list the user saved stays empty.
func cleanHighlights(in []Highlight) []Highlight {
	if in == nil {
		return nil
	}
	out := make([]Highlight, 0, min(len(in), maxHighlights))
	for _, h := range in {
		p := strings.TrimSpace(stripControl(h.Pattern, false))
		if p == "" || len(p) > maxHighlightPattern {
			continue
		}
		if h.Kind != HighlightRegex {
			h.Kind = HighlightWords
		}
		if !validColor(h.Color) {
			h.Color = HighlightColors[0]
		}
		name := []rune(strings.TrimSpace(stripControl(h.Name, false)))
		if len(name) > maxHighlightName {
			name = name[:maxHighlightName]
		}
		out = append(out, Highlight{Name: string(name), Kind: h.Kind, Pattern: p, Color: h.Color, Off: h.Off})
		if len(out) == maxHighlights {
			break
		}
	}
	return out
}

func validColor(c string) bool {
	for _, x := range HighlightColors {
		if c == x {
			return true
		}
	}
	return false
}
