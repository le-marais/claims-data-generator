package shared

import (
	"fmt"
	"time"
)

// Month is a calendar month, held as an absolute month index so offsets and
// differences are integer arithmetic and two months compare with ==.
type Month struct {
	index int // year*12 + int(month) - 1
}

// NewMonth builds a calendar month. The month number is normalised, so
// NewMonth(1998, time.Month(13)) is January 1999.
func NewMonth(year int, m time.Month) Month {
	return Month{index: year*12 + int(m) - 1}
}

// Month returns the calendar month the date falls in.
func (d Date) Month() Month {
	return NewMonth(d.t.Year(), d.t.Month())
}

// Year returns the calendar year. Division is floored, not truncated, so a
// negative index (a month before year 0) round-trips through Year and Month
// rather than landing on a nonexistent month-of-year.
func (m Month) Year() int { return floorDiv(m.index, 12) }

// Month returns the month of the year.
func (m Month) Month() time.Month { return time.Month(floorMod(m.index, 12) + 1) }

// Quarter returns the calendar quarter, 1 to 4.
func (m Month) Quarter() int { return floorMod(m.index, 12)/3 + 1 }

// floorDiv is integer division rounded toward negative infinity, unlike Go's
// truncating /.
func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// floorMod is a%b with the sign of b, unlike Go's truncating %.
func floorMod(a, b int) int {
	r := a % b
	if r != 0 && (r < 0) != (b < 0) {
		r += b
	}
	return r
}

// Add returns the month n months later; n may be negative.
func (m Month) Add(n int) Month { return Month{index: m.index + n} }

// Start returns the first day of the month.
func (m Month) Start() Date { return NewDate(m.Year(), m.Month(), 1) }

// End returns the last day of the month.
func (m Month) End() Date { return m.Add(1).Start().AddDays(-1) }

// String formats the month as "1998-01".
func (m Month) String() string { return fmt.Sprintf("%04d-%02d", m.Year(), int(m.Month())) }

// MonthsBetween returns the number of whole months from a to b, negative when
// b is earlier than a.
func MonthsBetween(a, b Month) int { return b.index - a.index }
