package calendar_test

import (
	"slices"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/calendar"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

func mustLookup(t *testing.T, name string) calendar.Calendar {
	t.Helper()
	c, err := calendar.Lookup(name)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// weekdayHolidays lists the weekdays of a year the calendar closes on, as
// ISO dates.
func weekdayHolidays(c calendar.Calendar, year int) []string {
	var out []string
	for d := shared.NewDate(year, time.January, 1); d.Year() == year; d = d.AddDays(1) {
		if wd := d.Weekday(); wd != time.Saturday && wd != time.Sunday && !c.IsBusinessDay(d) {
			out = append(out, d.String())
		}
	}
	return out
}

func TestHolidayTables(t *testing.T) {
	tests := []struct {
		calendar string
		year     int
		want     []string
	}{
		{"us", 1998, []string{"1998-01-01", "1998-01-19", "1998-02-16", "1998-05-25", "1998-07-03", "1998-09-07", "1998-10-12", "1998-11-11", "1998-11-26", "1998-12-25"}},
		// Juneteenth on a Saturday is observed on Friday 18 June, Independence
		// Day on a Sunday on Monday 5 July, Christmas on a Saturday on Friday
		// 24 December, and New Year's Day 2022, a Saturday, on 31 December.
		{"us", 2021, []string{"2021-01-01", "2021-01-18", "2021-02-15", "2021-05-31", "2021-06-18", "2021-07-05", "2021-09-06", "2021-10-11", "2021-11-11", "2021-11-25", "2021-12-24", "2021-12-31"}},
		{"us", 1985, []string{"1985-01-01", "1985-02-18", "1985-05-27", "1985-07-04", "1985-09-02", "1985-10-14", "1985-11-11", "1985-11-28", "1985-12-25"}},
		// Boxing Day on a Saturday moves to Monday 28 December.
		{"uk", 2020, []string{"2020-01-01", "2020-04-10", "2020-04-13", "2020-05-04", "2020-05-25", "2020-08-31", "2020-12-25", "2020-12-28"}},
		// Christmas on a Saturday: Monday 27 and Tuesday 28 December.
		{"uk", 2021, []string{"2021-01-01", "2021-04-02", "2021-04-05", "2021-05-03", "2021-05-31", "2021-08-30", "2021-12-27", "2021-12-28"}},
		// New Year's Day on a Saturday: Monday 3 January. Christmas on a
		// Sunday: Boxing Day Monday 26 and a substitute Tuesday 27. The
		// moved spring and jubilee holidays of 2022 are one-offs, not
		// modelled.
		{"uk", 2022, []string{"2022-01-03", "2022-04-15", "2022-04-18", "2022-05-02", "2022-05-30", "2022-08-29", "2022-12-26", "2022-12-27"}},
		// Human Rights Day on a Sunday moves to Monday 22 March, the Day of
		// Goodwill on a Sunday to Monday 27 December; Workers' Day and
		// Christmas on a Saturday have no substitute.
		{"za", 2021, []string{"2021-01-01", "2021-03-22", "2021-04-02", "2021-04-05", "2021-04-27", "2021-06-16", "2021-08-09", "2021-09-24", "2021-12-16", "2021-12-27"}},
		// Christmas on a Sunday: Monday 26 December is already the Day of
		// Goodwill, so there is no extra day.
		{"za", 2016, []string{"2016-01-01", "2016-03-21", "2016-03-25", "2016-03-28", "2016-04-27", "2016-05-02", "2016-06-16", "2016-08-09", "2016-12-16", "2016-12-26"}},
		{"weekends", 2021, nil},
	}
	for _, tt := range tests {
		got := weekdayHolidays(mustLookup(t, tt.calendar), tt.year)
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s %d holidays:\n got %v\nwant %v", tt.calendar, tt.year, got, tt.want)
		}
	}
}

func TestWeekendsAreNeverBusinessDays(t *testing.T) {
	for _, name := range []string{"weekends", "us", "uk", "za"} {
		c := mustLookup(t, name)
		for _, d := range []shared.Date{shared.NewDate(2021, time.June, 19), shared.NewDate(2021, time.June, 20)} {
			if c.IsBusinessDay(d) {
				t.Errorf("%s: %v (%v) is a business day", name, d, d.Weekday())
			}
		}
	}
}

func TestEaster(t *testing.T) {
	// Good Friday is two days before Easter Sunday in both Easter calendars.
	easter := map[int]string{1998: "1998-04-12", 2000: "2000-04-23", 2019: "2019-04-21", 2021: "2021-04-04", 2024: "2024-03-31", 2038: "2038-04-25"}
	for year, sunday := range easter {
		got := calendar.EasterSunday(year).String()
		if got != sunday {
			t.Errorf("Easter %d = %s, want %s", year, got, sunday)
		}
	}
}

func TestFollowingAndPreceding(t *testing.T) {
	us := mustLookup(t, "us")
	// Thanksgiving 2021 is Thursday 25 November; the Friday after is a
	// business day.
	thanksgiving := shared.NewDate(2021, time.November, 25)
	if got := us.Following(thanksgiving); got != shared.NewDate(2021, time.November, 26) {
		t.Errorf("Following(Thanksgiving) = %v", got)
	}
	if got := us.Preceding(thanksgiving); got != shared.NewDate(2021, time.November, 24) {
		t.Errorf("Preceding(Thanksgiving) = %v", got)
	}
	// Christmas Eve 2021 (observed Christmas) through Sunday 26 December
	// roll forward to Monday 27, and back to Thursday 23.
	for d := shared.NewDate(2021, time.December, 24); d.Before(shared.NewDate(2021, time.December, 27)); d = d.AddDays(1) {
		if got := us.Following(d); got != shared.NewDate(2021, time.December, 27) {
			t.Errorf("Following(%v) = %v, want 2021-12-27", d, got)
		}
		if got := us.Preceding(d); got != shared.NewDate(2021, time.December, 23) {
			t.Errorf("Preceding(%v) = %v, want 2021-12-23", d, got)
		}
	}
	monday := shared.NewDate(2021, time.December, 27)
	if us.Following(monday) != monday || us.Preceding(monday) != monday {
		t.Error("a business day must roll to itself")
	}
}

func TestOffIsTheIdentity(t *testing.T) {
	for _, name := range []string{"", "none"} {
		c := mustLookup(t, name)
		if c.Enabled() {
			t.Errorf("%q is enabled", name)
		}
		sat := shared.NewDate(2021, time.June, 19)
		if !c.IsBusinessDay(sat) || c.Following(sat) != sat || c.Preceding(sat) != sat {
			t.Errorf("%q moved a Saturday", name)
		}
	}
	if !mustLookup(t, "us").Enabled() {
		t.Error("us is not enabled")
	}
}

func TestLookupRejectsAnUnknownCalendar(t *testing.T) {
	if _, err := calendar.Lookup("fr"); err == nil {
		t.Fatal("Lookup(fr): want error")
	}
}
