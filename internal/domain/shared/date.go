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

func (d Date) Before(other Date) bool {
	return d.t.Before(other.t)
}

func (d Date) After(other Date) bool {
	return d.t.After(other.t)
}

func (d Date) Year() int {
	return d.t.Year()
}

// DaysBetween returns the number of days from a to b (negative if b is earlier).
func DaysBetween(a, b Date) int {
	return int(b.t.Sub(a.t) / (24 * time.Hour))
}

// String formats the date as ISO 8601, e.g. "2020-03-01".
func (d Date) String() string {
	return d.t.Format("2006-01-02")
}
