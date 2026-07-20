package triangle_test

import (
	"math"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Two claims: one occurring 1998 paid over 1998-1999, one occurring 1999
// paid in 1999.
func fixtures() ([]claim.Claim, []transaction.Transaction) {
	claims := []claim.Claim{
		{
			ID: 1, PolicyID: 1,
			OccurrenceDate:  shared.NewDate(1998, time.March, 1),
			ReportDate:      shared.NewDate(1998, time.March, 3),
			CloseDate:       shared.NewDate(1999, time.February, 1),
			InitialEstimate: shared.FromDollars(1000),
		},
		{
			ID: 2, PolicyID: 2,
			OccurrenceDate:  shared.NewDate(1999, time.June, 1),
			ReportDate:      shared.NewDate(1999, time.June, 2),
			CloseDate:       shared.NewDate(1999, time.July, 1),
			InitialEstimate: shared.FromDollars(500),
		},
	}
	txs := []transaction.Transaction{
		// Claim 1: initial estimate 1000; pay 600 in 1998; revise up; pay 500 at close in 1999.
		{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.March, 3), Type: transaction.Estimate, Amount: shared.FromDollars(1000)},
		{ID: 2, ClaimID: 1, Date: shared.NewDate(1998, time.June, 1), Type: transaction.Payment, Amount: shared.FromDollars(600)},
		{ID: 3, ClaimID: 1, Date: shared.NewDate(1998, time.June, 1), Type: transaction.Estimate, Amount: shared.FromDollars(-600)},
		{ID: 4, ClaimID: 1, Date: shared.NewDate(1998, time.December, 1), Type: transaction.Estimate, Amount: shared.FromDollars(100)},
		{ID: 5, ClaimID: 1, Date: shared.NewDate(1999, time.February, 1), Type: transaction.Payment, Amount: shared.FromDollars(500)},
		{ID: 6, ClaimID: 1, Date: shared.NewDate(1999, time.February, 1), Type: transaction.Estimate, Amount: shared.FromDollars(-500)},
		// Claim 2: initial estimate 500; settle 450 in 1999.
		{ID: 7, ClaimID: 2, Date: shared.NewDate(1999, time.June, 2), Type: transaction.Estimate, Amount: shared.FromDollars(500)},
		{ID: 8, ClaimID: 2, Date: shared.NewDate(1999, time.July, 1), Type: transaction.Payment, Amount: shared.FromDollars(450)},
		{ID: 9, ClaimID: 2, Date: shared.NewDate(1999, time.July, 1), Type: transaction.Estimate, Amount: shared.FromDollars(-450)},
		{ID: 10, ClaimID: 2, Date: shared.NewDate(1999, time.July, 1), Type: transaction.Estimate, Amount: shared.FromDollars(-50)},
	}
	return claims, txs
}

func TestPaidTriangleAggregatesCumulativePayments(t *testing.T) {
	claims, txs := fixtures()
	tri := triangle.PaidTriangle(claims, txs, 1998, 2, 3)
	// Origin 1998: dev 0 = 600, dev 1 = 1100 (cumulative), dev 2 = 1100.
	if !approx(tri.Cells[0][0], 600) || !approx(tri.Cells[0][1], 1100) || !approx(tri.Cells[0][2], 1100) {
		t.Errorf("origin 1998 = %v, want [600 1100 1100]", tri.Cells[0])
	}
	// Origin 1999: dev 0 = 450.
	if !approx(tri.Cells[1][0], 450) {
		t.Errorf("origin 1999 dev 0 = %v, want 450", tri.Cells[1][0])
	}
	if tri.StartYear != 1998 {
		t.Errorf("StartYear = %d, want 1998", tri.StartYear)
	}
}

func TestIncurredTriangleIsPaidPlusOutstanding(t *testing.T) {
	claims, txs := fixtures()
	tri := triangle.IncurredTriangle(claims, txs, 1998, 2, 3)
	// Origin 1998 dev 0: paid 600 + outstanding (1000-600+100) = 1100.
	if !approx(tri.Cells[0][0], 1100) {
		t.Errorf("origin 1998 dev 0 = %v, want 1100", tri.Cells[0][0])
	}
	// Dev 1: claim closed, incurred = paid = 1100.
	if !approx(tri.Cells[0][1], 1100) {
		t.Errorf("origin 1998 dev 1 = %v, want 1100", tri.Cells[0][1])
	}
	// Origin 1999 dev 0: settled at 450 within the year.
	if !approx(tri.Cells[1][0], 450) {
		t.Errorf("origin 1999 dev 0 = %v, want 450", tri.Cells[1][0])
	}
}

func TestEarnedPremiumSplitsAcrossCalendarYears(t *testing.T) {
	policies := []policy.Policy{
		{
			ID:         1,
			CoverStart: shared.NewDate(1998, time.October, 1),
			CoverEnd:   shared.NewDate(1998, time.October, 1).AddDays(364),
			Premium:    shared.FromDollars(365), // one dollar per cover day
		},
	}
	ep := triangle.EarnedPremiumByYear(policies, 1998, 2)
	// Oct 1 1998 .. Dec 31 1998 = 92 days; rest earned in 1999.
	if !approx(ep[0], 92) {
		t.Errorf("1998 earned premium = %v, want 92", ep[0])
	}
	if !approx(ep[1], 273) {
		t.Errorf("1999 earned premium = %v, want 273", ep[1])
	}
}

func TestATAFactorsAreVolumeWeighted(t *testing.T) {
	tri := triangle.Triangle{
		StartYear: 1998,
		Cells: [][]float64{
			{100, 200, 220},
			{300, 450},
		},
	}
	factors := tri.ATAFactors()
	// Age 0->1: (200+450)/(100+300) = 1.625; only origin 0 has age 1->2: 220/200 = 1.1.
	if len(factors) != 2 || !approx(factors[0], 1.625) || !approx(factors[1], 1.1) {
		t.Errorf("ATA factors = %v, want [1.625 1.1]", factors)
	}
}

func TestNetPaidTriangleSubtractsRecoveries(t *testing.T) {
	claims := []claim.Claim{{ID: 1, OccurrenceDate: shared.NewDate(1998, time.March, 1)}}
	txs := []transaction.Transaction{
		{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.April, 1), Type: transaction.Payment, Amount: shared.FromDollars(1000)},
		{ID: 2, ClaimID: 1, Date: shared.NewDate(1999, time.June, 1), Type: transaction.Salvage, Amount: shared.FromDollars(150)},
		{ID: 3, ClaimID: 1, Date: shared.NewDate(2000, time.June, 1), Type: transaction.Subrogation, Amount: shared.FromDollars(300)},
	}
	gross := triangle.PaidTriangle(claims, txs, 1998, 3, 3)
	if got := gross.Cells[0]; got[0] != 1000 || got[1] != 1000 || got[2] != 1000 {
		t.Fatalf("gross paid row = %v, want [1000 1000 1000]", got)
	}
	net := triangle.NetPaidTriangle(claims, txs, 1998, 3, 3)
	if got := net.Cells[0]; got[0] != 1000 || got[1] != 850 || got[2] != 550 {
		t.Fatalf("net paid row = %v, want [1000 850 550]", got)
	}
}

func TestIncurredTriangleSubtractsRecoveries(t *testing.T) {
	claims := []claim.Claim{{ID: 1, OccurrenceDate: shared.NewDate(1998, time.March, 1)}}
	txs := []transaction.Transaction{
		{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.March, 10), Type: transaction.Estimate, Amount: shared.FromDollars(1000)},
		{ID: 2, ClaimID: 1, Date: shared.NewDate(1998, time.April, 1), Type: transaction.Payment, Amount: shared.FromDollars(1000)},
		{ID: 3, ClaimID: 1, Date: shared.NewDate(1998, time.April, 1), Type: transaction.Estimate, Amount: shared.FromDollars(-1000)},
		{ID: 4, ClaimID: 1, Date: shared.NewDate(1999, time.June, 1), Type: transaction.Salvage, Amount: shared.FromDollars(150)},
	}
	incurred := triangle.IncurredTriangle(claims, txs, 1998, 2, 2)
	if got := incurred.Cells[0]; got[0] != 1000 || got[1] != 850 {
		t.Fatalf("incurred row = %v, want [1000 850] (gross case + net paid)", got)
	}
}

func TestPercentileInterpolates(t *testing.T) {
	// Values 10..50; Percentile does not mutate its argument.
	cases := []struct {
		p    float64
		want float64
	}{
		{5, 12},  // rank 0.2 -> 10 + 0.2*(20-10)
		{50, 30}, // rank 2.0 -> xs[2]
		{95, 48}, // rank 3.8 -> 40 + 0.8*(50-40)
	}
	for _, c := range cases {
		if got := triangle.Percentile([]float64{50, 10, 30, 20, 40}, c.p); !approx(got, c.want) {
			t.Errorf("Percentile(p=%v) = %v, want %v", c.p, got, c.want)
		}
	}
	if got := triangle.Percentile([]float64{7}, 5); !approx(got, 7) {
		t.Errorf("Percentile single = %v, want 7", got)
	}
	if got := triangle.Percentile(nil, 5); !math.IsNaN(got) {
		t.Errorf("Percentile(nil) = %v, want NaN", got)
	}
}

func TestBandsAcrossReferenceSets(t *testing.T) {
	refs := []triangle.ReferenceSet{
		{Paid: triangle.Triangle{Cells: [][]float64{{100, 150, 165}}}},
		{Paid: triangle.Triangle{Cells: [][]float64{{100, 200, 210}}}},
	}
	paids := make([]triangle.Triangle, len(refs))
	for i, r := range refs {
		paids[i] = r.Paid
	}
	bands := triangle.ATABands(paids)
	if len(bands) != 2 {
		t.Fatalf("got %d bands, want 2", len(bands))
	}
	// Full range is the min/max; the scored band is P5-P95 (interpolated).
	if !approx(bands[0].Min, 1.5) || !approx(bands[0].Max, 2.0) {
		t.Errorf("age 0 min/max = %+v, want [1.5, 2.0]", bands[0])
	}
	if !approx(bands[0].Lo, 1.525) || !approx(bands[0].Hi, 1.975) {
		t.Errorf("age 0 scored = [%v, %v], want [1.525, 1.975]", bands[0].Lo, bands[0].Hi)
	}
	if !approx(bands[1].Min, 1.05) || !approx(bands[1].Max, 1.1) {
		t.Errorf("age 1 min/max = %+v, want [1.05, 1.1]", bands[1])
	}
}

func TestCompareFiltersDegenerateReferences(t *testing.T) {
	// Two healthy companies plus one with zero earned premium carrying an
	// extreme paid factor. The zero-premium company must not widen the band.
	refs := []triangle.ReferenceSet{
		{Name: "good1", Paid: triangle.Triangle{Cells: [][]float64{{100, 150}}},
			Incurred: triangle.Triangle{Cells: [][]float64{{140, 150}}}, EarnedPremium: []float64{200}},
		{Name: "good2", Paid: triangle.Triangle{Cells: [][]float64{{100, 160}}},
			Incurred: triangle.Triangle{Cells: [][]float64{{150, 160}}}, EarnedPremium: []float64{250}},
		{Name: "zeroEP", Paid: triangle.Triangle{Cells: [][]float64{{100, 500}}}, // ATA 5.0
			Incurred: triangle.Triangle{Cells: [][]float64{{100, 500}}}, EarnedPremium: []float64{0}},
	}
	c := triangle.Comparison{
		Paid:          triangle.Triangle{Cells: [][]float64{{100, 155}}},
		Incurred:      triangle.Triangle{Cells: [][]float64{{145, 155}}},
		EarnedPremium: []float64{220},
	}
	report := triangle.CompareToReference(c, refs)
	if len(report.PaidATA) == 0 {
		t.Fatal("no paid ATA checks")
	}
	if report.PaidATA[0].Band.Max > 2.0 {
		t.Errorf("paid band max = %v; zero-premium company was not filtered out", report.PaidATA[0].Band.Max)
	}
}

func TestCompareFiltersZeroIncurredReferences(t *testing.T) {
	// Two healthy companies plus one with positive earned premium but an
	// all-zero incurred latest diagonal, carrying an extreme incurred factor
	// via a nonzero paid triangle. The zero-incurred company must not widen
	// the band.
	refs := []triangle.ReferenceSet{
		{Name: "good1", Paid: triangle.Triangle{Cells: [][]float64{{100, 150}}},
			Incurred: triangle.Triangle{Cells: [][]float64{{140, 150}}}, EarnedPremium: []float64{200}},
		{Name: "good2", Paid: triangle.Triangle{Cells: [][]float64{{100, 160}}},
			Incurred: triangle.Triangle{Cells: [][]float64{{150, 160}}}, EarnedPremium: []float64{250}},
		{Name: "zeroIncurred", Paid: triangle.Triangle{Cells: [][]float64{{100, 500}}}, // extreme paid ATA 5.0
			Incurred: triangle.Triangle{Cells: [][]float64{{0, 0}}}, EarnedPremium: []float64{200}},
	}
	c := triangle.Comparison{
		Paid:          triangle.Triangle{Cells: [][]float64{{100, 155}}},
		Incurred:      triangle.Triangle{Cells: [][]float64{{145, 155}}},
		EarnedPremium: []float64{220},
	}
	report := triangle.CompareToReference(c, refs)
	if len(report.PaidATA) == 0 {
		t.Fatal("no paid ATA checks")
	}
	if report.PaidATA[0].Band.Max > 2.0 {
		t.Errorf("paid band max = %v; zero-incurred company was not filtered out", report.PaidATA[0].Band.Max)
	}
}

func TestCompareFailsWhenGeneratedHasNoPremium(t *testing.T) {
	refs := []triangle.ReferenceSet{
		{Name: "a", Incurred: triangle.Triangle{Cells: [][]float64{{140, 150}}}, EarnedPremium: []float64{200}},
		{Name: "b", Incurred: triangle.Triangle{Cells: [][]float64{{210, 200}}}, EarnedPremium: []float64{250}},
	}
	c := triangle.Comparison{
		Incurred:      triangle.Triangle{Cells: [][]float64{{180, 180}}},
		EarnedPremium: []float64{0}, // no premium -> loss ratio undefined
	}
	report := triangle.CompareToReference(c, refs)
	if report.LossRatio.Within {
		t.Error("loss ratio scored as within despite zero generated premium")
	}
}

func TestCompareToReferencePassesInsideBands(t *testing.T) {
	ref := []triangle.ReferenceSet{
		{
			Name:          "a",
			Paid:          triangle.Triangle{Cells: [][]float64{{100, 150}}},
			Incurred:      triangle.Triangle{Cells: [][]float64{{140, 150}}},
			EarnedPremium: []float64{200}, // LR 0.75
		},
		{
			Name:          "b",
			Paid:          triangle.Triangle{Cells: [][]float64{{100, 200}}},
			Incurred:      triangle.Triangle{Cells: [][]float64{{210, 200}}},
			EarnedPremium: []float64{250}, // LR 0.8
		},
	}
	inside := triangle.Comparison{
		Paid:          triangle.Triangle{Cells: [][]float64{{100, 180}}},
		Incurred:      triangle.Triangle{Cells: [][]float64{{180, 180}}},
		EarnedPremium: []float64{230}, // LR ~0.78
	}
	report := triangle.CompareToReference(inside, ref)
	if !report.Pass() {
		t.Errorf("expected pass, got %+v", report)
	}

	outside := triangle.Comparison{
		Paid:          triangle.Triangle{Cells: [][]float64{{100, 300}}}, // ATA 3.0 outside [1.5, 2.0]
		Incurred:      triangle.Triangle{Cells: [][]float64{{180, 300}}},
		EarnedPremium: []float64{100}, // LR 3.0 outside [0.75, 0.8]
	}
	report = triangle.CompareToReference(outside, ref)
	if report.Pass() {
		t.Errorf("expected failure, got %+v", report)
	}
	if report.String() == "" {
		t.Error("report should describe the comparison")
	}
}
