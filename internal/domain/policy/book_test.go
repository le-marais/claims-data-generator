package policy_test

import (
	"math"
	"sort"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func params() lob.BookParams {
	return lob.BookParams{
		GrowthFactor:        1.05,
		SizeVolatility:      0.06,
		Spread:              0.4,
		SumInsuredMedian:    20000,
		SumInsuredInflation: 1.03,
		ExcessChoices: []lob.ExcessChoice{
			{Value: 0, Weight: 0.1},
			{Value: 100, Weight: 0.2},
			{Value: 300, Weight: 0.3},
			{Value: 500, Weight: 0.3},
			{Value: 1000, Weight: 0.1},
		},
	}
}

func pricingParams() lob.PricingParams {
	return lob.PricingParams{
		TargetLossRatio: 0.72,
		BaseFrequency:   0.12,
		Severity: lob.SeverityParams{
			ThirdPartyWeight:        0.20,
			OwnDamageMedianFraction: 0.12,
			OwnDamageSigma:          1.0,
			ThirdPartyScale:         4000,
			ThirdPartyAlpha:         2.2,
		},
		ReopenProbability:    0.04,
		ReopenEstimateFactor: 0.45,
		InflationMean:        1.04,
	}
}

func countByStartYear(book []policy.Policy) map[int]int {
	counts := map[int]int{}
	for _, p := range book {
		counts[p.CoverStart.Year()]++
	}
	return counts
}

func TestBookSizeFollowsRecursionWithoutVolatility(t *testing.T) {
	p := params()
	p.SizeVolatility = 0
	sim := policy.NewBookSimulator(p, pricingParams())
	book := sim.Simulate(random.NewSource(1), 1998, 3, 100)
	counts := countByStartYear(book)
	// Warm-up round(100/1.05)=95, then 100, round(100*1.05)=105, round(105*1.05)=110
	want := map[int]int{1997: 95, 1998: 100, 1999: 105, 2000: 110}
	for year, n := range want {
		if counts[year] != n {
			t.Errorf("year %d count = %d, want %d", year, counts[year], n)
		}
	}
	if len(book) != 410 {
		t.Errorf("total policies = %d, want 410", len(book))
	}
}

func TestBookSizeCanShrinkSomeYears(t *testing.T) {
	p := params()
	p.GrowthFactor = 1.02
	p.SizeVolatility = 0.10
	sim := policy.NewBookSimulator(p, pricingParams())
	book := sim.Simulate(random.NewSource(3), 1998, 15, 1000)
	counts := countByStartYear(book)
	years := make([]int, 0, len(counts))
	for y := range counts {
		years = append(years, y)
	}
	sort.Ints(years)
	shrinks := 0
	for i := 1; i < len(years); i++ {
		if counts[years[i]] < counts[years[i-1]] {
			shrinks++
		}
	}
	if shrinks == 0 {
		t.Error("expected at least one shrinking year over 15 years at 10% volatility")
	}
}

func TestPolicyIDsAreSequential(t *testing.T) {
	sim := policy.NewBookSimulator(params(), pricingParams())
	book := sim.Simulate(random.NewSource(1), 1998, 2, 50)
	for i, p := range book {
		if p.ID != i+1 {
			t.Fatalf("policy %d has ID %d, want %d", i, p.ID, i+1)
		}
	}
}

func TestPolicyFieldConsistency(t *testing.T) {
	prm := params()
	sim := policy.NewBookSimulator(prm, pricingParams())
	book := sim.Simulate(random.NewSource(2), 1998, 3, 500)
	validExcess := map[float64]bool{0: true, 100: true, 300: true, 500: true, 1000: true}
	for _, p := range book {
		if p.CoverStart.Year() < 1997 || p.CoverStart.Year() > 2000 {
			t.Fatalf("cover start %s outside the simulated years and the warm-up year", p.CoverStart)
		}
		if got := p.CoverStart.AddDays(364); got != p.CoverEnd {
			t.Fatalf("cover end %s, want %s (12-month term)", p.CoverEnd, got)
		}
		if p.SumInsured <= 0 {
			t.Fatalf("sum insured %v not positive", p.SumInsured)
		}
		if !validExcess[p.Excess.Dollars()] {
			t.Fatalf("excess %v not in configured set", p.Excess)
		}
		if p.RiskFactor <= 0 {
			t.Fatalf("risk factor %v not positive", p.RiskFactor)
		}
		pp := pricingParams()
		yearOffset := float64(p.CoverStart.Year() - 1998)
		// Pricing trends the loss cost to the middle of the cover (MR-3).
		infl := math.Pow(pp.InflationMean, shared.TrendYears(p.CoverStart.AddDays(182), 1998))
		siDrift := math.Pow(prm.SumInsuredInflation, yearOffset)
		wantPremium := pp.ExpectedPolicyLoss(p.SumInsured.Dollars(), p.Excess.Dollars(), p.RiskFactor, infl, siDrift) / pp.TargetLossRatio
		if math.Abs(p.Premium.Dollars()-wantPremium) > 0.01 {
			t.Fatalf("premium %v, want %v", p.Premium.Dollars(), wantPremium)
		}
	}
}

func TestSumInsuredMedianInflatesAcrossYears(t *testing.T) {
	p := params()
	p.Spread = 0.05 // near-homogeneous so medians are tight
	p.GrowthFactor = 1.0
	p.SizeVolatility = 0
	sim := policy.NewBookSimulator(p, pricingParams())
	book := sim.Simulate(random.NewSource(4), 1998, 2, 20000)
	var y1, y2 []float64
	for _, pol := range book {
		si := pol.SumInsured.Dollars()
		switch pol.CoverStart.Year() {
		case 1998:
			y1 = append(y1, si)
		case 1999:
			y2 = append(y2, si)
		}
	}
	sort.Float64s(y1)
	sort.Float64s(y2)
	m1 := y1[len(y1)/2]
	m2 := y2[len(y2)/2]
	ratio := m2 / m1
	if math.Abs(ratio-1.03) > 0.02 {
		t.Errorf("median ratio year2/year1 = %v, want ~1.03", ratio)
	}
	if math.Abs(m1-20000)/20000 > 0.02 {
		t.Errorf("year-1 median = %v, want ~20000", m1)
	}
}

func TestRiskFactorMeanIsOne(t *testing.T) {
	sim := policy.NewBookSimulator(params(), pricingParams())
	book := sim.Simulate(random.NewSource(5), 1998, 1, 50000)
	sum := 0.0
	for _, p := range book {
		sum += p.RiskFactor
	}
	mean := sum / float64(len(book))
	if math.Abs(mean-1) > 0.03 {
		t.Errorf("risk factor mean = %v, want ~1", mean)
	}
}

func TestExcessWeightsAreRespected(t *testing.T) {
	sim := policy.NewBookSimulator(params(), pricingParams())
	book := sim.Simulate(random.NewSource(6), 1998, 1, 50000)
	freq := map[float64]float64{}
	for _, p := range book {
		freq[p.Excess.Dollars()]++
	}
	want := map[float64]float64{0: 0.1, 100: 0.2, 300: 0.3, 500: 0.3, 1000: 0.1}
	for value, w := range want {
		got := freq[value] / float64(len(book))
		if math.Abs(got-w) > 0.02 {
			t.Errorf("excess %v frequency = %v, want ~%v", value, got, w)
		}
	}
}

func TestSimulateIsDeterministic(t *testing.T) {
	sim := policy.NewBookSimulator(params(), pricingParams())
	a := sim.Simulate(random.NewSource(42), 1998, 3, 200)
	b := sim.Simulate(random.NewSource(42), 1998, 3, 200)
	if len(a) != len(b) {
		t.Fatalf("lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("policy %d differs between identical runs", i)
		}
	}
}

func TestProjectedSizeMatchesTheGrowthRule(t *testing.T) {
	book := params() // growth 1.05
	// Warm-up round(100/1.05)=95, then 100 + 105 + 110.25, before rounding.
	if got, want := policy.ProjectedSize(book, 3, 100), 95+315.25; math.Abs(got-want) > 1e-9 {
		t.Fatalf("ProjectedSize = %v, want %v", got, want)
	}
	// A shrinking book floors at one policy a year, as Simulate does; its
	// warm-up year is the first year with the shrink undone.
	book.GrowthFactor = 0.5
	if got, want := policy.ProjectedSize(book, 4, 2), 4.0+2+1+1+1; got != want {
		t.Fatalf("shrinking ProjectedSize = %v, want %v", got, want)
	}
	// An invalid growth factor, seen before validation, takes nothing off.
	book.GrowthFactor = 0
	if got := policy.ProjectedSize(book, 1, 10); got != 20 {
		t.Fatalf("zero-growth ProjectedSize = %v, want 20", got)
	}
}

func TestProjectedSizeMatchesTheSimulatedBookWithoutNoise(t *testing.T) {
	// The projection is the noise-free path, so with the size noise switched
	// off it should be exactly the book Simulate writes.
	book := params()
	book.SizeVolatility = 0
	got := len(policy.NewBookSimulator(book, pricingParams()).Simulate(random.NewSource(3), 1998, 8, 400))
	if want := policy.ProjectedSize(book, 8, 400); math.Abs(float64(got)-want) > 8 {
		t.Fatalf("simulated %d policies, projection %v", got, want)
	}
}

// priceFactor is a policy's premium over its assumed expected loss: one over
// the target loss ratio its underwriting year was priced to.
func priceFactor(p policy.Policy, prm lob.BookParams, pp lob.PricingParams) float64 {
	yearOffset := float64(p.CoverStart.Year() - 1998)
	infl := math.Pow(pp.InflationMean, shared.TrendYears(p.CoverStart.AddDays(182), 1998))
	siDrift := math.Pow(prm.SumInsuredInflation, yearOffset)
	return p.Premium.Dollars() / pp.ExpectedPolicyLoss(p.SumInsured.Dollars(), p.Excess.Dollars(), p.RiskFactor, infl, siDrift)
}

// Adequacy volatility prices every policy in an underwriting year to the same
// noisy target loss ratio, a different one each year, and moves nothing but
// premium.
func TestAdequacyVolatilityPricesEachYearToItsOwnTarget(t *testing.T) {
	prm := params()
	plain := policy.NewBookSimulator(prm, pricingParams()).Simulate(random.NewSource(4), 1998, 5, 300)
	pp := pricingParams()
	pp.AdequacyVolatility = 0.1
	noisy := policy.NewBookSimulator(prm, pp).Simulate(random.NewSource(4), 1998, 5, 300)

	if len(noisy) != len(plain) {
		t.Fatalf("book size moved: %d policies, want %d", len(noisy), len(plain))
	}
	factorByYear := map[int]float64{}
	for i, p := range noisy {
		q := plain[i]
		if p.CoverStart != q.CoverStart || p.SumInsured != q.SumInsured || p.Excess != q.Excess || p.RiskFactor != q.RiskFactor {
			t.Fatalf("policy %d: adequacy volatility moved a non-premium field", p.ID)
		}
		f := priceFactor(p, prm, pp)
		year := p.CoverStart.Year()
		if want, ok := factorByYear[year]; !ok {
			factorByYear[year] = f
		} else if math.Abs(f/want-1) > 1e-4 {
			t.Fatalf("policy %d: price factor %.6f, want the year's %.6f", p.ID, f, want)
		}
	}
	distinct := map[float64]bool{}
	for _, f := range factorByYear {
		distinct[math.Round(f*1e4)/1e4] = true
	}
	if len(distinct) != len(factorByYear) {
		t.Fatalf("underwriting years share a target loss ratio: %v", factorByYear)
	}
	for year, f := range factorByYear {
		if lr := 1 / f; math.Abs(lr/pp.TargetLossRatio-1) > 0.5 {
			t.Errorf("year %d priced to loss ratio %.3f, implausibly far from target %.2f", year, lr, pp.TargetLossRatio)
		}
	}
}

// MR-6: the book carries a warm-up underwriting year before the window, so
// policies are in force from the window's first day. The first window year
// keeps the initial size.
func TestSimulateWritesAWarmUpYear(t *testing.T) {
	p := params()
	book := policy.NewBookSimulator(p, pricingParams()).Simulate(random.NewSource(7), 1998, 2, 1000)
	counts := countByStartYear(book)
	if counts[1997] != 952 || counts[1998] != 1000 { // round(1000/1.05) = 952
		t.Fatalf("policies by start year %v, want 952 in the 1997 warm-up and 1000 in 1998", counts)
	}
	windowStart := shared.NewDate(1998, time.January, 1)
	inForce := 0
	for _, pol := range book {
		if pol.CoverStart.Before(windowStart) && !pol.CoverEnd.Before(windowStart) {
			inForce++
		}
	}
	if inForce < 900 {
		t.Fatalf("%d policies in force on the window's first day, want nearly all of the warm-up year", inForce)
	}
}

// RF-14: each policy records its sum insured in start-year dollars, deflated
// by the book's drift to its underwriting year, warm-up year included.
func TestPolicyRecordsBaseSumInsured(t *testing.T) {
	prm := params()
	book := policy.NewBookSimulator(prm, pricingParams()).Simulate(random.NewSource(8), 1998, 3, 200)
	for _, p := range book {
		offset := float64(p.CoverStart.Year() - 1998)
		want := p.SumInsured.Dollars() / math.Pow(prm.SumInsuredInflation, offset)
		if p.BaseSumInsured != want {
			t.Fatalf("policy %d (written %d): base sum insured %v, want %v", p.ID, p.CoverStart.Year(), p.BaseSumInsured, want)
		}
	}
}
