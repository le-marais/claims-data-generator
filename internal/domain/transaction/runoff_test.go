package transaction_test

import (
	"math"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func params() lob.RunoffParams {
	return lob.RunoffParams{
		CaseAdequacyMean:  1.0,
		CaseAdequacySigma: 0.35,
		PaymentsPerYear:   2.5,
		SettlementShare:   0.4,
		Concentration:     1.0,
		RevisionsPerYear:  4,
		RevisionSigma:     0.3,
	}
}

// testClaims builds n claims with varying sizes and durations.
func testClaims(n int) []claim.Claim {
	claims := make([]claim.Claim, n)
	for i := range claims {
		report := shared.NewDate(1998, time.March, 1).AddDays(i % 300)
		durations := []int{0, 10, 45, 180, 700}
		estimates := []float64{800, 3000, 12000, 40000, 250000}
		claims[i] = claim.Claim{
			ID:              i + 1,
			PolicyID:        i + 1,
			OccurrenceDate:  report.AddDays(-2),
			ReportDate:      report,
			CloseDate:       report.AddDays(durations[i%5]),
			InitialEstimate: shared.FromDollars(estimates[(i+2)%5]),
			RiskFactor:      1.0,
		}
	}
	return claims
}

// byClaim groups transactions per claim, preserving order.
func byClaim(txs []transaction.Transaction) map[int][]transaction.Transaction {
	m := map[int][]transaction.Transaction{}
	for _, tx := range txs {
		m[tx.ClaimID] = append(m[tx.ClaimID], tx)
	}
	return m
}

func TestRunoffInvariants(t *testing.T) {
	claims := testClaims(500)
	sim := transaction.NewRunoffSimulator(params())
	txs := sim.Simulate(random.NewSource(1), claims)
	grouped := byClaim(txs)
	if len(grouped) != len(claims) {
		t.Fatalf("transactions cover %d claims, want %d", len(grouped), len(claims))
	}
	for _, c := range claims {
		rows := grouped[c.ID]
		if len(rows) < 3 {
			t.Fatalf("claim %d has %d transactions, want at least initial estimate, payment, and closing movement", c.ID, len(rows))
		}
		first := rows[0]
		if first.Type != transaction.Estimate || first.Amount != c.InitialEstimate || first.Date != c.ReportDate {
			t.Fatalf("claim %d first row = %+v, want initial ESTIMATE %v on %s", c.ID, first, c.InitialEstimate, c.ReportDate)
		}
		outstanding := shared.Money(0)
		paid := shared.Money(0)
		prevDate := c.ReportDate
		for _, tx := range rows {
			if tx.Date.Before(prevDate) {
				t.Fatalf("claim %d transactions not chronological", c.ID)
			}
			prevDate = tx.Date
			if tx.Date.Before(c.ReportDate) || tx.Date.After(c.CloseDate) {
				t.Fatalf("claim %d transaction on %s outside report..close %s..%s", c.ID, tx.Date, c.ReportDate, c.CloseDate)
			}
			switch tx.Type {
			case transaction.Estimate:
				outstanding += tx.Amount
			case transaction.Payment:
				if tx.Amount <= 0 {
					t.Fatalf("claim %d non-positive payment %v", c.ID, tx.Amount)
				}
				paid += tx.Amount
			default:
				t.Fatalf("claim %d unknown transaction type %q", c.ID, tx.Type)
			}
			if outstanding < 0 {
				t.Fatalf("claim %d outstanding went negative", c.ID)
			}
		}
		if outstanding != 0 {
			t.Fatalf("claim %d outstanding at close = %v, want 0", c.ID, outstanding)
		}
		if paid <= 0 {
			t.Fatalf("claim %d total paid = %v, want positive", c.ID, paid)
		}
		last := rows[len(rows)-1]
		if last.Date != c.CloseDate {
			t.Fatalf("claim %d last transaction on %s, want close date %s", c.ID, last.Date, c.CloseDate)
		}
	}
}

func TestEveryPaymentHasMatchingEstimateReduction(t *testing.T) {
	claims := testClaims(200)
	sim := transaction.NewRunoffSimulator(params())
	txs := sim.Simulate(random.NewSource(2), claims)
	for i, tx := range txs {
		if tx.Type != transaction.Payment {
			continue
		}
		if i+1 >= len(txs) {
			t.Fatal("payment is the last transaction overall; missing estimate reduction")
		}
		next := txs[i+1]
		if next.Type != transaction.Estimate || next.ClaimID != tx.ClaimID || next.Date != tx.Date || next.Amount != -tx.Amount {
			t.Fatalf("payment %+v not followed by matching estimate reduction, got %+v", tx, next)
		}
	}
}

func TestTotalPaidCentersOnCaseAdequacy(t *testing.T) {
	claims := testClaims(4000)
	sim := transaction.NewRunoffSimulator(params())
	txs := sim.Simulate(random.NewSource(3), claims)
	paidByClaim := map[int]float64{}
	for _, tx := range txs {
		if tx.Type == transaction.Payment {
			paidByClaim[tx.ClaimID] += tx.Amount.Dollars()
		}
	}
	sumRatio := 0.0
	for _, c := range claims {
		sumRatio += paidByClaim[c.ID] / c.InitialEstimate.Dollars()
	}
	mean := sumRatio / float64(len(claims))
	if math.Abs(mean-1.0) > 0.05 {
		t.Errorf("mean paid/initial = %v, want ~1.0 (case adequacy)", mean)
	}
}

func TestSameDayCloseSettlesInFull(t *testing.T) {
	c := claim.Claim{
		ID: 1, PolicyID: 1,
		OccurrenceDate:  shared.NewDate(1998, time.May, 1),
		ReportDate:      shared.NewDate(1998, time.May, 3),
		CloseDate:       shared.NewDate(1998, time.May, 3),
		InitialEstimate: shared.FromDollars(1000),
		RiskFactor:      1.0,
	}
	sim := transaction.NewRunoffSimulator(params())
	txs := sim.Simulate(random.NewSource(4), []claim.Claim{c})
	outstanding := shared.Money(0)
	paid := shared.Money(0)
	for _, tx := range txs {
		if tx.Date != c.ReportDate {
			t.Fatalf("transaction on %s, want all on %s", tx.Date, c.ReportDate)
		}
		if tx.Type == transaction.Estimate {
			outstanding += tx.Amount
		} else {
			paid += tx.Amount
		}
	}
	if outstanding != 0 || paid <= 0 {
		t.Fatalf("same-day close: outstanding %v (want 0), paid %v (want > 0)", outstanding, paid)
	}
}

func TestTransactionIDsSequential(t *testing.T) {
	claims := testClaims(100)
	sim := transaction.NewRunoffSimulator(params())
	txs := sim.Simulate(random.NewSource(5), claims)
	for i, tx := range txs {
		if tx.ID != i+1 {
			t.Fatalf("transaction %d has ID %d, want %d", i, tx.ID, i+1)
		}
	}
}

func TestRunoffIsDeterministic(t *testing.T) {
	claims := testClaims(200)
	sim := transaction.NewRunoffSimulator(params())
	a := sim.Simulate(random.NewSource(42), claims)
	b := sim.Simulate(random.NewSource(42), claims)
	if len(a) != len(b) {
		t.Fatalf("lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("transaction %d differs between identical runs", i)
		}
	}
}

func TestLongClaimsReviseMoreThanShortClaims(t *testing.T) {
	claims := testClaims(1000)
	sim := transaction.NewRunoffSimulator(params())
	txs := sim.Simulate(random.NewSource(6), claims)
	grouped := byClaim(txs)
	var shortSum, shortN, longSum, longN float64
	for _, c := range claims {
		n := float64(len(grouped[c.ID]))
		if shared.DaysBetween(c.ReportDate, c.CloseDate) >= 180 {
			longSum += n
			longN++
		} else {
			shortSum += n
			shortN++
		}
	}
	if longSum/longN <= shortSum/shortN {
		t.Errorf("long claims average %v transactions, short %v; want more on long claims",
			longSum/longN, shortSum/shortN)
	}
}

func TestNilClaimHasNoPaymentsAndClosesToZero(t *testing.T) {
	c := claim.Claim{
		ID:              1,
		PolicyID:        1,
		OccurrenceDate:  shared.NewDate(2000, time.January, 1),
		ReportDate:      shared.NewDate(2000, time.January, 10),
		CloseDate:       shared.NewDate(2001, time.June, 1),
		InitialEstimate: shared.FromDollars(5000),
		RiskFactor:      1.0,
		Nil:             true,
	}
	sim := transaction.NewRunoffSimulator(params())
	txs := sim.Simulate(random.NewSource(1), []claim.Claim{c})

	if len(txs) == 0 {
		t.Fatal("expected transactions for the nil claim")
	}
	outstanding := shared.Money(0)
	paid := shared.Money(0)
	for _, tx := range txs {
		switch tx.Type {
		case transaction.Payment:
			paid += tx.Amount
		case transaction.Estimate:
			outstanding += tx.Amount
		}
	}
	if paid != 0 {
		t.Fatalf("nil claim paid %v, want 0", paid)
	}
	if outstanding != 0 {
		t.Fatalf("nil claim outstanding at close %v, want 0", outstanding)
	}
	first := txs[0]
	if first.Type != transaction.Estimate || first.Amount != c.InitialEstimate || first.Date != c.ReportDate {
		t.Fatalf("first row %+v is not the initial estimate on the report date", first)
	}
	if last := txs[len(txs)-1]; last.Date != c.CloseDate {
		t.Fatalf("last row on %s, want close date %s", last.Date, c.CloseDate)
	}
}

// reopenedClaim builds one claim with a reopen episode.
func reopenedClaim(isNil bool) claim.Claim {
	return claim.Claim{
		ID:              1,
		PolicyID:        1,
		OccurrenceDate:  shared.NewDate(2000, time.January, 1),
		ReportDate:      shared.NewDate(2000, time.January, 5),
		FirstCloseDate:  shared.NewDate(2000, time.June, 1),
		ReopenDate:      shared.NewDate(2000, time.September, 1),
		CloseDate:       shared.NewDate(2001, time.February, 1),
		InitialEstimate: shared.FromDollars(8000),
		ReopenEstimate:  shared.FromDollars(3000),
		RiskFactor:      1.0,
		Nil:             isNil,
	}
}

func TestReopenedClaimRunsTwoEpisodes(t *testing.T) {
	c := reopenedClaim(false)
	txs := transaction.NewRunoffSimulator(params()).Simulate(random.NewSource(11), []claim.Claim{c})

	outstanding := shared.Money(0)
	outstandingAtFirstClose := shared.Money(-1)
	var reopenRow *transaction.Transaction
	for i, tx := range txs {
		if tx.Type == transaction.Estimate {
			outstanding += tx.Amount
		}
		if !tx.Date.After(c.FirstCloseDate) {
			outstandingAtFirstClose = outstanding
		} else if reopenRow == nil {
			reopenRow = &txs[i]
		}
	}
	if outstandingAtFirstClose != 0 {
		t.Fatalf("outstanding at first close = %v, want 0", outstandingAtFirstClose)
	}
	if reopenRow == nil {
		t.Fatal("no transactions after the first close")
	}
	if reopenRow.Type != transaction.Estimate || reopenRow.Amount != c.ReopenEstimate || reopenRow.Date != c.ReopenDate {
		t.Fatalf("re-raise row %+v, want ESTIMATE %v on %s", *reopenRow, c.ReopenEstimate, c.ReopenDate)
	}
	if outstanding != 0 {
		t.Fatalf("outstanding at final close = %v, want 0", outstanding)
	}
	if last := txs[len(txs)-1]; last.Date != c.CloseDate {
		t.Fatalf("last transaction on %s, want final close %s", last.Date, c.CloseDate)
	}
}

func TestReopenedNilClaimPaysOnlyInEpisodeTwo(t *testing.T) {
	c := reopenedClaim(true)
	txs := transaction.NewRunoffSimulator(params()).Simulate(random.NewSource(12), []claim.Claim{c})

	paidBeforeReopen := shared.Money(0)
	paidAfterReopen := shared.Money(0)
	for _, tx := range txs {
		if tx.Type != transaction.Payment {
			continue
		}
		if tx.Date.Before(c.ReopenDate) {
			paidBeforeReopen += tx.Amount
		} else {
			paidAfterReopen += tx.Amount
		}
	}
	if paidBeforeReopen != 0 {
		t.Fatalf("reopened nil claim paid %v before the reopen, want 0", paidBeforeReopen)
	}
	if paidAfterReopen <= 0 {
		t.Fatalf("reopened nil claim paid %v in episode 2, want positive", paidAfterReopen)
	}
}

func TestReopenedClaimRowsChronological(t *testing.T) {
	claims := testClaims(50)
	for i := range claims {
		if i%4 == 0 {
			claims[i].FirstCloseDate = claims[i].CloseDate
			claims[i].ReopenDate = claims[i].CloseDate.AddDays(60)
			claims[i].ReopenEstimate = shared.FromDollars(2000)
			claims[i].CloseDate = claims[i].ReopenDate.AddDays(90)
		}
	}
	txs := transaction.NewRunoffSimulator(params()).Simulate(random.NewSource(13), claims)
	for id, rows := range byClaim(txs) {
		for i := 1; i < len(rows); i++ {
			if rows[i].Date.Before(rows[i-1].Date) {
				t.Fatalf("claim %d rows out of order", id)
			}
		}
	}
}

func TestTinyReopenEstimateStillClosesOnFinalCloseDate(t *testing.T) {
	c := reopenedClaim(false)
	c.ReopenEstimate = shared.Money(2) // two cents over a five-month episode
	for seed := uint64(1); seed <= 25; seed++ {
		txs := transaction.NewRunoffSimulator(params()).Simulate(random.NewSource(seed), []claim.Claim{c})
		outstanding := shared.Money(0)
		for _, tx := range txs {
			if tx.Type == transaction.Estimate {
				outstanding += tx.Amount
			}
		}
		if outstanding != 0 {
			t.Fatalf("seed %d: outstanding at final close = %v, want 0", seed, outstanding)
		}
		if last := txs[len(txs)-1]; last.Date != c.CloseDate {
			t.Fatalf("seed %d: last transaction on %s, want final close %s", seed, last.Date, c.CloseDate)
		}
	}
}

func TestNilClaimTinyEstimateStillClosesOnCloseDate(t *testing.T) {
	// A tiny initial estimate over a long duration is the case where revision
	// targets can round to zero; the terminal release must still land on the
	// close date.
	c := claim.Claim{
		ID:              1,
		PolicyID:        1,
		OccurrenceDate:  shared.NewDate(2000, time.January, 1),
		ReportDate:      shared.NewDate(2000, time.January, 2),
		CloseDate:       shared.NewDate(2003, time.January, 2),
		InitialEstimate: shared.FromDollars(0.02),
		RiskFactor:      1.0,
		Nil:             true,
	}
	sim := transaction.NewRunoffSimulator(params())
	// Try several seeds so at least one exercises revisions that round toward zero.
	for seed := uint64(1); seed <= 25; seed++ {
		txs := sim.Simulate(random.NewSource(seed), []claim.Claim{c})
		if len(txs) == 0 {
			t.Fatalf("seed %d: no transactions", seed)
		}
		outstanding := shared.Money(0)
		paid := shared.Money(0)
		for _, tx := range txs {
			switch tx.Type {
			case transaction.Payment:
				paid += tx.Amount
			case transaction.Estimate:
				outstanding += tx.Amount
			}
		}
		if paid != 0 {
			t.Fatalf("seed %d: nil claim paid %v, want 0", seed, paid)
		}
		if outstanding != 0 {
			t.Fatalf("seed %d: outstanding at close %v, want 0", seed, outstanding)
		}
		if last := txs[len(txs)-1]; last.Date != c.CloseDate {
			t.Fatalf("seed %d: last transaction on %s, want close date %s", seed, last.Date, c.CloseDate)
		}
	}
}
