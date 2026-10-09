// Package calendar holds the business-day calendars claims are processed on:
// weekends plus each market's rule-based public holidays.
package calendar

import (
	"fmt"
	"sync"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// Name values for Lookup.
const (
	None     = "none"
	Weekends = "weekends"
	US       = "us"
	UK       = "uk"
	ZA       = "za"
)

// Calendar says which days are business days. The zero value is off: every
// day is a business day. Holidays are computed by rule per year and cached,
// so a Calendar is safe to share across stages and goroutines.
type Calendar struct {
	rules *rules
}

// rules is a market's holiday rule and its per-year cache.
type rules struct {
	holidays func(year int) []shared.Date
	mu       sync.Mutex
	cache    map[int]map[shared.Date]bool
}

// Lookup returns the named calendar: "" or "none" (off), "weekends", "us"
// (federal holidays), "uk" (England and Wales bank holidays) or "za" (South
// African public holidays). One-off holidays are not modelled.
func Lookup(name string) (Calendar, error) {
	var h func(int) []shared.Date
	switch name {
	case "", None:
		return Calendar{}, nil
	case Weekends:
		h = func(int) []shared.Date { return nil }
	case US:
		h = usHolidays
	case UK:
		h = ukHolidays
	case ZA:
		h = zaHolidays
	default:
		return Calendar{}, fmt.Errorf("unknown calendar %q: want %q, %q, %q, %q or %q", name, None, Weekends, US, UK, ZA)
	}
	return Calendar{rules: &rules{holidays: h, cache: map[int]map[shared.Date]bool{}}}, nil
}

// Enabled reports whether the calendar closes on any day.
func (c Calendar) Enabled() bool { return c.rules != nil }

// IsBusinessDay reports whether d is neither a weekend nor a public holiday.
func (c Calendar) IsBusinessDay(d shared.Date) bool {
	if c.rules == nil {
		return true
	}
	if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	return !c.rules.holiday(d)
}

// Following is d when it is a business day, and otherwise the next business
// day after it.
func (c Calendar) Following(d shared.Date) shared.Date {
	for !c.IsBusinessDay(d) {
		d = d.AddDays(1)
	}
	return d
}

// Preceding is d when it is a business day, and otherwise the last business
// day before it.
func (c Calendar) Preceding(d shared.Date) shared.Date {
	for !c.IsBusinessDay(d) {
		d = d.AddDays(-1)
	}
	return d
}

// holiday reports whether d is a public holiday. A year's holidays come from
// its own rules and the next year's, since an observed New Year's Day can
// fall on 31 December.
func (r *rules) holiday(d shared.Date) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	y := d.Year()
	set, ok := r.cache[y]
	if !ok {
		set = map[shared.Date]bool{}
		for _, ry := range []int{y, y + 1} {
			for _, h := range r.holidays(ry) {
				if h.Year() == y {
					set[h] = true
				}
			}
		}
		r.cache[y] = set
	}
	return set[d]
}

// nthWeekday is the nth (1-based) weekday wd of a month; n = -1 is the last.
func nthWeekday(year int, month time.Month, wd time.Weekday, n int) shared.Date {
	if n < 0 {
		last := shared.NewMonth(year, month).End()
		return last.AddDays(-((int(last.Weekday()) - int(wd) + 7) % 7))
	}
	first := shared.NewDate(year, month, 1)
	return first.AddDays((int(wd)-int(first.Weekday())+7)%7 + 7*(n-1))
}

// EasterSunday is Easter Sunday of a year in the Gregorian calendar, by the
// anonymous Gregorian algorithm.
func EasterSunday(year int) shared.Date {
	a := year % 19
	b, c := year/100, year%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return shared.NewDate(year, time.Month(month), day)
}

func isWeekend(d shared.Date) bool {
	wd := d.Weekday()
	return wd == time.Saturday || wd == time.Sunday
}

// usObserved moves a fixed-date federal holiday on a Saturday to the Friday
// before and on a Sunday to the Monday after.
func usObserved(d shared.Date) shared.Date {
	switch d.Weekday() {
	case time.Saturday:
		return d.AddDays(-1)
	case time.Sunday:
		return d.AddDays(1)
	}
	return d
}

func usHolidays(year int) []shared.Date {
	hs := []shared.Date{
		usObserved(shared.NewDate(year, time.January, 1)),
		nthWeekday(year, time.February, time.Monday, 3),
		nthWeekday(year, time.May, time.Monday, -1),
		usObserved(shared.NewDate(year, time.July, 4)),
		nthWeekday(year, time.September, time.Monday, 1),
		nthWeekday(year, time.October, time.Monday, 2),
		usObserved(shared.NewDate(year, time.November, 11)),
		nthWeekday(year, time.November, time.Thursday, 4),
		usObserved(shared.NewDate(year, time.December, 25)),
	}
	if year >= 1986 {
		hs = append(hs, nthWeekday(year, time.January, time.Monday, 3))
	}
	if year >= 2021 {
		hs = append(hs, usObserved(shared.NewDate(year, time.June, 19)))
	}
	return hs
}

// substitute gives each holiday that falls on a weekend the next weekday not
// already a holiday, taking the weekday holidays first, as the England and
// Wales substitute days work.
func substitute(days ...shared.Date) []shared.Date {
	taken := map[shared.Date]bool{}
	var out []shared.Date
	for _, d := range days {
		if !isWeekend(d) {
			taken[d] = true
			out = append(out, d)
		}
	}
	for _, d := range days {
		if isWeekend(d) {
			for isWeekend(d) || taken[d] {
				d = d.AddDays(1)
			}
			taken[d] = true
			out = append(out, d)
		}
	}
	return out
}

func ukHolidays(year int) []shared.Date {
	easter := EasterSunday(year)
	hs := []shared.Date{
		easter.AddDays(-2),
		easter.AddDays(1),
		nthWeekday(year, time.May, time.Monday, 1),
		nthWeekday(year, time.May, time.Monday, -1),
		nthWeekday(year, time.August, time.Monday, -1),
	}
	hs = append(hs, substitute(shared.NewDate(year, time.January, 1))...)
	return append(hs, substitute(shared.NewDate(year, time.December, 25), shared.NewDate(year, time.December, 26))...)
}

func zaHolidays(year int) []shared.Date {
	easter := EasterSunday(year)
	fixed := []shared.Date{
		shared.NewDate(year, time.January, 1),
		shared.NewDate(year, time.March, 21),
		shared.NewDate(year, time.April, 27),
		shared.NewDate(year, time.May, 1),
		shared.NewDate(year, time.June, 16),
		shared.NewDate(year, time.August, 9),
		shared.NewDate(year, time.September, 24),
		shared.NewDate(year, time.December, 16),
		shared.NewDate(year, time.December, 25),
		shared.NewDate(year, time.December, 26),
	}
	hs := []shared.Date{easter.AddDays(-2), easter.AddDays(1)}
	for _, d := range fixed {
		hs = append(hs, d)
		if d.Weekday() == time.Sunday {
			hs = append(hs, d.AddDays(1)) // a Sunday holiday makes the Monday a holiday
		}
	}
	return hs
}
