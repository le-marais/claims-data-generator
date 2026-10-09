package lob

import (
	"fmt"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// Hemisphere names whose summer holiday slows claims handling.
type Hemisphere string

const (
	// NoHoliday switches the seasonal holiday off, as does the empty string.
	NoHoliday Hemisphere = "none"
	// Northern defers from 15 July to 14 August.
	Northern Hemisphere = "northern"
	// Southern defers from 15 December to 14 January, across the year end.
	Southern Hemisphere = "southern"
)

// holidayStartDay is the day of the month the holiday window opens; it
// closes the day before the same day of the next month.
const holidayStartDay = 15

// SeasonalHolidayParams defers a share of the reports, reopens and payments dated in
// the summer holiday window to the same day of the next month, where they
// land on top of that month's own. A deferred date is always outside the
// window, so nothing is deferred twice. The zero value is off.
type SeasonalHolidayParams struct {
	Hemisphere Hemisphere
	// ReportShare is the share of reports, and of reopens, dated in the
	// window that are deferred. A deferred report or reopen moves the rest of
	// the claim, or of its reopen episode, back with it.
	ReportShare float64
	// PaymentShare is the share of payments dated in the window that are
	// deferred: interim payments, and the final settlement with the close
	// date it lands on. A nil close pays nothing and never moves.
	PaymentShare float64
}

// Enabled reports whether a hemisphere is chosen.
func (s SeasonalHolidayParams) Enabled() bool {
	return s.Hemisphere == Northern || s.Hemisphere == Southern
}

// InWindow reports whether d falls in the holiday window: from the 15th of
// the window's first month to the 14th of the next, inclusive.
func (s SeasonalHolidayParams) InWindow(d shared.Date) bool {
	var first time.Month
	switch s.Hemisphere {
	case Northern:
		first = time.July
	case Southern:
		first = time.December
	default:
		return false
	}
	m := d.Month()
	if m.Month() == first {
		return d.Day() >= holidayStartDay
	}
	return m.Add(-1).Month() == first && d.Day() < holidayStartDay
}

// Defer is d moved to the same day of the next month when d is in the window
// and the uniform draw u falls below share, and d otherwise.
func (s SeasonalHolidayParams) Defer(d shared.Date, u, share float64) shared.Date {
	if u < share && s.InWindow(d) {
		return d.AddMonths(1)
	}
	return d
}

func (s SeasonalHolidayParams) validate() error {
	switch s.Hemisphere {
	case "", NoHoliday:
		return nil // switched off: the shares are not read
	case Northern, Southern:
	default:
		return fmt.Errorf("seasonal_holiday.hemisphere: must be %q, %q or %q, got %q", NoHoliday, Northern, Southern, s.Hemisphere)
	}
	if err := checkFinite(
		namedFloat{"seasonal_holiday.report_share", s.ReportShare},
		namedFloat{"seasonal_holiday.payment_share", s.PaymentShare},
	); err != nil {
		return err
	}
	for _, f := range []namedFloat{
		{"seasonal_holiday.report_share", s.ReportShare},
		{"seasonal_holiday.payment_share", s.PaymentShare},
	} {
		if f.v < 0 || f.v > 1 {
			return fmt.Errorf("%s: must be in [0, 1], got %v", f.name, f.v)
		}
	}
	return nil
}
