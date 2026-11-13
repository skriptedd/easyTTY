package main

import (
	"regexp"
	"strings"

	"easytty/internal/store"
)

// importColors are the editor's swatches, most distinct first.
var importColors = []string{"#4fb3ff", "#3ccf91", "#f2b84b", "#a68bff", "#ff8ad8", "#ff5d6c", "#8a94a6"}

var colorPrefix = regexp.MustCompile(`^[a-zA-Z]+`)

// colorKey groups sessions by the letters their name starts with:
// "con1-cda-r12" → "con", "is02-ntr-000" → "is".
func colorKey(name string) string { return strings.ToLower(colorPrefix.FindString(name)) }

// autoColor gives imported sessions without a colour the colour of their name
// group, so console servers, switches … are told apart at a glance. Groups that
// already have a colour among the existing sessions keep it.
func autoColor(existing, imported []store.Session) []store.Session {
	byKey := map[string]string{}
	used := map[string]bool{}
	for _, s := range existing {
		if k := colorKey(s.Name); k != "" && s.Color != "" {
			if _, ok := byKey[k]; !ok {
				byKey[k] = s.Color
			}
			used[s.Color] = true
		}
	}
	next := 0
	pick := func() string {
		for i := range importColors { // an unused colour first, then round robin
			if c := importColors[(next+i)%len(importColors)]; !used[c] {
				next = (next + i + 1) % len(importColors)
				return c
			}
		}
		c := importColors[next]
		next = (next + 1) % len(importColors)
		return c
	}
	out := make([]store.Session, len(imported))
	for i, s := range imported {
		if k := colorKey(s.Name); s.Color == "" && k != "" {
			c, ok := byKey[k]
			if !ok {
				c = pick()
				byKey[k], used[c] = c, true
			}
			s.Color = c
		}
		out[i] = s
	}
	return out
}
