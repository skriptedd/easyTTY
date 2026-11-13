// Package automation runs command lists on devices (SSH shell or serial
// console) and collects their output, for scheduled tasks and the MCP API.
package automation

import (
	"strings"
	"unicode/utf8"
)

// CleanText turns raw terminal output into plain text: ANSI/VT escape
// sequences removed, CR/LF normalised, a lone CR restarts the line (pagers
// erase "--More--" that way) and backspaces delete the previous character.
func CleanText(raw []byte) string {
	var out strings.Builder
	line := make([]byte, 0, 256)
	flush := func() {
		out.Write(line)
		out.WriteByte('\n')
		line = line[:0]
	}
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		switch {
		case b == 0x1b:
			i = skipEscape(raw, i)
		case b == '\n':
			flush()
		case b == '\r':
			if i+1 < len(raw) && raw[i+1] == '\n' {
				continue // CRLF: the LF ends the line
			}
			if i+1 < len(raw) {
				line = line[:0] // lone CR: the line gets overwritten
			}
		case b == '\b':
			if len(line) > 0 {
				_, n := utf8.DecodeLastRune(line)
				line = line[:len(line)-n]
			}
		case b == '\t':
			line = append(line, b)
		case b < 0x20 || b == 0x7f:
			// bell and other controls: ignore
		default:
			line = append(line, b)
		}
	}
	out.Write(line)
	return out.String()
}

// skipEscape returns the index of the last byte of the escape sequence at i.
func skipEscape(raw []byte, i int) int {
	if i+1 >= len(raw) {
		return i
	}
	switch raw[i+1] {
	case '[': // CSI: parameters, then a final byte 0x40–0x7e
		for j := i + 2; j < len(raw); j++ {
			if raw[j] >= 0x40 && raw[j] <= 0x7e {
				return j
			}
		}
		return len(raw) - 1
	case ']': // OSC: ends with BEL or ESC \
		for j := i + 2; j < len(raw); j++ {
			if raw[j] == 0x07 {
				return j
			}
			if raw[j] == 0x1b && j+1 < len(raw) && raw[j+1] == '\\' {
				return j + 1
			}
		}
		return len(raw) - 1
	case '(', ')', '#': // charset selection: one more byte
		return min(i+2, len(raw)-1)
	}
	return i + 1
}

// lastLine returns the text after the final newline.
func lastLine(text string) string {
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		return text[i+1:]
	}
	return text
}
