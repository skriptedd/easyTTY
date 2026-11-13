package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"easytty/internal/fsutil"
)

// Settings are global UI/terminal preferences.
type Settings struct {
	FontFamily      string `json:"fontFamily"`
	FontSize        int    `json:"fontSize"`
	Theme           string `json:"theme"`
	Scrollback      int    `json:"scrollback"`
	CopyOnSelect    bool   `json:"copyOnSelect"`
	RightClickPaste bool   `json:"rightClickPaste"`
	CursorBlink     bool   `json:"cursorBlink"`
	ConfirmClose    bool   `json:"confirmClose"`

	// Automatic session logging (set up in the first-run dialog).
	LogEnabled bool   `json:"logEnabled"`
	LogDir     string `json:"logDir"` // "" = app default (Dokumente\easyTTY\Logs)
	LogRaw     bool   `json:"logRaw"` // true = raw bytes incl. colour codes, no timestamps
	// ConfigDir keeps saved running-configs ("" = Dokumente\easyTTY\Configs).
	ConfigDir    string `json:"configDir"`
	FirstRunDone bool   `json:"firstRunDone"`

	// Pasting multi-line text (e.g. a running-config).
	PasteConfirm   bool `json:"pasteConfirm"`   // ask before pasting more than one line
	PasteLineDelay int  `json:"pasteLineDelay"` // extra ms after each line in paced mode

	// Naming scheme applied whenever a session is saved (see FormatName).
	NameCase   string `json:"nameCase"`   // "" | lower | upper | capitalize | title
	NameSpaces string `json:"nameSpaces"` // "" | "_" | "-"

	// SerialWake sends one Enter after a COM port opens, so a console shows its prompt.
	SerialWake bool `json:"serialWake"`

	// AltShortcuts enables the Alt+key layer (Omarchy style). Off = every Alt
	// combo goes to the terminal (bash uses Alt+b/f/d for word movement).
	AltShortcuts bool `json:"altShortcuts"`

	// MCP: lets AI assistants (Claude …) use easyTTY over http://127.0.0.1:<McpPort>/mcp.
	McpEnabled  bool   `json:"mcpEnabled"`
	McpPort     int    `json:"mcpPort"`
	McpToken    string `json:"mcpToken"`    // bearer token, generated on first enable
	McpShowAuto bool   `json:"mcpShowAuto"` // read-only show/display commands without approval
	McpReadTabs bool   `json:"mcpReadTabs"` // AI may read the output of open tabs (may contain secrets)

	// TypeSpeed for commands typed into visible tabs (AI, "Jetzt ausführen"):
	// "" = readable (default), "fast", "slow".
	TypeSpeed string `json:"typeSpeed"`

	// UpdateCheck asks the release server for a newer version at every start.
	UpdateCheck bool `json:"updateCheck"`

	// Snippets are commands sent to the active tab with one click (Alt+J).
	Snippets []Snippet `json:"snippets"`

	// Highlights colour words and patterns in terminal output (IPs, MACs …).
	Highlights []Highlight `json:"highlights"`
}

// Snippet is a named command or block of commands.
type Snippet struct {
	Name string `json:"name"`
	Text string `json:"text"`
	// SaveOutput runs the commands one by one and writes their output to a
	// file in the config folder (instead of only typing them).
	SaveOutput bool `json:"saveOutput,omitempty"`
}

// DefaultMcpPort is where the MCP endpoint listens (127.0.0.1 only).
const DefaultMcpPort = 47321

const (
	minFontSize       = 8
	maxFontSize       = 32
	maxScrollback     = 1_000_000
	maxPasteLineDelay = 2000

	maxSnippets     = 200
	maxSnippetName  = 60   // runes
	maxSnippetBytes = 8000 // a long config block still fits
)

// DefaultSettings returns the out-of-the-box preferences.
func DefaultSettings() Settings {
	return Settings{
		FontFamily:      "Cascadia Mono, Consolas, monospace",
		FontSize:        14,
		Theme:           "dark",
		Scrollback:      20000,
		CopyOnSelect:    true,
		RightClickPaste: true,
		CursorBlink:     true,
		ConfirmClose:    true,
		LogEnabled:      true,
		PasteConfirm:    true,
		SerialWake:      true,
		AltShortcuts:    true,
		McpPort:         DefaultMcpPort,
		UpdateCheck:     true,
		Highlights:      DefaultHighlights(),
	}
}

// Sanitize clamps values into sensible ranges.
func (s Settings) Sanitize() Settings {
	d := DefaultSettings()
	out := s
	if out.FontFamily == "" {
		out.FontFamily = d.FontFamily
	}
	if out.FontSize < minFontSize || out.FontSize > maxFontSize {
		out.FontSize = d.FontSize
	}
	if out.Theme != "dark" && out.Theme != "light" {
		out.Theme = d.Theme
	}
	if out.Scrollback < 0 || out.Scrollback > maxScrollback {
		out.Scrollback = d.Scrollback
	}
	out.LogDir = strings.TrimSpace(out.LogDir)
	out.ConfigDir = strings.TrimSpace(out.ConfigDir)
	out.PasteLineDelay = min(max(out.PasteLineDelay, 0), maxPasteLineDelay)
	if out.McpPort < 1024 || out.McpPort > 65535 {
		out.McpPort = DefaultMcpPort
	}
	out.McpToken = strings.TrimSpace(out.McpToken)
	if out.TypeSpeed != "fast" && out.TypeSpeed != "slow" {
		out.TypeSpeed = ""
	}
	if !ValidNaming(out.NameCase, out.NameSpaces) {
		out.NameCase, out.NameSpaces = CaseKeep, SpacesKeep
	}
	out.Snippets = cleanSnippets(out.Snippets)
	out.Highlights = cleanHighlights(out.Highlights)
	return out
}

// cleanSnippets drops empty and oversized entries and control characters
// (a command must not carry Esc or Ctrl+C); line breaks and tabs stay.
func cleanSnippets(in []Snippet) []Snippet {
	out := make([]Snippet, 0, min(len(in), maxSnippets))
	for _, sn := range in {
		text := strings.TrimRight(strings.ReplaceAll(stripControl(sn.Text, true), "\r\n", "\n"), "\n \t")
		if strings.TrimSpace(text) == "" || len(text) > maxSnippetBytes {
			continue
		}
		name := []rune(strings.TrimSpace(stripControl(sn.Name, false)))
		if len(name) > maxSnippetName {
			name = name[:maxSnippetName]
		}
		if len(name) == 0 {
			name = []rune(firstLine(text, maxSnippetName))
		}
		out = append(out, Snippet{Name: string(name), Text: text, SaveOutput: sn.SaveOutput})
		if len(out) == maxSnippets {
			break
		}
	}
	if len(out) == 0 {
		return nil // same as a settings file without snippets
	}
	return out
}

func stripControl(s string, keepLines bool) string {
	return strings.Map(func(r rune) rune {
		switch {
		case keepLines && (r == '\n' || r == '\r' || r == '\t'):
			return r
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == 0x200b || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069):
			return -1
		}
		return r
	}, s)
}

func firstLine(s string, maxRunes int) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	r := []rune(strings.TrimSpace(line))
	if len(r) > maxRunes {
		return string(r[:maxRunes])
	}
	return string(r)
}

// MergeSnippets adds the entries of extra that local does not have yet.
func MergeSnippets(local, extra []Snippet) []Snippet {
	out := append([]Snippet(nil), local...)
	have := map[Snippet]bool{}
	for _, sn := range local {
		have[sn] = true
	}
	for _, sn := range extra {
		if !have[sn] {
			have[sn] = true
			out = append(out, sn)
		}
	}
	return out
}

// LoadSettings reads settings from path, falling back to defaults.
func LoadSettings(path string) (Settings, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return DefaultSettings(), err
	}
	s := DefaultSettings()
	if err := json.Unmarshal(raw, &s); err != nil {
		s = DefaultSettings()
		bak, berr := os.ReadFile(path + ".bak")
		if berr != nil || json.Unmarshal(bak, &s) != nil {
			return DefaultSettings(), fmt.Errorf("Einstellungen beschädigt: %w", err)
		}
		_ = os.WriteFile(path+".damaged", raw, 0o600) // kept for inspection
	}
	return s.Sanitize(), nil
}

// SaveSettings writes settings atomically.
func SaveSettings(path string, s Settings) (Settings, error) {
	clean := s.Sanitize()
	raw, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return Settings{}, err
	}
	if err := fsutil.WriteAtomic(path, raw); err != nil {
		return Settings{}, err
	}
	return clean, nil
}
