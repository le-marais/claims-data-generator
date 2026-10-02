package transaction_test

import (
	"math"
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func estimated(p lob.RunoffParams, claims []claim.Claim, seed uint64) []claim.Claim {
	out := append([]claim.Claim(nil), claims...)
	return transaction.NewCaseEstimator(p).Apply(random.NewSource(seed), out)
}

func TestCaseAdequacyMeanIsTheUltimateOverTheOpeningCase(t *testing.T) {
	claims := testClaims(5000)
	for _, mean := range []float64{0.8, 1.0, 1.25} {
		p := params()
		p.CaseAdequacyMean = mean
		ultimate, opening := 0.0, 0.0
		for _, c := range estimated(p, claims, 2) {
			ultimate += c.Episodes[0].Ultimate.Dollars()
			opening += c.InitialEstimate().Dollars()
		}
		if got := ultimate / opening; math.Abs(got-mean) > 0.05*mean {
			t.Errorf("adequacy mean %v: total ultimate / total opening case = %v", mean, got)
		}
	}
}

func TestCaseEstimatorLeavesTheTrueCostAlone(t *testing.T) {
	claims := testClaims(200)
	p := params()
	p.CaseAdequacyMean = 1.25
	for i, c := range estimated(p, claims, 3) {
		if c.Cost() != claims[i].Cost() {
			t.Fatalf("claim %d true cost changed by case estimation", c.ID)
		}
		if c.InitialEstimate() < shared.OneCent {
			t.Fatalf("claim %d opens at %v, want at least one cent", c.ID, c.InitialEstimate())
		}
	}
}

func TestExactCaseEstimatesWithNoNoiseAndNoBias(t *testing.T) {
	claims := testClaims(50)
	reopen := claims[0].CloseDate().AddDays(30)
	claims[0].Episodes = append(claims[0].Episodes, claim.Episode{Open: reopen, Close: reopen.AddDays(30), Ultimate: shared.FromDollars(700)})
	p := params()
	p.CaseAdequacySigma = 0
	for _, c := range estimated(p, claims, 4) {
		for j, e := range c.Episodes {
			if e.OpeningCase != e.Ultimate {
				t.Fatalf("claim %d episode %d opens at %v, want exactly its ultimate %v", c.ID, j+1, e.OpeningCase, e.Ultimate)
			}
		}
	}
}

// Case adequacy is a reserving knob: it must move the case, never a payment.
func TestCaseAdequacyNeverMovesPayments(t *testing.T) {
	claims := testClaims(300)
	payments := func(mean float64) []transaction.Transaction {
		p := params()
		p.CaseAdequacyMean = mean
		var out []transaction.Transaction
		for _, tx := range transaction.NewRunoffSimulator(p, sections()).Simulate(random.NewSource(6), estimated(p, claims, 6)) {
			if tx.Type == transaction.Payment {
				tx.ID = 0 // IDs shift with the number of case rows
				out = append(out, tx)
			}
		}
		return out
	}
	a, b := payments(1.0), payments(1.3)
	if len(a) != len(b) {
		t.Fatalf("payment count moved with case adequacy: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("payment %d moved with case adequacy: %+v vs %+v", i, a[i], b[i])
		}
	}
}
