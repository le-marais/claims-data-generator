package transaction_test

import (
	"reflect"
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// interimInWindow counts the payments dated in the holiday window that are
// not a final settlement on an episode's close date.
func interimInWindow(h lob.SeasonalHolidayParams, claims []claim.Claim, txs []transaction.Transaction) int {
	closes := map[int]map[shared.Date]bool{}
	for _, c := range claims {
		closes[c.ID] = map[shared.Date]bool{}
		for _, ep := range c.Episodes {
			closes[c.ID][ep.Close] = true
		}
	}
	n := 0
	for _, tx := range txs {
		if tx.Type == transaction.Payment && h.InWindow(tx.Date) && !closes[tx.ClaimID][tx.Date] {
			n++
		}
	}
	return n
}

func TestSeasonalHolidayDefersInterimPayments(t *testing.T) {
	claims := delayClaims(1000, 7)
	p := params()
	p.PaymentDelayDays = 7
	p.PaymentsPerYear = 12
	h := lob.SeasonalHolidayParams{Hemisphere: lob.Northern, PaymentShare: 1}
	base := transaction.NewRunoffSimulator(p, sections()).Simulate(random.NewSource(71), claims)
	txs := transaction.NewRunoffSimulator(p, sections()).WithSeasonalHoliday(h).Simulate(random.NewSource(71), claims)

	if interimInWindow(h, claims, base) == 0 {
		t.Fatal("no interim payment fell in the window; the test proves nothing")
	}
	if n := interimInWindow(h, claims, txs); n != 0 {
		t.Errorf("%d interim payments left in the window at payment share 1", n)
	}
	if n := delayBreaches(txs, 7); n != 0 {
		t.Errorf("%d payments break the payment delay", n)
	}
	grouped := byClaim(txs)
	for _, c := range claims {
		paid, outstanding := shared.Money(0), shared.Money(0)
		for _, tx := range grouped[c.ID] {
			if tx.Date.After(c.CloseDate()) {
				t.Fatalf("claim %d: %s row on %v after close %v", c.ID, tx.Type, tx.Date, c.CloseDate())
			}
			switch tx.Type {
			case transaction.Payment:
				paid += tx.Amount
			case transaction.Estimate:
				outstanding += tx.Amount
				if outstanding < 0 {
					t.Fatalf("claim %d: negative case %v", c.ID, outstanding)
				}
			}
		}
		if paid != c.Cost() || outstanding != 0 {
			t.Fatalf("claim %d: paid %v, case %v; want %v and 0", c.ID, paid, outstanding, c.Cost())
		}
	}
}

func TestSeasonalHolidayHoldsLatePaymentsForTheSettlement(t *testing.T) {
	// Open 15 July, close 30 August: an interim payment deferred from late
	// July or August lands after the last interim day and is paid with the
	// final settlement.
	open := shared.NewDate(1999, 7, 15)
	var claims []claim.Claim
	for i := range 200 {
		claims = append(claims, claim.Claim{
			ID: i + 1, PolicyID: i + 1, OccurrenceDate: open,
			Episodes: []claim.Episode{{Open: open, Close: shared.NewDate(1999, 8, 30), Ultimate: shared.FromDollars(10000), OpeningCase: shared.FromDollars(10000)}},
		})
	}
	p := params()
	p.PaymentsPerYear = 40
	h := lob.SeasonalHolidayParams{Hemisphere: lob.Northern, PaymentShare: 1}
	count := func(txs []transaction.Transaction) int {
		n := 0
		for _, tx := range txs {
			if tx.Type == transaction.Payment {
				n++
			}
		}
		return n
	}
	base := transaction.NewRunoffSimulator(p, sections()).Simulate(random.NewSource(72), claims)
	txs := transaction.NewRunoffSimulator(p, sections()).WithSeasonalHoliday(h).Simulate(random.NewSource(72), claims)
	if count(txs) >= count(base) {
		t.Errorf("%d payments with the holiday, want fewer than %d: late deferrals should fold into the settlement", count(txs), count(base))
	}
	if n := interimInWindow(h, claims, txs); n != 0 {
		t.Errorf("%d interim payments left in the window", n)
	}
	for id, rows := range paymentsByClaim(txs) {
		total := shared.Money(0)
		for _, a := range rows {
			total += a
		}
		if total != shared.FromDollars(10000) {
			t.Fatalf("claim %d paid %v, want 10000", id, total)
		}
	}
}

func TestSeasonalHolidayOffLeavesTheRunoffUnchanged(t *testing.T) {
	claims := delayClaims(300, 7)
	p := params()
	p.PaymentDelayDays = 7
	off := lob.SeasonalHolidayParams{Hemisphere: lob.NoHoliday, PaymentShare: 1}
	want := transaction.NewRunoffSimulator(p, sections()).Simulate(random.NewSource(73), claims)
	got := transaction.NewRunoffSimulator(p, sections()).WithSeasonalHoliday(off).Simulate(random.NewSource(73), claims)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("a switched-off seasonal holiday changed the runoff")
	}
}
