package claim_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func reopeningParams() lob.ClaimParams {
	p := params()
	p.Reopening = lob.ReopeningParams{Probability: 0.5, EstimateFactor: 0.45, EstimateSigma: 0.5, LagMedianDays: 90, LagSigma: 0.7}
	return p
}

// reopenFixture simulates a book of claims and applies the reopen pass.
func reopenFixture(t *testing.T, p lob.ClaimParams, seed uint64) []claim.Claim {
	t.Helper()
	claims := claim.NewClaimSimulator(p).Simulate(random.NewSource(seed), fixedBook(3000, 20000, 0, 1.0))
	if len(claims) == 0 {
		t.Fatal("expected claims")
	}
	return claim.NewReopenSimulator(p).Apply(random.NewSource(seed), claims)
}

func TestReopenZeroProbabilityIsANoOp(t *testing.T) {
	p := reopeningParams()
	p.Reopening.Probability = 0
	before := claim.NewClaimSimulator(p).Simulate(random.NewSource(41), fixedBook(1000, 20000, 0, 1.0))
	after := claim.NewReopenSimulator(p).Apply(random.NewSource(41), append([]claim.Claim(nil), before...))
	for i := range after {
		if after[i] != before[i] {
			t.Fatalf("claim %d changed with reopening probability 0", after[i].ID)
		}
		if after[i].Reopened() {
			t.Fatalf("claim %d reopened with probability 0", after[i].ID)
		}
	}
}

func TestReopenDatesAndEstimates(t *testing.T) {
	claims := reopenFixture(t, reopeningParams(), 42)
	reopened := 0
	for _, c := range claims {
		if !c.Reopened() {
			if c.FirstCloseDate != (claim.Claim{}).FirstCloseDate || c.ReopenEstimate != 0 {
				t.Fatalf("claim %d not reopened but carries reopen fields: %+v", c.ID, c)
			}
			continue
		}
		reopened++
		if !c.ReopenDate.After(c.FirstCloseDate) {
			t.Fatalf("claim %d reopen %s not strictly after first close %s", c.ID, c.ReopenDate, c.FirstCloseDate)
		}
		if !c.CloseDate.After(c.ReopenDate) {
			t.Fatalf("claim %d final close %s not strictly after reopen %s", c.ID, c.CloseDate, c.ReopenDate)
		}
		if c.FirstCloseDate.Before(c.ReportDate) {
			t.Fatalf("claim %d first close %s before report %s", c.ID, c.FirstCloseDate, c.ReportDate)
		}
		if c.ReopenEstimate <= 0 {
			t.Fatalf("claim %d reopen estimate %v not positive", c.ID, c.ReopenEstimate)
		}
	}
	if reopened == 0 {
		t.Fatal("fixture produced no reopened claims at probability 0.5")
	}
}

func TestReopenIsDeterministicPerSeed(t *testing.T) {
	a := reopenFixture(t, reopeningParams(), 7)
	b := reopenFixture(t, reopeningParams(), 7)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("claim %d differs between identical runs", a[i].ID)
		}
	}
}

func TestReopenLeavesNonReopenedClaimsUntouched(t *testing.T) {
	p := reopeningParams()
	base := claim.NewClaimSimulator(p).Simulate(random.NewSource(9), fixedBook(2000, 20000, 0, 1.0))
	applied := claim.NewReopenSimulator(p).Apply(random.NewSource(9), append([]claim.Claim(nil), base...))
	for i := range applied {
		if applied[i].Reopened() {
			continue
		}
		if applied[i] != base[i] {
			t.Fatalf("non-reopened claim %d changed by the reopen pass", applied[i].ID)
		}
	}
}
