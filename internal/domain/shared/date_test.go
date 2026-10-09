package shared

import (
	"math"
	"testing"
)

func TestDateString(t *testing.T) {
	d := NewDate(2020, 3, 1)
	if got := d.String(); got != "2020-03-01" {
		t.Errorf("String() = %q, want 2020-03-01", got)
	}
}

func TestDateAddDays(t *testing.T) {
	d := NewDate(2020, 2, 27).AddDays(3)
	if got := d.String(); got != "2020-03-01" {
		t.Errorf("AddDays(3) = %q, want 2020-03-01 (leap year)", got)
	}
}

func TestDaysBetween(t *testing.T) {
	a := NewDate(2020, 1, 1)
	b := NewDate(2020, 12, 31)
	if got := DaysBetween(a, b); got != 365 {
		t.Errorf("DaysBetween = %d, want 365 (leap year)", got)
	}
	if got := DaysBetween(b, a); got != -365 {
		t.Errorf("DaysBetween reversed = %d, want -365", got)
	}
}

func TestDateBefore(t *testing.T) {
	a := NewDate(2020, 1, 1)
	b := NewDate(2020, 1, 2)
	if !a.Before(b) || b.Before(a) || a.Before(a) {
		t.Error("Before ordering wrong")
	}
}

func TestDateYear(t *testing.T) {
	if got := NewDate(2003, 7, 15).Year(); got != 2003 {
		t.Errorf("Year() = %d, want 2003", got)
	}
}

func TestTrendYears(t *testing.T) {
	cases := []struct {
		d    Date
		want float64
	}{
		{NewDate(1998, 7, 2), 0},                     // day 183 of 365: the middle of the start year
		{NewDate(1999, 7, 2), 1},                     // a year later
		{NewDate(1998, 1, 1), 0.5/365 - 0.5},         // midday of 1 January
		{NewDate(1998, 12, 31), 364.5/365 - 0.5},     // midday of 31 December
		{NewDate(2000, 12, 31), 2 + 365.5/366 - 0.5}, // leap year: 366 days
		{NewDate(1997, 7, 2), -1},                    // before the start year
	}
	for _, c := range cases {
		if got := TrendYears(c.d, 1998); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("TrendYears(%s) = %v, want %v", c.d, got, c.want)
		}
	}
}

func TestDateAddMonths(t *testing.T) {
	tests := []struct {
		from Date
		n    int
		want string
	}{
		{NewDate(2001, 7, 15), 1, "2001-08-15"},
		{NewDate(2001, 1, 31), 1, "2001-02-28"},
		{NewDate(2000, 1, 31), 1, "2000-02-29"},
		{NewDate(2001, 8, 31), 1, "2001-09-30"},
		{NewDate(2001, 12, 20), 1, "2002-01-20"},
		{NewDate(2001, 3, 31), -1, "2001-02-28"},
	}
	for _, tt := range tests {
		if got := tt.from.AddMonths(tt.n).String(); got != tt.want {
			t.Errorf("%v.AddMonths(%d) = %s, want %s", tt.from, tt.n, got, tt.want)
		}
	}
}
