package claim_test

import (
	"reflect"
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/calendar"
	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func usCalendar(t *testing.T) calendar.Calendar {
	t.Helper()
	c, err := calendar.Lookup("us")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// businessDayRun simulates the same book and reopens with and without a
// calendar.
func businessDayRun(t *testing.T, cal calendar.Calendar, rollReports bool) (base, rolled []claim.Claim) {
	t.Helper()
	p := reopeningParams()
	p.NilProbability = 0.2
	book := fixedBook(3000, 20000, 0, 1.0)
	base = claim.NewClaimSimulator(p).WithPaymentDelay(7).Simulate(random.NewSource(91), book)
	base = claim.NewReopenSimulator(p).WithPaymentDelay(7).Apply(random.NewSource(91), base)
	rolled = claim.NewClaimSimulator(p).WithPaymentDelay(7).WithBusinessDays(cal, rollReports).Simulate(random.NewSource(91), book)
	rolled = claim.NewReopenSimulator(p).WithPaymentDelay(7).WithCalendar(cal).Apply(random.NewSource(91), rolled)
	return base, rolled
}

func TestBusinessDaysRollClosesAndReopens(t *testing.T) {
	cal := usCalendar(t)
	base, rolled := businessDayRun(t, cal, false)
	baseBy, rolledBy := byKey(t, base), byKey(t, rolled)
	reopens, movedReports := 0, 0
	for k, b := range baseBy {
		r := rolledBy[k]
		if r.ReportDate() != b.ReportDate() {
			t.Fatalf("report %v moved to %v without roll_reports", b.ReportDate(), r.ReportDate())
		}
		if !cal.IsBusinessDay(b.ReportDate()) {
			movedReports++ // a weekend report the option would move
		}
		for i, ep := range r.Episodes {
			if !cal.IsBusinessDay(ep.Close) {
				t.Fatalf("episode %d closes on %v, a %v", i+1, ep.Close, ep.Close.Weekday())
			}
			if i > 0 {
				reopens++
				if !cal.IsBusinessDay(ep.Open) || !ep.Open.After(r.Episodes[i-1].Close) {
					t.Fatalf("reopen on %v after a close on %v", ep.Open, r.Episodes[i-1].Close)
				}
			}
		}
		if r.Episodes[0].Close != cal.Following(b.Episodes[0].Close) {
			t.Fatalf("close %v rolled to %v, want %v", b.Episodes[0].Close, r.Episodes[0].Close, cal.Following(b.Episodes[0].Close))
		}
	}
	if reopens == 0 || movedReports == 0 {
		t.Fatalf("%d reopens and %d weekend reports: the test needs both", reopens, movedReports)
	}
}

func TestBusinessDaysRollReportsWhenAsked(t *testing.T) {
	cal := usCalendar(t)
	base, rolled := businessDayRun(t, cal, true)
	baseBy, rolledBy := byKey(t, base), byKey(t, rolled)
	for k, b := range baseBy {
		r := rolledBy[k]
		if want := cal.Following(b.ReportDate()); r.ReportDate() != want {
			t.Fatalf("report %v rolled to %v, want %v", b.ReportDate(), r.ReportDate(), want)
		}
	}
}

func TestBusinessDaysOffChangesNothing(t *testing.T) {
	base, rolled := businessDayRun(t, calendar.Calendar{}, true)
	if !reflect.DeepEqual(base, rolled) {
		t.Fatal("a switched-off calendar changed the claims")
	}
}

func TestBusinessDaysComposeWithTheSeasonalHoliday(t *testing.T) {
	cal := usCalendar(t)
	p := params()
	h := lob.SeasonalHolidayParams{Hemisphere: lob.Northern, ReportShare: 1, PaymentShare: 1}
	claims := claim.NewClaimSimulator(p).WithSeasonalHoliday(h).WithBusinessDays(cal, true).Simulate(random.NewSource(92), fixedBook(3000, 20000, 0, 1.0))
	for _, c := range claims {
		if !cal.IsBusinessDay(c.ReportDate()) || !cal.IsBusinessDay(c.CloseDate()) {
			t.Fatalf("claim %d: report %v or close %v not a business day", c.ID, c.ReportDate(), c.CloseDate())
		}
	}
}
