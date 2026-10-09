package transaction_test

import (
	"reflect"
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/calendar"
	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
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

// onBusinessDays rolls test claims' episode dates to business days, as the
// claim and reopen stages do, keeping each paying episode at least delay
// days long and each reopen after the previous close.
func onBusinessDays(cal calendar.Calendar, claims []claim.Claim, delay int) []claim.Claim {
	out := make([]claim.Claim, len(claims))
	for i, c := range claims {
		c.Episodes = append([]claim.Episode(nil), c.Episodes...)
		for j := range c.Episodes {
			ep := &c.Episodes[j]
			ep.Open = cal.Following(ep.Open)
			if j > 0 && !ep.Open.After(c.Episodes[j-1].Close) {
				ep.Open = cal.Following(c.Episodes[j-1].Close.AddDays(1))
			}
			ep.Close = cal.Following(ep.Close)
			if ep.Close.Before(ep.Open.AddDays(delay)) {
				ep.Close = cal.Following(ep.Open.AddDays(delay))
			}
		}
		out[i] = c
	}
	return out
}

func TestRunoffRowsFallOnBusinessDays(t *testing.T) {
	cal := usCalendar(t)
	for _, delay := range []int{0, 7} {
		claims := onBusinessDays(cal, delayClaims(1000, 7), delay)
		p := params()
		p.PaymentDelayDays = float64(delay)
		p.PaymentsPerYear = 6
		p.MinPayment = 50
		p.CaseAdequacyMean = 1.5
		h := lob.SeasonalHolidayParams{Hemisphere: lob.Northern, PaymentShare: 0.5}
		txs := transaction.NewRunoffSimulator(p, sections()).WithSeasonalHoliday(h).WithCalendar(cal).Simulate(random.NewSource(93), claims)
		for _, tx := range txs {
			if !cal.IsBusinessDay(tx.Date) {
				t.Fatalf("delay %d: claim %d has a %s row on %v, a %v", delay, tx.ClaimID, tx.Type, tx.Date, tx.Date.Weekday())
			}
		}
		if n := delayBreaches(txs, delay); n != 0 {
			t.Errorf("delay %d: %d payments break the payment delay", delay, n)
		}
		grouped := byClaim(txs)
		for _, c := range claims {
			paid, outstanding := shared.Money(0), shared.Money(0)
			for i, rows := range episodeRows(c, grouped[c.ID]) {
				for _, tx := range rows {
					switch tx.Type {
					case transaction.Payment:
						paid += tx.Amount
					case transaction.Estimate:
						outstanding += tx.Amount
					}
					if outstanding < 0 {
						t.Fatalf("delay %d: claim %d negative case", delay, c.ID)
					}
					if tx.Date.Before(c.Episodes[i].Close) && tx.Type != transaction.Payment && outstanding <= 0 {
						t.Fatalf("delay %d: claim %d case at zero on %v before its close", delay, c.ID, tx.Date)
					}
				}
			}
			if paid != c.Cost() || outstanding != 0 {
				t.Fatalf("delay %d: claim %d paid %v with %v outstanding, want %v and 0", delay, c.ID, paid, outstanding, c.Cost())
			}
		}
	}
}

func TestRunoffWithoutACalendarIsUnchanged(t *testing.T) {
	claims := delayClaims(300, 7)
	p := params()
	p.PaymentDelayDays = 7
	want := transaction.NewRunoffSimulator(p, sections()).Simulate(random.NewSource(94), claims)
	got := transaction.NewRunoffSimulator(p, sections()).WithCalendar(calendar.Calendar{}).Simulate(random.NewSource(94), claims)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("an off calendar changed the runoff")
	}
}
