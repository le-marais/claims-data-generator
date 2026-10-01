package claim

import (
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// bookForCap builds a small book of identical policies for severity tests.
func bookForCap(n int) []policy.Policy {
	var b []policy.Policy
	start := shared.NewDate(2000, time.January, 1)
	for i := 1; i <= n; i++ {
		b = append(b, policy.Policy{
			ID:         i,
			CoverStart: start,
			CoverEnd:   start.AddDays(364),
			SumInsured: shared.FromDollars(20000),
			Excess:     shared.FromDollars(300),
			RiskFactor: 1.0,
		})
	}
	return b
}

// ownDamageOnly is a class with a single sum-insured section.
func ownDamageOnly(frequency, medianFraction, sigma float64, cl lob.CloseLagParams) lob.ClaimParams {
	return lob.ClaimParams{Sections: []lob.SectionParams{{
		Name:          "own_damage",
		BaseFrequency: frequency,
		Severity:      lob.SeverityParams{Kind: lob.SumInsuredLognormal, MedianFraction: medianFraction, Sigma: sigma},
		ReportLag:     lob.ReportLagParams{Median: 2, Sigma: 1.2},
		CloseLag:      cl,
	}}}
}

func TestSumInsuredSeverityIsCappedAtSumInsured(t *testing.T) {
	// Many claims per policy so the tail is exercised, and a heavy severity so
	// the cap bites often.
	params := ownDamageOnly(2.0, 0.5, 1.5, lob.CloseLagParams{Shape: 1.2, MeanDays: 120, RiskLoading: 0.3})
	claims := NewClaimSimulator(params).Simulate(random.NewSource(1), bookForCap(500))
	if len(claims) == 0 {
		t.Fatal("no claims generated")
	}
	for _, c := range claims {
		groundUp := c.Episodes[0].Ultimate.Dollars() + 300 // + excess
		if groundUp > 20000+1e-6 {
			t.Fatalf("claim %d own-damage ground-up %.2f exceeds sum insured 20000", c.ID, groundUp)
		}
	}
}

// RF-14: a sum-insured severity is sized off the policy's BaseSumInsured, so the
// claim stage needs no book parameter; a policy without one falls back to the
// nominal sum insured.
func TestSumInsuredSeverityReadsBaseSumInsured(t *testing.T) {
	params := ownDamageOnly(1, 0.01, 0.5, lob.CloseLagParams{Shape: 1.2, MeanDays: 40})
	book := func(base float64) []policy.Policy {
		var b []policy.Policy
		start := shared.NewDate(2000, time.January, 1)
		for i := 1; i <= 300; i++ {
			b = append(b, policy.Policy{
				ID: i, CoverStart: start, CoverEnd: start.AddDays(364), RiskFactor: 1,
				SumInsured: shared.FromDollars(1e7), BaseSumInsured: base, // far above any loss, so no cap
			})
		}
		return b
	}
	nominal := NewClaimSimulator(params).Simulate(random.NewSource(3), book(0))
	base := NewClaimSimulator(params).Simulate(random.NewSource(3), book(5e6))
	if len(nominal) == 0 || len(nominal) != len(base) {
		t.Fatalf("claim counts %d and %d, want equal and non-zero", len(nominal), len(base))
	}
	for i := range nominal {
		// Half the base sum insured, half the loss, to the cent.
		if got, want := base[i].Episodes[0].Ultimate.Dollars(), nominal[i].Episodes[0].Ultimate.Dollars()/2; got < want-0.01 || got > want+0.01 {
			t.Fatalf("claim %d: ultimate %.2f at half the base sum insured, want %.2f", i, got, want)
		}
	}
}
