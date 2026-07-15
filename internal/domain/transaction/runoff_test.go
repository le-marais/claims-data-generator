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
