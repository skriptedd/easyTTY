package transfer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/pkg/sftp"
)

// Limits of one search: a remote tree can be huge, the answer has to come back.
const (
	FindMaxResults = 1000
	FindMaxDirs    = 20000
)

// Why a search stopped before the whole tree was read.
const (
	StopResults   = "results"   // FindMaxResults hits
	StopDirs      = "dirs"      // FindMaxDirs folders read
	StopCancelled = "cancelled" // cancelled or timed out
)

// FindResult is what a search found below Dir.
type FindResult struct {
	Dir     string  `json:"dir"`
	Entries []Entry `json:"entries"`
	Dirs    int     `json:"dirs"`    // folders read
	Skipped int     `json:"skipped"` // folders that could not be read
	Stopped string  `json:"stopped"` // "" = searched everything
}

// Matcher tests a file name like the filter of the file list: every word
// must appear (case-insensitive); a word with * or ? must match the whole name.
type Matcher func(name string) bool

// NewMatcher builds a Matcher; an empty query is an error.
func NewMatcher(query string) (Matcher, error) {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil, errors.New("Suchbegriff fehlt")
	}
	tests := make([]func(string) bool, 0, len(words))
	for _, w := range words {
		if !strings.ContainsAny(w, "*?") {
			tests = append(tests, func(n string) bool { return strings.Contains(n, w) })
			continue
		}
		pattern := regexp.QuoteMeta(w)
		pattern = strings.ReplaceAll(pattern, `\*`, ".*")
		pattern = strings.ReplaceAll(pattern, `\?`, ".")
		re := regexp.MustCompile("^" + pattern + "$")
		tests = append(tests, re.MatchString)
	}
	return func(name string) bool {
		n := strings.ToLower(name)
		for _, t := range tests {
			if !t(n) {
				return false
			}
		}
		return true
	}, nil
}

// Find searches dir and all folders below it, nearest first. Links to
// folders are listed but not entered (no loops). Unreadable folders are
// counted and skipped. On cancel or a limit it returns what it found so far.
func Find(ctx context.Context, c *sftp.Client, dir, query string) (FindResult, error) {
	match, err := NewMatcher(query)
	if err != nil {
		return FindResult{}, err
	}
	if dir == "" {
		dir = "."
	}
	root, err := c.RealPath(dir)
	if err != nil {
		return FindResult{}, fmt.Errorf("%s: %w", dir, err)
	}
	res := FindResult{Dir: root, Entries: []Entry{}}
	queue := []string{root}
	for len(queue) > 0 {
		if ctx.Err() != nil {
			res.Stopped = StopCancelled
			break
		}
		if res.Dirs >= FindMaxDirs {
			res.Stopped = StopDirs
			break
		}
		cur := queue[0]
		queue = queue[1:]
		infos, err := c.ReadDir(cur)
		if err != nil {
			if cur == root {
				return res, fmt.Errorf("%s: %w", root, err)
			}
			res.Skipped++
			continue
		}
		res.Dirs++
		for _, fi := range infos {
			p := path.Join(cur, fi.Name())
			isLink := fi.Mode()&os.ModeSymlink != 0
			if fi.IsDir() && !isLink {
				queue = append(queue, p)
			}
			if !match(fi.Name()) {
				continue
			}
			res.Entries = append(res.Entries, Entry{
				Name: fi.Name(), Path: p, Size: fi.Size(), Mode: fi.Mode().String(),
				ModTime: fi.ModTime().UnixMilli(), IsDir: fi.IsDir(), IsLink: isLink,
			})
			if len(res.Entries) >= FindMaxResults {
				res.Stopped = StopResults
				return res, nil
			}
		}
	}
	return res, nil
}
