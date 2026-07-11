package window_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/window"
)

// SPEC-023 behavior 1: the fixture's shapes parse, garbage doesn't.
func TestParse(t *testing.T) {
	good := map[string]window.Window{
		"Sat 02:00-06:00":     {Day: time.Saturday, Start: 120, End: 360},
		"Sun 01:00-05:00":     {Day: time.Sunday, Start: 60, End: 300},
		"Sat 22:00-02:00":     {Day: time.Saturday, Start: 1320, End: 120}, // the shipped wrap
		"mon 9:15-17:30":      {Day: time.Monday, Start: 555, End: 1050},   // case + 1-digit hour
		"  Fri 08:00-09:00  ": {Day: time.Friday, Start: 480, End: 540},
	}
	for raw, want := range good {
		got, err := window.Parse(raw)
		require.NoError(t, err, raw)
		require.Equal(t, want, got, raw)
	}

	bad := []string{
		"", "Sat", "Saturday 02:00-06:00", "Sat 02:00", "Sat 02:00–06:00", // en dash
		"Sat 2:0-06:00", "Sat 25:00-26:00", "Sat 02:60-06:00", "Sat -1:00-06:00",
		"Sat 02:00-02:00", // zero-length = typo, not a 24h window
		"Sat 02:00-06:00 weekly",
	}
	for _, raw := range bad {
		_, err := window.Parse(raw)
		require.ErrorIs(t, err, window.ErrUnparseable, "%q must not parse", raw)
	}
}

// SPEC-023 behavior 2: boundary and wrap semantics.
func TestContains(t *testing.T) {
	at := func(day time.Weekday, hh, mm int) time.Time {
		// 2026-07-05 is a Sunday; walk to the wanted weekday.
		base := time.Date(2026, 7, 5, hh, mm, 0, 0, time.Local)
		return base.AddDate(0, 0, int(day-time.Sunday))
	}

	w, err := window.Parse("Sat 02:00-06:00")
	require.NoError(t, err)
	require.True(t, w.Contains(at(time.Saturday, 2, 0)), "start is inclusive")
	require.True(t, w.Contains(at(time.Saturday, 4, 30)))
	require.False(t, w.Contains(at(time.Saturday, 6, 0)), "end is exclusive")
	require.False(t, w.Contains(at(time.Saturday, 1, 59)))
	require.False(t, w.Contains(at(time.Wednesday, 4, 30)), "wrong day")

	wrap, err := window.Parse("Sat 22:00-02:00")
	require.NoError(t, err)
	require.True(t, wrap.Contains(at(time.Saturday, 23, 0)), "before midnight")
	require.True(t, wrap.Contains(at(time.Sunday, 1, 0)), "after midnight, next day")
	require.True(t, wrap.Contains(at(time.Saturday, 22, 0)), "wrapped start inclusive")
	require.False(t, wrap.Contains(at(time.Sunday, 2, 0)), "wrapped end exclusive")
	require.False(t, wrap.Contains(at(time.Sunday, 15, 0)))
	require.False(t, wrap.Contains(at(time.Friday, 23, 0)))

	sunWrap, err := window.Parse("Sun 23:00-01:00")
	require.NoError(t, err)
	require.True(t, sunWrap.Contains(at(time.Monday, 0, 30)), "wrap crosses the week boundary Sun->Mon")
}
