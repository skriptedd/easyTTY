package update

import (
	"strconv"
	"strings"
)

// version is major.minor.patch; pre marks builds like 0.2.13-dev, which sort
// before the release of the same number.
type version struct {
	num [3]int
	pre bool
}

func parse(s string) (version, bool) {
	var v version
	core, _, pre := strings.Cut(strings.TrimPrefix(s, "v"), "-")
	v.pre = pre
	parts := strings.Split(core, ".")
	if len(parts) != len(v.num) {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || len(p) > 6 {
			return v, false
		}
		v.num[i] = n
	}
	return v, true
}

// Newer reports whether candidate is a later version than current. Versions
// that cannot be read are never newer.
func Newer(current, candidate string) bool {
	cur, ok1 := parse(current)
	cand, ok2 := parse(candidate)
	if !ok1 || !ok2 {
		return false
	}
	for i := range cur.num {
		if cand.num[i] != cur.num[i] {
			return cand.num[i] > cur.num[i]
		}
	}
	return cur.pre && !cand.pre
}
