package approval

import (
	"regexp"
	"strings"

	"easytty/internal/automation"
)

var (
	// showCmd: the only commands that may skip approval (Cisco/H3C/Huawei style).
	// "sh"/"dis" abbreviations are not accepted: on Linux "sh -c …" is a shell.
	showCmd = regexp.MustCompile(`(?i)^\s*(show|display)(\s|$)`)
	// pipeFilter: output filters allowed after "|".
	pipeFilter = regexp.MustCompile(`(?i)^\s*(include|inc|i|exclude|exc|e|begin|beg|b|section|sec|count)(\s|$)`)
)

// AllReadOnly reports whether every command is show/display, without shell
// metacharacters or options, and pipes only into output filters (no
// redirect/append/tee/more).
func AllReadOnly(cmds []string) bool {
	if len(cmds) == 0 {
		return false
	}
	for _, c := range cmds {
		if automation.HasHiddenChars(c) || strings.ContainsAny(c, ";&`$()<>\\") {
			return false
		}
		parts := strings.Split(c, "|")
		if !showCmd.MatchString(parts[0]) {
			return false
		}
		// Options make no sense on a switch but do on a Linux host ("display -write f").
		if strings.Contains(parts[0], " -") {
			return false
		}
		for _, p := range parts[1:] {
			if !pipeFilter.MatchString(p) {
				return false
			}
		}
	}
	return true
}
