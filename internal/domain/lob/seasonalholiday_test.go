package lob_test

import (
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

func TestSeasonalHolidayWindow(t *testing.T) {
	tests := []struct {
		hemisphere lob.Hemisphere
		date       shared.Date
		want       bool
	}{
		{lob.Northern, shared.NewDate(2001, time.July, 14), false},
		{lob.Northern, shared.NewDate(2001, time.July, 15), true},
		{lob.Northern, shared.NewDate(2001, time.August, 14), true},
		{lob.Northern, shared.NewDate(2001, time.August, 15), false},
		{lob.Northern, shared.NewDate(2001, time.December, 20), false},
		{lob.Southern, shared.NewDate(2001, time.December, 14), false},
		{lob.Southern, shared.NewDate(2001, time.December, 15), true},
		{lob.Southern, shared.NewDate(2001, time.December, 31), true},
		{lob.Southern, shared.NewDate(2002, time.January, 1), true},
		{lob.Southern, shared.NewDate(2002, time.January, 14), true},
		{lob.Southern, shared.NewDate(2002, time.January, 15), false},
		{lob.Southern, shared.NewDate(2001, time.July, 20), false},
		{lob.NoHoliday, shared.NewDate(2001, time.July, 20), false},
		{"", shared.NewDate(2001, time.December, 20), false},
	}
	for _, tt := range tests {
		p := lob.SeasonalHolidayParams{Hemisphere: tt.hemisphere}
		if got := p.InWindow(tt.date); got != tt.want {
			t.Errorf("%q InWindow(%v) = %v, want %v", tt.hemisphere, tt.date, got, tt.want)
		}
	}
}

func TestSeasonalHolidayDefer(t *testing.T) {
	p := lob.SeasonalHolidayParams{Hemisphere: lob.Northern}
	in := shared.NewDate(2001, time.July, 31)
	out := shared.NewDate(2001, time.June, 30)
	tests := []struct {
		date  shared.Date
		u     float64
		share float64
		want  shared.Date
	}{
		{in, 0.29, 0.3, shared.NewDate(2001, time.August, 31)},
		{in, 0.3, 0.3, in},
		{in, 0, 0, in},
		{out, 0, 1, out},
	}
	for _, tt := range tests {
		if got := p.Defer(tt.date, tt.u, tt.share); got != tt.want {
			t.Errorf("Defer(%v, %v, %v) = %v, want %v", tt.date, tt.u, tt.share, got, tt.want)
		}
	}
}

func TestSeasonalHolidayDeferredDatesLeaveTheWindow(t *testing.T) {
	for _, h := range []lob.Hemisphere{lob.Northern, lob.Southern} {
		p := lob.SeasonalHolidayParams{Hemisphere: h}
		for d := shared.NewDate(2000, time.January, 1); d.Year() < 2002; d = d.AddDays(1) {
			if moved := p.Defer(d, 0, 1); moved != d && p.InWindow(moved) {
				t.Errorf("%s: %v deferred to %v, still in the window", h, d, moved)
			}
		}
	}
}

func TestSeasonalHolidayEnabled(t *testing.T) {
	for h, want := range map[lob.Hemisphere]bool{"": false, lob.NoHoliday: false, lob.Northern: true, lob.Southern: true} {
		if got := (lob.SeasonalHolidayParams{Hemisphere: h}).Enabled(); got != want {
			t.Errorf("%q Enabled() = %v, want %v", h, got, want)
		}
	}
}
