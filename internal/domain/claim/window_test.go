package claim

import (
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func windowParams() lob.ClaimParams {
	return lob.ClaimParams{
		BaseFrequency:   3.0, // high, so tail policies would spill without windowing
		ReportLagMedian: 2,
		ReportLagSigma:  1.2,
		Severity: lob.SeverityParams{
			ThirdPartyWeight:        0.2,
			OwnDamageMedianFraction: 0.12,
			OwnDamageSigma:          1.0,
			ThirdPartyScale:         4000,
			ThirdPartyAlpha:         2.2,
		},
		CloseLag: lob.CloseLagParams{Shape: 1.2, MeanDays: 120, SizeThreshold: 20000, SizeMultiplier: 6, RiskLoading: 0.3, ThirdPartyShape: 1.0, ThirdPartyMeanDays: 680},
	}
}

// lateBook writes policies deep in the final underwriting year, whose 12-month
// cover spills into the year after the window.
func lateBook(startYear, years, n int) []policy.Policy {
	var b []policy.Policy
	lastUY := startYear + years - 1
	start := shared.NewDate(lastUY, time.December, 1) // cover runs into lastUY+1
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

func TestWindowedOccurrencesStayInWindow(t *testing.T) {
	const startYear, years = 1998, 10
	windowEnd := shared.NewDate(startYear+years, time.January, 1)
	claims := NewClaimSimulator(windowParams()).
		WithWindow(startYear, years).
		Simulate(random.NewSource(1), lateBook(startYear, years, 2000))
	if len(claims) == 0 {
		t.Fatal("no claims generated")
	}
	for _, c := range claims {
		if !c.OccurrenceDate.Before(windowEnd) {
			t.Fatalf("claim %d occurred %s, on/after window end %s", c.ID, c.OccurrenceDate, windowEnd)
		}
	}
}

// boundaryBook writes policies whose CoverEnd lands exactly on windowEnd:
// startYear=1998, years=10 puts the last underwriting year at 2007, a
// non-leap year, so a Jan-2 cover start plus a 364-day (12-month) term ends
// exactly on Jan 1 1998+10 = windowEnd. This exercises the equality boundary
// that a strict "Before" clamp check would miss (MF-2 regression).
func boundaryBook(startYear, years, n int) []policy.Policy {
	var b []policy.Policy
	lastUY := startYear + years - 1
	start := shared.NewDate(lastUY, time.January, 2)
	for i := 1; i <= n; i++ {
		b = append(b, policy.Policy{
			ID:         i,
			CoverStart: start,
			CoverEnd:   start.AddDays(364), // == windowEnd exactly for this startYear/years
			SumInsured: shared.FromDollars(20000),
			Excess:     shared.FromDollars(300),
			RiskFactor: 1.0,
		})
	}
	return b
}

func TestWindowedOccurrencesStayInWindowAtExactBoundary(t *testing.T) {
	const startYear, years = 1998, 10
	windowEnd := shared.NewDate(startYear+years, time.January, 1)
	book := boundaryBook(startYear, years, 1)
	if !book[0].CoverEnd.Equal(windowEnd) {
		t.Fatalf("test setup invalid: CoverEnd %s does not equal windowEnd %s", book[0].CoverEnd, windowEnd)
	}
	claims := NewClaimSimulator(windowParams()).
		WithWindow(startYear, years).
		Simulate(random.NewSource(1), boundaryBook(startYear, years, 2000))
	if len(claims) == 0 {
		t.Fatal("no claims generated")
	}
	for _, c := range claims {
		if !c.OccurrenceDate.Before(windowEnd) {
			t.Fatalf("claim %d occurred %s, on/after window end %s", c.ID, c.OccurrenceDate, windowEnd)
		}
	}
}
