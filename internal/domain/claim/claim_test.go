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

// A lognormal section draws a dollar loss around its median, uncapped by the
// sum insured and with no cover limit.
func TestLognormalClaimsCentreOnTheirMedian(t *testing.T) {
	p := only(params(), thirdParty, 0.15)
	p.Sections[thirdParty].Severity = lob.SeverityParams{Kind: lob.Lognormal, Median: 3000, Sigma: 0.8}
	claims := claim.NewClaimSimulator(p).Simulate(random.NewSource(11), fixedBook(40000, 2000, 0, 1.0))
	if len(claims) < 1000 {
		t.Fatalf("got %d claims, want plenty", len(claims))
	}
	costs := make([]float64, len(claims))
	exceeded := false
	for i, c := range claims {
		costs[i] = c.Episodes[0].Ultimate.Dollars()
		exceeded = exceeded || costs[i] > 2000
		if c.CoverLimit != 0 {
			t.Fatalf("claim %d has cover limit %v, want none", c.ID, c.CoverLimit)
		}
	}
	sort.Float64s(costs)
	if median := costs[len(costs)/2]; math.Abs(median/3000-1) > 0.05 {
		t.Errorf("median cost %v, want about 3000", median)
	}
	if !exceeded {
		t.Error("no claim exceeded the sum insured; a lognormal severity should be uncapped")
	}
}

// MR-8: a section that takes no excess reports and pays every ground-up loss
// on a policy with an excess, exactly as on a policy without one, on a
// sum-insured section as on a liability one.
func TestNoExcessSectionIgnoresThePolicyExcess(t *testing.T) {
	for _, section := range []int{ownDamage, thirdParty} {
		p := only(params(), section, 0.3)
		withExcess := claim.NewClaimSimulator(p).Simulate(random.NewSource(4), fixedBook(5000, 20000, 5000, 1.0))
		p.Sections[section].NoExcess = true
		got := claim.NewClaimSimulator(p).Simulate(random.NewSource(4), fixedBook(5000, 20000, 5000, 1.0))
		want := claim.NewClaimSimulator(p).Simulate(random.NewSource(4), fixedBook(5000, 20000, 0, 1.0))
		if !reflect.DeepEqual(got, want) {
			t.Errorf("section %d: no_excess claims on a $5,000-excess book differ from the same book at excess 0", section)
		}
		if len(withExcess) >= len(got) {
			t.Errorf("section %d: with the excess %d claims, want fewer than the %d without", section, len(withExcess), len(got))
		}
	}
}

// A lognormal_pareto section has a body of claims well below its scale, the
// floor a bare Pareto would put under every claim (MR-8), and its tail share
// of claims above it.
func TestLognormalParetoClaimsHaveABody(t *testing.T) {
	p := only(params(), thirdParty, 0.5)
	p.Sections[thirdParty].Severity = lob.SeverityParams{Kind: lob.LognormalPareto, Median: 4000, Sigma: 1.0, Scale: 25000, Alpha: 2.0}
	p.Sections[thirdParty].NoExcess = true
	claims := claim.NewClaimSimulator(p).Simulate(random.NewSource(9), fixedBook(20000, 20000, 500, 1.0))
	small, large := 0, 0
	for _, c := range claims {
		switch cost := c.Episodes[0].Ultimate.Dollars(); {
		case cost < 2000:
			small++
		case cost > 25000:
			large++
		}
	}
	share := float64(large) / float64(len(claims))
	if small < len(claims)/10 {
		t.Errorf("%d of %d claims below $2,000, want a body of small claims", small, len(claims))
	}
	if share < 0.03 || share > 0.045 {
		t.Errorf("share above the scale = %.4f, want about the 0.037 tail share", share)
	}
}

// A paying claim stays open at least the payment delay: its close lag is the
// delay plus the drawn lag, and nothing else about it moves. A nil claim,
// which pays nothing, keeps its drawn lag.
func TestPaymentDelayKeepsPayingClaimsOpen(t *testing.T) {
	p := params()
	p.NilProbability = 0.3
	book := fixedBook(5000, 20000, 0, 1.0)
	plain := claim.NewClaimSimulator(p).Simulate(random.NewSource(23), book)
	delayed := claim.NewClaimSimulator(p).WithPaymentDelay(7).Simulate(random.NewSource(23), book)
	if len(plain) == 0 || len(delayed) != len(plain) {
		t.Fatalf("got %d claims with the delay and %d without, want the same non-zero count", len(delayed), len(plain))
	}
	nils := 0
	for i := range plain {
		a, b := plain[i].Episodes[0], delayed[i].Episodes[0]
		if a.Open != b.Open || a.Nil != b.Nil || a.Ultimate != b.Ultimate {
			t.Fatalf("claim %d: the delay moved more than the close date", plain[i].ID)
		}
		want := a.Close.AddDays(7)
		if a.Nil {
			want = a.Close
			nils++
		}
		if b.Close != want {
			t.Fatalf("claim %d (nil %v): close %s with the delay, want %s", plain[i].ID, a.Nil, b.Close, want)
		}
	}
	if nils == 0 {
		t.Fatal("no nil claims at probability 0.3")
	}
}
