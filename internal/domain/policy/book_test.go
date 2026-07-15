package policy_test

import (
	"math"
	"sort"
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
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
		PremiumRateFactor: 0.03,
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
	sim := policy.NewBookSimulator(p)
	book := sim.Simulate(random.NewSource(1), 1998, 3, 100)
	counts := countByStartYear(book)
	// 100, round(100*1.05)=105, round(105*1.05)=110
	want := map[int]int{1998: 100, 1999: 105, 2000: 110}
	for year, n := range want {
		if counts[year] != n {
			t.Errorf("year %d count = %d, want %d", year, counts[year], n)
		}
	}
	if len(book) != 315 {
		t.Errorf("total policies = %d, want 315", len(book))
	}
}

func TestBookSizeCanShrinkSomeYears(t *testing.T) {
	p := params()
	p.GrowthFactor = 1.02
	p.SizeVolatility = 0.10
	sim := policy.NewBookSimulator(p)
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
	sim := policy.NewBookSimulator(params())
	book := sim.Simulate(random.NewSource(1), 1998, 2, 50)
	for i, p := range book {
		if p.ID != i+1 {
			t.Fatalf("policy %d has ID %d, want %d", i, p.ID, i+1)
		}
	}
}

func TestPolicyFieldConsistency(t *testing.T) {
	prm := params()
	sim := policy.NewBookSimulator(prm)
	book := sim.Simulate(random.NewSource(2), 1998, 3, 500)
	validExcess := map[float64]bool{0: true, 100: true, 300: true, 500: true, 1000: true}
	for _, p := range book {
		if p.CoverStart.Year() < 1998 || p.CoverStart.Year() > 2000 {
			t.Fatalf("cover start %s outside simulated years", p.CoverStart)
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
		wantPremium := p.SumInsured.Dollars() * prm.PremiumRateFactor * p.RiskFactor
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
	sim := policy.NewBookSimulator(p)
	book := sim.Simulate(random.NewSource(4), 1998, 2, 20000)
	var y1, y2 []float64
	for _, pol := range book {
		si := pol.SumInsured.Dollars()
		if pol.CoverStart.Year() == 1998 {
			y1 = append(y1, si)
		} else {
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
	sim := policy.NewBookSimulator(params())
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
	sim := policy.NewBookSimulator(params())
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
	sim := policy.NewBookSimulator(params())
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
