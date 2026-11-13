// Package textdiff compares two texts line by line (for saved device configs).
package textdiff

import "strings"

// Kinds of a diff line.
const (
	Same    = " "
	Removed = "-"
	Added   = "+"
)

// Line is one line of the result: unchanged, only in the old text or only in
// the new one. OldNo/NewNo are 1-based line numbers (0 = not in that text).
type Line struct {
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	OldNo int    `json:"oldNo,omitempty"`
	NewNo int    `json:"newNo,omitempty"`
}

// maxCells limits the comparison table (rows × columns of the changed middle
// part). Beyond it the middle is reported as replaced as a whole.
const maxCells = 6_000_000

// Lines splits text into lines without line breaks; a trailing break adds no
// empty last line.
func Lines(text string) []string {
	t := strings.ReplaceAll(text, "\r\n", "\n")
	t = strings.TrimSuffix(strings.ReplaceAll(t, "\r", "\n"), "\n")
	if t == "" {
		return nil
	}
	return strings.Split(t, "\n")
}

// Diff returns the lines of both texts in order, marked as same, removed
// (only in old) or added (only in new). The result is a shortest edit script
// unless the texts differ so much that the table would get too big.
func Diff(old, new []string) []Line {
	// Equal start and end need no table.
	start := 0
	for start < len(old) && start < len(new) && old[start] == new[start] {
		start++
	}
	end := 0
	for end < len(old)-start && end < len(new)-start && old[len(old)-1-end] == new[len(new)-1-end] {
		end++
	}
	out := make([]Line, 0, len(old)+len(new)-start-end)
	for i := 0; i < start; i++ {
		out = append(out, Line{Kind: Same, Text: old[i], OldNo: i + 1, NewNo: i + 1})
	}
	a, b := old[start:len(old)-end], new[start:len(new)-end]
	out = append(out, middle(a, b, start)...)
	for i := 0; i < end; i++ {
		o, n := len(old)-end+i, len(new)-end+i
		out = append(out, Line{Kind: Same, Text: old[o], OldNo: o + 1, NewNo: n + 1})
	}
	return out
}

// middle diffs the part between the common start and end; offset is the
// number of lines before it in both texts.
func middle(a, b []string, offset int) []Line {
	if len(a)*len(b) > maxCells {
		out := make([]Line, 0, len(a)+len(b))
		for i, t := range a {
			out = append(out, Line{Kind: Removed, Text: t, OldNo: offset + i + 1})
		}
		for j, t := range b {
			out = append(out, Line{Kind: Added, Text: t, NewNo: offset + j + 1})
		}
		return out
	}
	// lcs[i][j] = length of the longest common subsequence of a[i:] and b[j:].
	w := len(b) + 1
	lcs := make([]int32, (len(a)+1)*w)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i*w+j] = lcs[(i+1)*w+j+1] + 1
			} else {
				lcs[i*w+j] = max(lcs[(i+1)*w+j], lcs[i*w+j+1])
			}
		}
	}
	out := make([]Line, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, Line{Kind: Same, Text: a[i], OldNo: offset + i + 1, NewNo: offset + j + 1})
			i++
			j++
		case lcs[(i+1)*w+j] >= lcs[i*w+j+1]:
			out = append(out, Line{Kind: Removed, Text: a[i], OldNo: offset + i + 1})
			i++
		default:
			out = append(out, Line{Kind: Added, Text: b[j], NewNo: offset + j + 1})
			j++
		}
	}
	for ; i < len(a); i++ {
		out = append(out, Line{Kind: Removed, Text: a[i], OldNo: offset + i + 1})
	}
	for ; j < len(b); j++ {
		out = append(out, Line{Kind: Added, Text: b[j], NewNo: offset + j + 1})
	}
	return out
}

// Changed counts removed and added lines.
func Changed(lines []Line) (removed, added int) {
	for _, l := range lines {
		switch l.Kind {
		case Removed:
			removed++
		case Added:
			added++
		}
	}
	return removed, added
}
