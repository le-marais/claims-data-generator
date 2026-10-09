package shared

import "time"

// Date is a calendar date (no time of day), held at UTC midnight.
type Date struct {
	t time.Time
}

func NewDate(year int, month time.Month, day int) Date {
	return Date{time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

func (d Date) AddDays(n int) Date {
	return Date{d.t.AddDate(0, 0, n)}
}

// AddMonths moves the date to the same day n months on, clamped to the last
// day of the target month: 31 January plus one month is 28 or 29 February.
func (d Date) AddMonths(n int) Date {
	target := d.Month().Add(n)
	return NewDate(target.Year(), target.Month(), min(d.Day(), target.End().Day()))
}

func (d Date) Before(other Date) bool {
	return d.t.Before(other.t)
}

func (d Date) After(other Date) bool {
	return d.t.After(other.t)
}

func (d Date) Year() int {
	return d.t.Year()
}

// Day is the day of the month, 1 to 31.
func (d Date) Day() int { return d.t.Day() }

// Weekday is the day of the week.
func (d Date) Weekday() time.Weekday { return d.t.Weekday() }

// IsZero reports whether the date is the zero value.
func (d Date) IsZero() bool { return d.t.IsZero() }

// Equal reports whether two dates fall on the same instant.
func (d Date) Equal(other Date) bool { return d.t.Equal(other.t) }

// TrendYears places a date on the continuous time axis that claims inflation
// trends along: years elapsed since the middle of the given start year,
// measuring each day at its midpoint. 2 July of a non-leap start year is 0, the
// same date a year later is 1, and 1 January of the start year is just under
// -0.5. The inflation index anchors each simulated annual factor at the middle
// of its year on this axis, and pricing trends premium along the same axis, so
// the two read time identically.
func TrendYears(d Date, startYear int) float64 {
	yearStart := NewDate(d.Year(), time.January, 1)
	daysInYear := DaysBetween(yearStart, NewDate(d.Year()+1, time.January, 1))
	dayOfYear := DaysBetween(yearStart, d)
	return float64(d.Year()-startYear) + (float64(dayOfYear)+0.5)/float64(daysInYear) - 0.5
}

// DaysBetween returns the number of days from a to b (negative if b is earlier).
func DaysBetween(a, b Date) int {
	return int(b.t.Sub(a.t) / (24 * time.Hour))
}

// String formats the date as ISO 8601, e.g. "2020-03-01".
func (d Date) String() string {
	return d.t.Format("2006-01-02")
}
