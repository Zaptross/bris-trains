package query

import (
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"trains/gtfsrail"
)

const (
	ansiReset  = "\x1b[0m"
	ansiDim    = "\x1b[2m"
	ansiBold   = "\x1b[1m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
	ansiCyan   = "\x1b[36m"
)

const serviceWidth = 44

// render prints the departure board (SPEC.md §4.2).
func render(w io.Writer, origin, dest gtfsrail.Station, js []gtfsrail.Journey, f gtfsrail.Freshness, at time.Time, color bool) {
	paint := func(code, s string) string {
		if !color || code == "" {
			return s
		}
		return code + s + ansiReset
	}

	when := f.GeneratedAt
	if !at.IsZero() {
		when = at
	}
	freshness := fmt.Sprintf("(live, %s old)", age(f.DataAge))
	if f.Degraded {
		freshness = paint(ansiYellow, "(schedule only — realtime unavailable)")
	}
	title := origin.Name + " → " + dest.Name
	fmt.Fprintf(w, "%s   %s  %s\n\n", paint(ansiBold, title), when.In(gtfsrail.Brisbane).Format("Mon 2 Jan, 15:04 MST"), freshness)

	if len(js) == 0 {
		fmt.Fprintln(w, "  No trains found in the next 24 hours.")
		footer(w, paint)
		return
	}

	fmt.Fprintf(w, "  %-6s  %-4s  %-6s  %-13s  %-*s  %s\n", "DEPART", "PLAT", "ARRIVE", "STATUS", serviceWidth, "SERVICE", "RUN")
	for i := range js {
		j := &js[i]
		first := &j.Legs[0]
		plat := first.Calls[first.Board].Platform
		if plat == "" {
			plat = "—"
		}
		label, code := status(j.Punctuality, first.DelaySeconds)
		runs := make([]string, len(j.Legs))
		for k, l := range j.Legs {
			runs[k] = l.RunNumber
			if runs[k] == "" {
				runs[k] = "—"
			}
		}
		fmt.Fprintf(w, "  %-6s  %-4s  %-6s  %s  %s  %s\n",
			j.Depart().In(gtfsrail.Brisbane).Format("15:04"),
			pad(plat, 4),
			j.Arrive().In(gtfsrail.Brisbane).Format("15:04"),
			paint(code, pad(label, 13)),
			pad(service(j), serviceWidth),
			strings.Join(runs, " → "),
		)
	}
	footer(w, paint)
}

func footer(w io.Writer, paint func(code, s string) string) {
	fmt.Fprintf(w, "\n  %s   %s   %s   %s   %s\n",
		paint(ansiGreen, "● on time"), paint(ansiYellow, "▲ late"), paint(ansiCyan, "▼ early"),
		paint(ansiDim, "○ no realtime"), paint(ansiRed, "✕ cancelled/skipped"))
	fmt.Fprintln(w, "  Data © Translink, CC BY 4.0")
}

// status renders punctuality with the glyphs shared with the UI.
func status(p gtfsrail.Punctuality, delay int32) (string, string) {
	mins := (abs(delay) + 30) / 60
	switch p {
	case gtfsrail.OnTime:
		return "● on time", ansiGreen
	case gtfsrail.Late:
		return fmt.Sprintf("▲ %d min late", max(mins, 1)), ansiYellow
	case gtfsrail.VeryLate:
		return fmt.Sprintf("▲ %d min late", mins), ansiRed
	case gtfsrail.Early:
		return fmt.Sprintf("▼ %d min early", max(mins, 1)), ansiCyan
	case gtfsrail.Skipping:
		return "✕ not stopping", ansiRed
	case gtfsrail.Cancelled:
		return "✕ cancelled", ansiRed
	default:
		return "○ scheduled", ansiDim
	}
}

// service describes a journey: the line and headsign for a direct train, the
// change for a one-change journey.
func service(j *gtfsrail.Journey) string {
	if c := j.Change; c != nil {
		s := fmt.Sprintf("via %s · %d min change", shortName(c.Name), int(c.Buffer.Round(time.Minute)/time.Minute))
		if c.Tight {
			s += " ⚠ tight"
		}
		return s
	}
	l := &j.Legs[0]
	line := strings.ReplaceAll(l.RouteName, " - ", " – ")
	if h := shortName(l.Headsign); h != "" {
		line += " → " + h
	}
	return line
}

func shortName(s string) string { return strings.TrimSuffix(s, " station") }

// pad left-justifies s to width runes, truncating with an ellipsis.
func pad(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n > width {
		r := []rune(s)
		return string(r[:width-1]) + "…"
	}
	return s + strings.Repeat(" ", width-n)
}

func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	default:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
}

func abs(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}
