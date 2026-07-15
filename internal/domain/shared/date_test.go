package shared

import "testing"

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
