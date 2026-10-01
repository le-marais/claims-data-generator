package claim_test

import (
	"math"
	"reflect"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// Section indices in params.
const (
	ownDamage  = 0
	thirdParty = 1
)

// params is a two-section motor book: own damage sized off the sum insured,
// and a Pareto third-party section, 0.15 claims per policy-year between them.
func params() lob.ClaimParams {
	return lob.ClaimParams{
		Sections: []lob.SectionParams{
			{
				Name:          "own_damage",
				BaseFrequency: 0.1275,
				Severity:      lob.SeverityParams{Kind: lob.SumInsuredLognormal, MedianFraction: 0.12, Sigma: 1.0},
				ReportLag:     lob.ReportLagParams{Median: 2, Sigma: 1.2},
				CloseLag:      lob.CloseLagParams{Shape: 1.5, MeanDays: 60, SizeReference: 3000, SizeElasticity: 0.5, RiskLoading: 0.3},
				Recoveries:    true,
			},
			{
				Name:          "third_party",
				BaseFrequency: 0.0225,
				Severity:      lob.SeverityParams{Kind: lob.Pareto, Scale: 4000, Alpha: 2.2},
				ReportLag:     lob.ReportLagParams{Median: 2, Sigma: 1.2},
				CloseLag:      lob.CloseLagParams{Shape: 1.0, MeanDays: 900, RiskLoading: 0.3},
			},
		},
	}
}

// only switches every section but one off.
func only(p lob.ClaimParams, section int, frequency float64) lob.ClaimParams {
	p.Sections = slices.Clone(p.Sections)
	for i := range p.Sections {
		p.Sections[i].BaseFrequency = 0
	}
	p.Sections[section].BaseFrequency = frequency
	return p
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
		if c.Episodes[0].Ultimate <= 0 {
			t.Fatalf("claim %d has non-positive ultimate %v", c.ID, c.Episodes[0].Ultimate)
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
		if c.ReportDate().Before(c.OccurrenceDate) {
			t.Fatalf("report %s before occurrence %s", c.ReportDate(), c.OccurrenceDate)
		}
		if c.CloseDate().Before(c.ReportDate()) {
			t.Fatalf("close %s before report %s", c.CloseDate(), c.ReportDate())
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
		lags[i] = shared.DaysBetween(c.OccurrenceDate, c.ReportDate())
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
	// Sum insured is set well above 20000 so the cap at sum insured does not
	// bind and both small and big claims still occur.
	claims := sim.Simulate(random.NewSource(6), fixedBook(30000, 200000, 0, 1.0))
	// Only the own-damage section links settlement time to size, so scope the
	// check to it.
	var smallSum, smallN, bigSum, bigN float64
	for _, c := range claims {
		if c.Section != ownDamage {
			continue
		}
		lag := float64(shared.DaysBetween(c.ReportDate(), c.CloseDate()))
		if c.Episodes[0].Ultimate.Dollars() > 20000 {
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

func TestParetoClaimsExceedSumInsured(t *testing.T) {
	sim := claim.NewClaimSimulator(only(params(), thirdParty, 0.15))
	claims := sim.Simulate(random.NewSource(7), fixedBook(20000, 10000, 0, 1.0))
	exceeded := false
	for _, c := range claims {
		if c.Episodes[0].Ultimate.Dollars() > 10000 {
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
		if i > 0 && c.ReportDate().Before(claims[i-1].ReportDate()) {
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
		if !reflect.DeepEqual(a[i], b[i]) {
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
		baseTotal += int64(c.Episodes[0].Ultimate)
	}
	for _, c := range inflated {
		inflatedTotal += int64(c.Episodes[0].Ultimate)
	}
	if inflatedTotal <= baseTotal {
		t.Fatalf("inflated total ultimate %d not greater than base %d", inflatedTotal, baseTotal)
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
		if !reflect.DeepEqual(withoutCall[i], withIdentity[i]) {
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
		if c.Nil() {
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
		if c.Nil() {
			nils++
		}
	}
	if frac := float64(nils) / float64(len(claims)); frac < 0.7 {
		t.Fatalf("nil fraction %v, want most claims nil at probability 0.9", frac)
	}
}

// A claim carries its section, and only a sum-insured section sets a cover
// limit.
func TestClaimsCarryTheirSection(t *testing.T) {
	for _, section := range []int{ownDamage, thirdParty} {
		claims := claim.NewClaimSimulator(only(params(), section, 0.15)).Simulate(random.NewSource(31), fixedBook(2000, 20000, 0, 1.0))
		if len(claims) == 0 {
			t.Fatal("expected claims")
		}
		for _, c := range claims {
			if c.Section != section {
				t.Fatalf("claim %d in section %d, want %d", c.ID, c.Section, section)
			}
			if limited := c.CoverLimit > 0; limited != (section == ownDamage) {
				t.Fatalf("claim %d in section %d has cover limit %v", c.ID, section, c.CoverLimit)
			}
		}
	}
}

// Each section draws from its own sub-stream, so changing one section's
// parameters moves no claim of another.
func TestSectionsDrawIndependently(t *testing.T) {
	book := fixedBook(20000, 20000, 300, 1.0)
	before := claim.NewClaimSimulator(params()).Simulate(random.NewSource(5), book)
	p := params()
	p.Sections[thirdParty].ReportLag = lob.ReportLagParams{Median: 20, Sigma: 1.6}
	p.Sections[thirdParty].BaseFrequency *= 2
	after := claim.NewClaimSimulator(p).Simulate(random.NewSource(5), book)

	type key struct {
		policy           int
		occurred, report string
		cost             int64
	}
	ownDamageClaims := func(claims []claim.Claim) map[key]bool {
		m := map[key]bool{}
		for _, c := range claims {
			if c.Section == ownDamage {
				m[key{c.PolicyID, c.OccurrenceDate.String(), c.ReportDate().String(), int64(c.Episodes[0].Ultimate)}] = true
			}
		}
		return m
	}
	if !reflect.DeepEqual(ownDamageClaims(before), ownDamageClaims(after)) {
		t.Fatal("own-damage claims moved when the third-party section changed")
	}
	var tpLags []int
	for _, c := range after {
		if c.Section == thirdParty {
			tpLags = append(tpLags, shared.DaysBetween(c.OccurrenceDate, c.ReportDate()))
		}
	}
	sort.Ints(tpLags)
	if median := tpLags[len(tpLags)/2]; median < 16 || median > 24 {
		t.Fatalf("third-party median report lag %d days, want about 20", median)
	}
}
