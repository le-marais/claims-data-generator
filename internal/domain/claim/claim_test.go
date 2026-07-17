package claim_test

import (
	"math"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func params() lob.ClaimParams {
	return lob.ClaimParams{
		BaseFrequency:   0.15,
		ReportLagMedian: 2,
		ReportLagSigma:  1.2,
		Severity: lob.SeverityParams{
			ThirdPartyWeight:        0.15,
			OwnDamageMedianFraction: 0.12,
			OwnDamageSigma:          1.0,
			ThirdPartyScale:         4000,
			ThirdPartyAlpha:         2.2,
		},
		CloseLag: lob.CloseLagParams{
			Shape:          1.5,
			MeanDays:       60,
			SizeThreshold:  20000,
			SizeMultiplier: 4,
			RiskLoading:    0.3,
		},
	}
}

// fixedBook builds n identical policies starting through 1998.
func fixedBook(n int, sumInsured, excess float64, riskFactor float64) []policy.Policy {
	book := make([]policy.Policy, n)
	for i := range book {
		start := shared.NewDate(1998, time.January, 1).AddDays(i % 365)
		book[i] = policy.Policy{
			ID:         i + 1,
			CoverStart: start,
			CoverEnd:   start.AddDays(364),
			SumInsured: shared.FromDollars(sumInsured),
			Excess:     shared.FromDollars(excess),
			RiskFactor: riskFactor,
			Premium:    shared.FromDollars(sumInsured * 0.03 * riskFactor),
		}
	}
	return book
}

func TestClaimFrequencyMatchesBaseAndRiskFactor(t *testing.T) {
	p := params()
	sim := claim.NewClaimSimulator(p)
	// Excess 0 means no claims are discarded, so counts should match the
	// base frequency scaled by the risk factor.
	base := sim.Simulate(random.NewSource(1), fixedBook(30000, 20000, 0, 1.0))
	double := sim.Simulate(random.NewSource(2), fixedBook(30000, 20000, 0, 2.0))
	gotBase := float64(len(base)) / 30000
	gotDouble := float64(len(double)) / 30000
	if math.Abs(gotBase-0.15) > 0.01 {
		t.Errorf("frequency at risk factor 1 = %v, want ~0.15", gotBase)
	}
	if math.Abs(gotDouble-0.30) > 0.015 {
		t.Errorf("frequency at risk factor 2 = %v, want ~0.30", gotDouble)
	}
}

func TestClaimsBelowExcessAreDiscarded(t *testing.T) {
	sim := claim.NewClaimSimulator(params())
	// A large excess discards many small own-damage losses.
	withExcess := sim.Simulate(random.NewSource(3), fixedBook(20000, 20000, 1000, 1.0))
	noExcess := sim.Simulate(random.NewSource(3), fixedBook(20000, 20000, 0, 1.0))
	if len(withExcess) >= len(noExcess) {
		t.Errorf("excess 1000 produced %d claims, want fewer than %d at excess 0", len(withExcess), len(noExcess))
	}
	for _, c := range withExcess {
		if c.InitialEstimate <= 0 {
			t.Fatalf("claim %d has non-positive initial estimate %v", c.ID, c.InitialEstimate)
		}
	}
}

func TestClaimDateOrdering(t *testing.T) {
	sim := claim.NewClaimSimulator(params())
	book := fixedBook(20000, 20000, 500, 1.0)
	claims := sim.Simulate(random.NewSource(4), book)
	if len(claims) == 0 {
		t.Fatal("no claims generated")
	}
	byID := map[int]policy.Policy{}
	for _, p := range book {
		byID[p.ID] = p
	}
	for _, c := range claims {
		pol := byID[c.PolicyID]
		if c.OccurrenceDate.Before(pol.CoverStart) || c.OccurrenceDate.After(pol.CoverEnd) {
			t.Fatalf("occurrence %s outside cover %s..%s", c.OccurrenceDate, pol.CoverStart, pol.CoverEnd)
		}
		if c.ReportDate.Before(c.OccurrenceDate) {
			t.Fatalf("report %s before occurrence %s", c.ReportDate, c.OccurrenceDate)
		}
		if c.CloseDate.Before(c.ReportDate) {
			t.Fatalf("close %s before report %s", c.CloseDate, c.ReportDate)
		}
	}
}

func TestReportLagIsShortWithOutliers(t *testing.T) {
	sim := claim.NewClaimSimulator(params())
	claims := sim.Simulate(random.NewSource(5), fixedBook(30000, 20000, 0, 1.0))
	lags := make([]int, len(claims))
	within5 := 0
	over10 := 0
	for i, c := range claims {
		lags[i] = shared.DaysBetween(c.OccurrenceDate, c.ReportDate)
		if lags[i] <= 5 {
			within5++
		}
		if lags[i] > 10 {
			over10++
		}
	}
	if frac := float64(within5) / float64(len(claims)); frac < 0.6 {
		t.Errorf("only %v of report lags within 5 days, want most", frac)
	}
	if over10 == 0 {
		t.Error("no report lag outliers beyond 10 days, want some")
	}
}

func TestLargerClaimsCloseSlower(t *testing.T) {
	sim := claim.NewClaimSimulator(params())
	claims := sim.Simulate(random.NewSource(6), fixedBook(30000, 20000, 0, 1.0))
	var smallSum, smallN, bigSum, bigN float64
	for _, c := range claims {
		lag := float64(shared.DaysBetween(c.ReportDate, c.CloseDate))
		if c.InitialEstimate.Dollars() > 20000 {
			bigSum += lag
			bigN++
		} else {
			smallSum += lag
			smallN++
		}
	}
	if smallN == 0 || bigN == 0 {
		t.Fatalf("need both small (%v) and big (%v) claims", smallN, bigN)
	}
	if bigSum/bigN < 2*(smallSum/smallN) {
		t.Errorf("mean close lag big = %v, small = %v; want big at least 2x small", bigSum/bigN, smallSum/smallN)
	}
}

func TestThirdPartyClaimsExceedSumInsured(t *testing.T) {
	p := params()
	p.Severity.ThirdPartyWeight = 1.0
	sim := claim.NewClaimSimulator(p)
	claims := sim.Simulate(random.NewSource(7), fixedBook(20000, 10000, 0, 1.0))
	exceeded := false
	for _, c := range claims {
		if c.InitialEstimate.Dollars() > 10000 {
			exceeded = true
			break
		}
	}
	if !exceeded {
		t.Error("no third party claim exceeded the sum insured; severity should be uncapped")
	}
}

func TestClaimIDsSequentialAndSortedByReportDate(t *testing.T) {
	sim := claim.NewClaimSimulator(params())
	claims := sim.Simulate(random.NewSource(8), fixedBook(5000, 20000, 0, 1.0))
	for i, c := range claims {
		if c.ID != i+1 {
			t.Fatalf("claim %d has ID %d, want %d", i, c.ID, i+1)
		}
		if i > 0 && c.ReportDate.Before(claims[i-1].ReportDate) {
			t.Fatalf("claims not sorted by report date at index %d", i)
		}
	}
}

func TestSimulateClaimsIsDeterministic(t *testing.T) {
	sim := claim.NewClaimSimulator(params())
	book := fixedBook(2000, 20000, 500, 1.0)
	a := sim.Simulate(random.NewSource(42), book)
	b := sim.Simulate(random.NewSource(42), book)
	if len(a) != len(b) {
		t.Fatalf("lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("claim %d differs between identical runs", i)
		}
	}
}

func TestInflationScalesGroundUpLoss(t *testing.T) {
	// A homogeneous book with no report/close randomness knobs still yields a
	// higher total initial estimate once inflation is applied, because every
	// claim's ground-up loss is multiplied by its occurrence-year factor.
	book := fixedBook(30000, 20000, 0, 1.0)

	base := claim.NewClaimSimulator(params()).
		Simulate(random.NewSource(11), book)

	inflated := claim.NewClaimSimulator(params()).
		WithInflation(claim.NewInflationIndex(random.NewSource(11), lob.InflationParams{Mean: 2.0, Volatility: 0.0}, book[0].CoverStart.Year(), 3)).
		Simulate(random.NewSource(11), book)

	if len(base) == 0 || len(inflated) == 0 {
		t.Fatal("expected claims in both runs")
	}
	var baseTotal, inflatedTotal int64
	for _, c := range base {
		baseTotal += int64(c.InitialEstimate)
	}
	for _, c := range inflated {
		inflatedTotal += int64(c.InitialEstimate)
	}
	if inflatedTotal <= baseTotal {
		t.Fatalf("inflated total estimate %d not greater than base %d", inflatedTotal, baseTotal)
	}
}

func TestNoInflationMatchesIdentity(t *testing.T) {
	book := fixedBook(30000, 20000, 0, 1.0)
	withoutCall := claim.NewClaimSimulator(params()).Simulate(random.NewSource(12), book)
	withIdentity := claim.NewClaimSimulator(params()).
		WithInflation(claim.NewInflationIndex(random.NewSource(99), lob.InflationParams{Mean: 1.0, Volatility: 0.0}, book[0].CoverStart.Year(), 3)).
		Simulate(random.NewSource(12), book)
	if len(withoutCall) != len(withIdentity) {
		t.Fatalf("identity inflation changed claim count: %d vs %d", len(withoutCall), len(withIdentity))
	}
	for i := range withoutCall {
		if withoutCall[i] != withIdentity[i] {
			t.Fatalf("identity inflation changed claim %d", i)
		}
	}
}

func TestNilProbabilityZeroFlagsNoClaims(t *testing.T) {
	p := params()
	p.NilProbability = 0
	claims := claim.NewClaimSimulator(p).Simulate(random.NewSource(21), fixedBook(30000, 20000, 0, 1.0))
	if len(claims) == 0 {
		t.Fatal("expected claims")
	}
	for _, c := range claims {
		if c.Nil {
			t.Fatalf("claim %d flagged nil with probability 0", c.ID)
		}
	}
}

func TestNilProbabilityHighFlagsMostClaims(t *testing.T) {
	p := params()
	p.NilProbability = 0.9
	claims := claim.NewClaimSimulator(p).Simulate(random.NewSource(22), fixedBook(30000, 20000, 0, 1.0))
	if len(claims) < 20 {
		t.Fatalf("expected a meaningful number of claims, got %d", len(claims))
	}
	nils := 0
	for _, c := range claims {
		if c.Nil {
			nils++
		}
	}
	if frac := float64(nils) / float64(len(claims)); frac < 0.7 {
		t.Fatalf("nil fraction %v, want most claims nil at probability 0.9", frac)
	}
}

func TestOwnDamageFlagFollowsSeverityMixture(t *testing.T) {
	allOwn := params()
	allOwn.Severity.ThirdPartyWeight = 0
	claims := claim.NewClaimSimulator(allOwn).Simulate(random.NewSource(31), fixedBook(2000, 20000, 0, 1.0))
	if len(claims) == 0 {
		t.Fatal("expected claims")
	}
	for _, c := range claims {
		if !c.OwnDamage {
			t.Fatalf("claim %d not flagged own-damage with third_party_weight 0", c.ID)
		}
	}

	allThird := params()
	allThird.Severity.ThirdPartyWeight = 1
	claims = claim.NewClaimSimulator(allThird).Simulate(random.NewSource(32), fixedBook(2000, 20000, 0, 1.0))
	if len(claims) == 0 {
		t.Fatal("expected claims")
	}
	for _, c := range claims {
		if c.OwnDamage {
			t.Fatalf("claim %d flagged own-damage with third_party_weight 1", c.ID)
		}
	}
}
