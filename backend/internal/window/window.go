// Package window gives instance.maintenance_window just enough semantics
// to warn (SPEC-023, resolving O-3): a weekly `Day HH:MM-HH:MM` window,
// evaluated in server-local time. Pure functions — the one parser behind
// both the audit stamp and the instance read model (mini-ADR 2). Windows
// warn, never block (D6): callers treat every parse failure as "no window".
package window

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrUnparseable wraps every rejection; the raw text stays out of the
// error (callers log it alongside the instance name once, not per use).
var ErrUnparseable = errors.New("window: unparseable")

var days = map[string]time.Weekday{
	"mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
	"sun": time.Sunday,
}

// Window is one weekly maintenance window. Start and End are minutes since
// midnight; Start is inclusive, End exclusive. End <= Start means the
// window wraps past midnight into the following day (the fixture ships
// `Sat 22:00-02:00` — wrap is a shipped shape, not an edge case).
type Window struct {
	Day        time.Weekday
	Start, End int
}

// Parse reads the `Day HH:MM-HH:MM` grammar (SPEC-023 mini-ADR 1):
// case-insensitive 3-letter day, 24h minute-precision times. A zero-length
// window (end == start) is rejected — reading a typo as a 24h window would
// be a surprising gift.
func Parse(raw string) (Window, error) {
	day, times, ok := strings.Cut(strings.TrimSpace(raw), " ")
	if !ok {
		return Window{}, fmt.Errorf("%w: want \"Day HH:MM-HH:MM\"", ErrUnparseable)
	}
	weekday, ok := days[strings.ToLower(day)]
	if !ok {
		return Window{}, fmt.Errorf("%w: unknown day %q", ErrUnparseable, day)
	}
	from, to, ok := strings.Cut(times, "-")
	if !ok {
		return Window{}, fmt.Errorf("%w: want \"HH:MM-HH:MM\"", ErrUnparseable)
	}
	start, err := parseMinutes(from)
	if err != nil {
		return Window{}, err
	}
	end, err := parseMinutes(to)
	if err != nil {
		return Window{}, err
	}
	if start == end {
		return Window{}, fmt.Errorf("%w: zero-length window", ErrUnparseable)
	}
	return Window{Day: weekday, Start: start, End: end}, nil
}

// Contains reports whether t falls inside the window, wrap-aware. Evaluate
// with server-local time — the portal's one wall clock (mini-ADR 6, same
// clock SPEC-022 pinned for cron).
func (w Window) Contains(t time.Time) bool {
	m := t.Hour()*60 + t.Minute()
	if w.Start < w.End {
		return t.Weekday() == w.Day && m >= w.Start && m < w.End
	}
	// Wrapped: [Start, midnight) on Day, then [midnight, End) the day after.
	return (t.Weekday() == w.Day && m >= w.Start) ||
		(t.Weekday() == (w.Day+1)%7 && m < w.End)
}

// parseMinutes reads a strict zero-padded-or-not `H:MM`/`HH:MM` 24h time
// into minutes since midnight. Hand-rolled: time.Parse layouts are lenient
// in ways an unvalidated CSV field should not enjoy.
func parseMinutes(s string) (int, error) {
	h, m, ok := strings.Cut(s, ":")
	if !ok || len(m) != 2 {
		return 0, fmt.Errorf("%w: bad time %q", ErrUnparseable, s)
	}
	hour, err1 := atoi(h)
	minute, err2 := atoi(m)
	if err1 != nil || err2 != nil || hour > 23 || minute > 59 {
		return 0, fmt.Errorf("%w: bad time %q", ErrUnparseable, s)
	}
	return hour*60 + minute, nil
}

// atoi is a digits-only strconv.Atoi: no signs, no spaces, 1-2 digits.
func atoi(s string) (int, error) {
	if len(s) == 0 || len(s) > 2 {
		return 0, ErrUnparseable
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, ErrUnparseable
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
