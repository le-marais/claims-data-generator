package shared_test

import (
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/shared"
)

func TestMonthStringIsZeroPadded(t *testing.T) {
	cases := []struct {
		m    shared.Month
		want string
	}{
		{shared.NewMonth(1998, time.January), "1998-01"},
		{shared.NewMonth(1998, time.October), "1998-10"},
		{shared.NewMonth(2007, time.December), "2007-12"},
	}
	for _, c := range cases {
		if got := c.m.String(); got != c.want {
			t.Errorf("String() = %q, want %q", got, c.want)
		}
	}
}

func TestDateMonth(t *testing.T) {
	got := shared.NewDate(1998, time.March, 15).Month()
	if want := shared.NewMonth(1998, time.March); got != want {
		t.Errorf("Month() = %v, want %v", got, want)
	}
}

func TestMonthAddCrossesYearBoundaries(t *testing.T) {
	cases := []struct {
		from shared.Month
		n    int
		want shared.Month
	}{
		{shared.NewMonth(1998, time.December), 1, shared.NewMonth(1999, time.January)},
		{shared.NewMonth(1999, time.January), -1, shared.NewMonth(1998, time.December)},
		{shared.NewMonth(1998, time.January), 24, shared.NewMonth(2000, time.January)},
		{shared.NewMonth(1998, time.March), 0, shared.NewMonth(1998, time.March)},
	}
	for _, c := range cases {
		if got := c.from.Add(c.n); got != c.want {
			t.Errorf("%v.Add(%d) = %v, want %v", c.from, c.n, got, c.want)
		}
	}
}

func TestMonthsBetween(t *testing.T) {
	cases := []struct {
		a, b shared.Month
		want int
	}{
		{shared.NewMonth(1998, time.January), shared.NewMonth(1999, time.January), 12},
		{shared.NewMonth(1998, time.March), shared.NewMonth(1999, time.January), 10},
		{shared.NewMonth(1999, time.January), shared.NewMonth(1998, time.January), -12},
		{shared.NewMonth(1998, time.June), shared.NewMonth(1998, time.June), 0},
	}
	for _, c := range cases {
		if got := shared.MonthsBetween(c.a, c.b); got != c.want {
			t.Errorf("MonthsBetween(%v, %v) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestMonthYearMonthAndQuarter(t *testing.T) {
	cases := []struct {
		m       shared.Month
		year    int
		month   time.Month
		quarter int
	}{
		{shared.NewMonth(1998, time.January), 1998, time.January, 1},
		{shared.NewMonth(1998, time.March), 1998, time.March, 1},
		{shared.NewMonth(1998, time.April), 1998, time.April, 2},
		{shared.NewMonth(1998, time.September), 1998, time.September, 3},
		{shared.NewMonth(2007, time.December), 2007, time.December, 4},
		// A negative year index must round-trip: Year, Month and Quarter use
		// floor division, not Go's truncating division, so an index before
		// year 0 does not land on a nonexistent month-of-year.
		{shared.NewMonth(-1, time.January), -1, time.January, 1},
		{shared.NewMonth(-1, time.October), -1, time.October, 4},
	}
	for _, c := range cases {
		if got := c.m.Year(); got != c.year {
			t.Errorf("%v.Year() = %d, want %d", c.m, got, c.year)
		}
		if got := c.m.Month(); got != c.month {
			t.Errorf("%v.Month() = %v, want %v", c.m, got, c.month)
		}
		if got := c.m.Quarter(); got != c.quarter {
			t.Errorf("%v.Quarter() = %d, want %d", c.m, got, c.quarter)
		}
	}
}

func TestMonthStartAndEnd(t *testing.T) {
	// February 2000 is a leap February, so End must be the 29th.
	m := shared.NewMonth(2000, time.February)
	if got, want := m.Start().String(), "2000-02-01"; got != want {
		t.Errorf("Start() = %s, want %s", got, want)
	}
	if got, want := m.End().String(), "2000-02-29"; got != want {
		t.Errorf("End() = %s, want %s", got, want)
	}
	dec := shared.NewMonth(1998, time.December)
	if got, want := dec.End().String(), "1998-12-31"; got != want {
		t.Errorf("Start() = %s, want %s", got, want)
	}
}

func TestNewMonthNormalisesOutOfRangeMonths(t *testing.T) {
	if got, want := shared.NewMonth(1998, time.Month(13)), shared.NewMonth(1999, time.January); got != want {
		t.Errorf("NewMonth(1998, 13) = %v, want %v", got, want)
	}
}
