package gtfs

import (
	"fmt"
	"math"
	"strings"
)

// Secs is a GTFS time: seconds since service-day midnight (strictly, noon
// minus 12h). It may exceed 86400 for trips that run past midnight.
type Secs int32

// ParseSecs parses a GTFS "H+:MM:SS" time. Hours may be one or more digits and
// may be 24 or more; minutes and seconds are exactly two digits below 60.
// Surrounding whitespace is ignored.
func ParseSecs(s string) (Secs, error) {
	s = strings.TrimSpace(s)
	h, rest, ok1 := strings.Cut(s, ":")
	m, sec, ok2 := strings.Cut(rest, ":")
	if !ok1 || !ok2 || len(h) == 0 || len(h) > 6 || len(m) != 2 || len(sec) != 2 {
		return 0, fmt.Errorf("gtfs time %q: want H:MM:SS", s)
	}
	hv, okh := digits(h)
	mv, okm := digits(m)
	sv, oks := digits(sec)
	if !okh || !okm || !oks {
		return 0, fmt.Errorf("gtfs time %q: non-digit", s)
	}
	if mv >= 60 || sv >= 60 {
		return 0, fmt.Errorf("gtfs time %q: minutes and seconds must be below 60", s)
	}
	total := hv*3600 + mv*60 + sv
	if total > math.MaxInt32 {
		return 0, fmt.Errorf("gtfs time %q: out of range", s)
	}
	return Secs(total), nil
}

// digits parses an all-ASCII-digit string; at most 6 digits, so no overflow.
func digits(s string) (int64, bool) {
	var v int64
	for i := range len(s) {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + int64(c-'0')
	}
	return v, true
}

// String formats s as HH:MM:SS, with hours past 23 kept as-is.
func (s Secs) String() string {
	sign := ""
	v := int64(s)
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%02d:%02d:%02d", sign, v/3600, v/60%60, v%60)
}
