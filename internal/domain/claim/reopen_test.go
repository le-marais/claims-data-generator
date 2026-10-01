package claim_test

import (
	"reflect"
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
		if !reflect.DeepEqual(after[i], before[i]) {
			t.Fatalf("claim %d changed with reopening probability 0", after[i].ID)
		}
		if after[i].Reopened() {
			t.Fatalf("claim %d reopened with probability 0", after[i].ID)
		}
	}
}

func TestReopenDates(t *testing.T) {
	claims := reopenFixture(t, reopeningParams(), 42)
	reopened := 0
	for _, c := range claims {
		if len(c.Episodes) > 2 {
			t.Fatalf("claim %d has %d episodes, want at most 2", c.ID, len(c.Episodes))
		}
		if !c.Reopened() {
			continue
		}
		reopened++
		first, second := c.Episodes[0], c.Episodes[1]
		if !second.Open.After(first.Close) {
			t.Fatalf("claim %d reopen %s not strictly after first close %s", c.ID, second.Open, first.Close)
		}
		if !second.Close.After(second.Open) {
			t.Fatalf("claim %d final close %s not strictly after reopen %s", c.ID, second.Close, second.Open)
		}
		if first.Close.Before(first.Open) {
			t.Fatalf("claim %d first close %s before report %s", c.ID, first.Close, first.Open)
		}
		if second.Nil {
			t.Fatalf("claim %d reopen episode is nil, want it to pay", c.ID)
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
		if !reflect.DeepEqual(a[i], b[i]) {
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
		if !reflect.DeepEqual(applied[i], base[i]) {
			t.Fatalf("non-reopened claim %d changed by the reopen pass", applied[i].ID)
		}
	}
}
