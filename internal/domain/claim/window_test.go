package claim

import (
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// windowParams has a high frequency, so tail policies would spill without
// windowing.
func windowParams() lob.ClaimParams {
	closeLag := lob.CloseLagParams{Shape: 1.2, MeanDays: 120, RiskLoading: 0.3}
	return lob.ClaimParams{Sections: []lob.SectionParams{
		{
			Name: "own_damage", BaseFrequency: 2.4,
			Severity:  lob.SeverityParams{Kind: lob.SumInsuredLognormal, MedianFraction: 0.12, Sigma: 1.0},
			ReportLag: lob.ReportLagParams{Median: 2, Sigma: 1.2}, CloseLag: closeLag,
		},
		{
			Name: "third_party", BaseFrequency: 0.6,
			Severity:  lob.SeverityParams{Kind: lob.Pareto, Scale: 4000, Alpha: 2.2},
			ReportLag: lob.ReportLagParams{Median: 2, Sigma: 1.2}, CloseLag: closeLag,
		},
	}}
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

// exposedFraction counts cover days, CoverStart to CoverEnd inclusive, against
// the exclusive window end (MR-11): a 365-day cover with 364 days in the
// window is exposed 364/365, not 364/364.
func TestExposedFractionCountsCoverDays(t *testing.T) {
	const startYear, years = 1998, 10
	sim := NewClaimSimulator(windowParams()).WithWindow(startYear, years)
	cover := func(start shared.Date) policy.Policy {
		return policy.Policy{CoverStart: start, CoverEnd: start.AddDays(364)}
	}
	cases := []struct {
		name string
		pol  policy.Policy
		want float64
	}{
		{"inside the window", cover(shared.NewDate(2007, time.January, 1)), 1},
		{"ends on the window end", cover(shared.NewDate(2007, time.January, 2)), 364.0 / 365},
		{"half out", cover(shared.NewDate(2007, time.July, 2)), 183.0 / 365},
		{"one day in", cover(shared.NewDate(2007, time.December, 31)), 1.0 / 365},
	}
	for _, c := range cases {
		if got := sim.exposedFraction(c.pol); got != c.want {
			t.Errorf("%s: exposedFraction = %v, want %v", c.name, got, c.want)
		}
	}
	if got := NewClaimSimulator(windowParams()).exposedFraction(cover(shared.NewDate(2007, time.July, 2))); got != 1 {
		t.Errorf("no window: exposedFraction = %v, want 1", got)
	}
}

// MR-6: warm-up policies written before the window yield claims only inside
// it, at a frequency pro-rated to their in-window cover.
func TestWindowedOccurrencesStartAtWindowStart(t *testing.T) {
	const startYear, years = 1998, 10
	windowStart := shared.NewDate(startYear, time.January, 1)
	var book []policy.Policy
	for i := 1; i <= 2000; i++ {
		start := shared.NewDate(startYear-1, time.July, 2) // half the cover before the window
		book = append(book, policy.Policy{
			ID: i, CoverStart: start, CoverEnd: start.AddDays(364),
			SumInsured: shared.FromDollars(20000), Excess: shared.FromDollars(300), RiskFactor: 1.0,
		})
	}
	sim := NewClaimSimulator(windowParams()).WithWindow(startYear, years)
	if got, want := sim.exposedFraction(book[0]), 182.0/365; got != want {
		t.Fatalf("exposedFraction = %v, want %v", got, want)
	}
	claims := sim.Simulate(random.NewSource(1), book)
	if len(claims) == 0 {
		t.Fatal("no claims generated")
	}
	for _, c := range claims {
		if c.OccurrenceDate.Before(windowStart) {
			t.Fatalf("claim %d occurred %s, before window start %s", c.ID, c.OccurrenceDate, windowStart)
		}
	}
	if got := sim.exposedFraction(policy.Policy{CoverStart: shared.NewDate(1996, time.March, 1), CoverEnd: shared.NewDate(1997, time.February, 28)}); got != 0 {
		t.Fatalf("cover entirely before the window: exposedFraction = %v, want 0", got)
	}
}
