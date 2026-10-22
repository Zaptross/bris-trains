// Package stationlex resolves free-text station queries ("central", "roma st",
// "indooroopily") to rail stations using tiered matching: exact or alias,
// prefix, word-boundary substring, then bounded edit distance (SPEC §3.7).
package stationlex

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"
)

// Entry is a resolvable station: its stop ID and exact feed name, e.g. "Central station".
type Entry struct {
	ID   string
	Name string
}

// DefaultAliases maps alias query -> target station Name (exact feed name).
var DefaultAliases = map[string]string{
	"roma st":       "Roma Street station",
	"fortitude":     "Fortitude Valley station",
	"indro":         "Indooroopilly station",
	"domestic":      "Domestic Airport station",
	"int airport":   "International Airport station",
	"international": "International Airport station",
	"park road":     "Boggo Road station",
	"the valley":    "Fortitude Valley station",
}

// maxFuzzyDistance is the largest Levenshtein distance accepted by the fuzzy tier.
const maxFuzzyDistance = 2

// maxSuggestions caps UnknownError.Suggestions.
const maxSuggestions = 3

type station struct {
	entry Entry
	norm  string   // Normalize(entry.Name)
	words []string // strings.Fields(norm)
}

type alias struct {
	query  string // alias as supplied
	target string // target station Name as supplied
	idx    int    // index into Lexicon.stations, -1 if target missing
}

// Lexicon resolves station queries against a fixed set of entries and aliases.
// It is immutable after New and safe for concurrent use.
type Lexicon struct {
	stations []station
	aliases  map[string][]alias // keyed by Normalize(alias query)
}

// New builds a Lexicon. Aliases map an alias query to a target station Name;
// aliases whose target name is missing are ignored by Resolve and reported by Validate.
func New(entries []Entry, aliases map[string]string) *Lexicon {
	l := &Lexicon{
		stations: make([]station, len(entries)),
		aliases:  make(map[string][]alias, len(aliases)),
	}
	byName := make(map[string]int, len(entries))
	for i, e := range entries {
		n := Normalize(e.Name)
		l.stations[i] = station{entry: e, norm: n, words: strings.Fields(n)}
		if _, dup := byName[e.Name]; !dup {
			byName[e.Name] = i
		}
	}
	for _, q := range slices.Sorted(maps.Keys(aliases)) {
		target := aliases[q]
		idx, ok := byName[target]
		if !ok {
			idx = -1
		}
		k := Normalize(q)
		l.aliases[k] = append(l.aliases[k], alias{query: q, target: target, idx: idx})
	}
	return l
}

// diacritics folds common lowercase Latin letters with diacritics to ASCII.
var diacritics = map[rune]string{
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'ā': "a",
	'æ': "ae",
	'ç': "c", 'č': "c",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ē': "e",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ī': "i",
	'ñ': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ø': "o", 'ō': "o",
	'œ': "oe",
	'š': "s", 'ß': "ss",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ū': "u",
	'ý': "y", 'ÿ': "y",
	'ž': "z",
}

// Normalize returns the canonical matching form of s: lowercased, common Latin
// diacritics folded to ASCII, punctuation replaced by spaces, whitespace
// collapsed and trimmed, and a trailing " station" word removed.
func Normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		switch {
		case diacritics[r] != "":
			b.WriteString(diacritics[r])
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	n := strings.Join(strings.Fields(b.String()), " ")
	if base, ok := strings.CutSuffix(n, " station"); ok {
		n = base
	}
	return n
}

// Resolve maps query to exactly one station. It returns *AmbiguousError when
// the first non-empty tier has several stations and *UnknownError when no tier matches.
func (l *Lexicon) Resolve(query string) (Entry, error) {
	q := Normalize(query)
	if q == "" {
		return Entry{}, &UnknownError{Query: query}
	}
	qWords := len(strings.Fields(q))

	tiers := []func(s *station) bool{
		// Tier 1: exact name (aliases are added below).
		func(s *station) bool { return s.norm == q },
		// Tier 2: name prefix.
		func(s *station) bool { return strings.HasPrefix(s.norm, q) },
		// Tier 3: substring starting at a word boundary.
		func(s *station) bool { return wordBoundaryContains(s.norm, q) },
		// Tier 4: bounded edit distance.
		func(s *station) bool { return fuzzyDistance(q, qWords, s) <= maxFuzzyDistance },
	}
	for t, match := range tiers {
		var hits []int
		for i := range l.stations {
			if match(&l.stations[i]) {
				hits = append(hits, i)
			}
		}
		if t == 0 {
			for _, a := range l.aliases[q] {
				if a.idx >= 0 {
					hits = append(hits, a.idx)
				}
			}
		}
		if entries := l.distinct(hits); len(entries) == 1 {
			return entries[0], nil
		} else if len(entries) > 1 {
			return Entry{}, &AmbiguousError{Query: query, Candidates: entries}
		}
	}
	return Entry{}, &UnknownError{Query: query, Suggestions: l.suggest(q, qWords)}
}

// distinct returns the entries at idxs, deduplicated by ID and sorted by Name.
func (l *Lexicon) distinct(idxs []int) []Entry {
	var out []Entry
	seen := make(map[string]bool, len(idxs))
	for _, i := range idxs {
		e := l.stations[i].entry
		if !seen[e.ID] {
			seen[e.ID] = true
			out = append(out, e)
		}
	}
	slices.SortFunc(out, compareEntries)
	return out
}

func compareEntries(a, b Entry) int {
	return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
}

// suggest returns up to maxSuggestions stations nearest to q by fuzzy distance.
func (l *Lexicon) suggest(q string, qWords int) []Entry {
	type scored struct {
		e Entry
		d int
	}
	all := make([]scored, 0, len(l.stations))
	for i := range l.stations {
		s := &l.stations[i]
		all = append(all, scored{s.entry, fuzzyDistance(q, qWords, s)})
	}
	slices.SortFunc(all, func(a, b scored) int {
		return cmp.Or(cmp.Compare(a.d, b.d), compareEntries(a.e, b.e))
	})
	out := make([]Entry, 0, min(maxSuggestions, len(all)))
	for _, s := range all[:min(maxSuggestions, len(all))] {
		out = append(out, s.e)
	}
	return out
}

// wordBoundaryContains reports whether q occurs in name starting at a word boundary.
func wordBoundaryContains(name, q string) bool {
	for off := 0; ; {
		i := strings.Index(name[off:], q)
		if i < 0 {
			return false
		}
		i += off
		if i == 0 || name[i-1] == ' ' {
			return true
		}
		off = i + 1
	}
}

// fuzzyDistance is the smaller Levenshtein distance from q to the station's
// full normalised name and to its first qWords words.
func fuzzyDistance(q string, qWords int, s *station) int {
	d := levenshtein(q, s.norm)
	if qWords < len(s.words) {
		d = min(d, levenshtein(q, strings.Join(s.words[:qWords], " ")))
	}
	return d
}

// levenshtein returns the rune-wise edit distance between a and b.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := range ra {
		cur[0] = i + 1
		for j := range rb {
			cost := 1
			if ra[i] == rb[j] {
				cost = 0
			}
			cur[j+1] = min(prev[j+1]+1, cur[j]+1, prev[j]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// Aliases returns the alias queries that resolve to the station with the given ID, sorted.
func (l *Lexicon) Aliases(id string) []string {
	var out []string
	for _, as := range l.aliases {
		for _, a := range as {
			if a.idx >= 0 && l.stations[a.idx].entry.ID == id {
				out = append(out, a.query)
			}
		}
	}
	slices.Sort(out)
	return out
}

// Validate returns human-readable warnings: alias targets that do not exist; each required name that does not
// match exactly one entry by exact Name; any entry or alias whose Normalize()d form equals Normalize(reserved word).
func (l *Lexicon) Validate(required []string, reserved []string) []string {
	var warns []string
	normAliases := slices.Sorted(maps.Keys(l.aliases))
	for _, k := range normAliases {
		for _, a := range l.aliases[k] {
			if a.idx < 0 {
				warns = append(warns, fmt.Sprintf("alias %q targets unknown station %q", a.query, a.target))
			}
		}
	}
	for _, name := range required {
		n := 0
		for i := range l.stations {
			if l.stations[i].entry.Name == name {
				n++
			}
		}
		if n != 1 {
			warns = append(warns, fmt.Sprintf("required station %q matches %d stations, want exactly 1", name, n))
		}
	}
	for _, word := range reserved {
		rw := Normalize(word)
		for i := range l.stations {
			if e := l.stations[i].entry; l.stations[i].norm == rw {
				warns = append(warns, fmt.Sprintf("station %q (%s) collides with reserved word %q", e.Name, e.ID, word))
			}
		}
		for _, a := range l.aliases[rw] {
			warns = append(warns, fmt.Sprintf("alias %q collides with reserved word %q", a.query, word))
		}
	}
	return warns
}

// AmbiguousError reports a query matching several stations in the same tier.
type AmbiguousError struct {
	Query      string
	Candidates []Entry // sorted by Name
}

// Error formats e.g. `ambiguous: "south" matches South Bank station, South Brisbane station`.
func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("ambiguous: %q matches %s", e.Query, strings.Join(names(e.Candidates), ", "))
}

// UnknownError reports a query matching no station.
type UnknownError struct {
	Query       string
	Suggestions []Entry // up to 3 nearest by edit distance
}

// Error formats e.g. `unknown station "xyz"; did you mean A, B or C?`.
func (e *UnknownError) Error() string {
	msg := fmt.Sprintf("unknown station %q", e.Query)
	ns := names(e.Suggestions)
	switch len(ns) {
	case 0:
		return msg
	case 1:
		return msg + "; did you mean " + ns[0] + "?"
	default:
		return msg + "; did you mean " + strings.Join(ns[:len(ns)-1], ", ") + " or " + ns[len(ns)-1] + "?"
	}
}

func names(es []Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name
	}
	return out
}
