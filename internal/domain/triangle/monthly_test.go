package triangle_test

import (
	"strings"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// jan1998 is the start month every grid test keys from.
var jan1998 = shared.NewMonth(1998, time.January)

// gridFixture is one claim occurring in March 1998 under a policy incepting
// in January 1998: reported the same month, $600 paid in June 1998 and $500
// paid at close in February 1999.
func gridFixture() ([]policy.Policy, []claim.Claim, []transaction.Transaction) {
	policies := []policy.Policy{{
		ID:         1,
		CoverStart: shared.NewDate(1998, time.January, 15),
		CoverEnd:   shared.NewDate(1998, time.January, 15).AddDays(364),
		Premium:    shared.FromDollars(365),
	}}
	claims := []claim.Claim{{
		Record: claim.Record{
			ID:              1,
			PolicyID:        1,
			OccurrenceDate:  shared.NewDate(1998, time.March, 1),
			ReportDate:      shared.NewDate(1998, time.March, 3),
			CloseDate:       shared.NewDate(1999, time.February, 1),
			InitialEstimate: shared.FromDollars(1000),
		},
	}}
	txs := []transaction.Transaction{
		{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.March, 3), Type: transaction.Estimate, Amount: shared.FromDollars(1000)},
		{ID: 2, ClaimID: 1, Date: shared.NewDate(1998, time.June, 1), Type: transaction.Payment, Amount: shared.FromDollars(600)},
		{ID: 3, ClaimID: 1, Date: shared.NewDate(1998, time.June, 1), Type: transaction.Estimate, Amount: shared.FromDollars(-600)},
		{ID: 4, ClaimID: 1, Date: shared.NewDate(1999, time.February, 1), Type: transaction.Payment, Amount: shared.FromDollars(500)},
		{ID: 5, ClaimID: 1, Date: shared.NewDate(1999, time.February, 1), Type: transaction.Estimate, Amount: shared.FromDollars(-400)},
	}
	return policies, claims, txs
}

func buildGrid(t *testing.T, basis triangle.OriginBasis) triangle.MonthlyGrid {
	t.Helper()
	policies, claims, txs := gridFixture()
	g, err := triangle.BuildMonthlyGrid(policies, claims, txs, jan1998, 24, basis)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestMonthlyGridCellsAreIncrementalAndOneBased(t *testing.T) {
	g := buildGrid(t, triangle.AccidentMonth)
	// The claim occurs in March 1998: origin row 2. Development period 1 is
	// March itself, so June 1998 is period 4 and February 1999 is period 12.
	if got := g.Cell(triangle.MeasurePaid, 2, 4); !approx(got, 600) {
		t.Errorf("paid at origin 2 dev 4 = %v, want 600", got)
	}
	if got := g.Cell(triangle.MeasurePaid, 2, 12); !approx(got, 500) {
		t.Errorf("paid at origin 2 dev 12 = %v, want 500 (incremental, not 1100)", got)
	}
	if got := g.Cell(triangle.MeasurePaid, 2, 1); !approx(got, 0) {
		t.Errorf("paid at origin 2 dev 1 = %v, want 0", got)
	}
	// Nothing lands in any other origin row.
	for o := 0; o < 24; o++ {
		if o == 2 {
			continue
		}
		for d := 1; d <= g.DevPeriods; d++ {
			if got := g.Cell(triangle.MeasurePaid, o, d); got != 0 {
				t.Fatalf("paid at origin %d dev %d = %v, want 0", o, d, got)
			}
		}
	}
}

func TestMonthlyGridIncurredIsCaseMovementsPlusPayments(t *testing.T) {
	g := buildGrid(t, triangle.AccidentMonth)
	// Dev 1: the initial estimate of 1000 is raised in March.
	if got := g.Cell(triangle.MeasureIncurred, 2, 1); !approx(got, 1000) {
		t.Errorf("incurred at dev 1 = %v, want 1000", got)
	}
	// Dev 4: pay 600 and release 600 of case, so incurred does not move.
	if got := g.Cell(triangle.MeasureIncurred, 2, 4); !approx(got, 0) {
		t.Errorf("incurred at dev 4 = %v, want 0", got)
	}
	// Dev 12: pay 500 and release 400, so incurred strengthens by 100.
	if got := g.Cell(triangle.MeasureIncurred, 2, 12); !approx(got, 100) {
		t.Errorf("incurred at dev 12 = %v, want 100", got)
	}
}

func TestMonthlyGridCountsClaimsByReportMonth(t *testing.T) {
	// A claim occurring in December 1998 but reported in January 1999 counts
	// in origin December at development period 2.
	claims := []claim.Claim{{
		Record: claim.Record{
			ID:             1,
			PolicyID:       1,
			OccurrenceDate: shared.NewDate(1998, time.December, 20),
			ReportDate:     shared.NewDate(1999, time.January, 15),
		},
	}}
	g, err := triangle.BuildMonthlyGrid(nil, claims, nil, jan1998, 24, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	if got := g.Cell(triangle.MeasureReported, 11, 1); got != 0 {
		t.Errorf("reported at origin 11 dev 1 = %v, want 0", got)
	}
	if got := g.Cell(triangle.MeasureReported, 11, 2); !approx(got, 1) {
		t.Errorf("reported at origin 11 dev 2 = %v, want 1", got)
	}
}

func TestMonthlyGridNetsRecoveriesOffPaidAndIncurred(t *testing.T) {
	claims := []claim.Claim{{
		Record: claim.Record{
			ID:             1,
			PolicyID:       1,
			OccurrenceDate: shared.NewDate(1998, time.March, 1),
			ReportDate:     shared.NewDate(1998, time.March, 1),
		},
	}}
	txs := []transaction.Transaction{
		{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.April, 1), Type: transaction.Payment, Amount: shared.FromDollars(1000)},
		{ID: 2, ClaimID: 1, Date: shared.NewDate(1999, time.June, 1), Type: transaction.Salvage, Amount: shared.FromDollars(150)},
	}
	g, err := triangle.BuildMonthlyGrid(nil, claims, txs, jan1998, 24, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	// April 1998 is development period 2 for a March origin; June 1999 is 16.
	if got := g.Cell(triangle.MeasurePaid, 2, 2); !approx(got, 1000) {
		t.Errorf("gross paid at dev 2 = %v, want 1000", got)
	}
	if got := g.Cell(triangle.MeasurePaid, 2, 16); !approx(got, 0) {
		t.Errorf("gross paid at dev 16 = %v, want 0 (recoveries are not payments)", got)
	}
	if got := g.Cell(triangle.MeasurePaidNet, 2, 16); !approx(got, -150) {
		t.Errorf("net paid at dev 16 = %v, want -150", got)
	}
	if got := g.Cell(triangle.MeasureIncurred, 2, 16); !approx(got, -150) {
		t.Errorf("incurred at dev 16 = %v, want -150", got)
	}
}

func TestMonthlyGridIsRectangularAndSizedToTheLastDevelopment(t *testing.T) {
	g := buildGrid(t, triangle.AccidentMonth)
	// The last movement is February 1999, development period 12 of a March
	// 1998 origin, so the grid is 12 wide.
	if g.DevPeriods != 12 {
		t.Errorf("DevPeriods = %d, want 12", g.DevPeriods)
	}
	if g.Origins() != 24 {
		t.Errorf("Origins() = %d, want 24", g.Origins())
	}
	for o, row := range g.Paid {
		if len(row) != g.DevPeriods {
			t.Fatalf("paid row %d is %d wide, want %d", o, len(row), g.DevPeriods)
		}
	}
	for o, row := range g.Reported {
		if len(row) != g.DevPeriods {
			t.Fatalf("reported row %d is %d wide, want %d", o, len(row), g.DevPeriods)
		}
	}
}

func TestMonthlyGridUnderwritingBasisKeysOnInception(t *testing.T) {
	g := buildGrid(t, triangle.UnderwritingMonth)
	// The policy incepts in January 1998, so the origin row is 0 and the
	// development periods count from January: June 1998 is period 6.
	if got := g.Cell(triangle.MeasurePaid, 0, 6); !approx(got, 600) {
		t.Errorf("paid at origin 0 dev 6 = %v, want 600", got)
	}
	if got := g.Cell(triangle.MeasurePaid, 2, 4); !approx(got, 0) {
		t.Errorf("paid at accident origin 2 dev 4 = %v, want 0 on the underwriting basis", got)
	}
	if got := g.Cell(triangle.MeasureReported, 0, 3); !approx(got, 1) {
		t.Errorf("reported at origin 0 dev 3 = %v, want 1 (reported in March)", got)
	}
}

func TestMonthlyGridUnderwritingBasisRejectsAMissingPolicy(t *testing.T) {
	_, claims, txs := gridFixture()
	_, err := triangle.BuildMonthlyGrid(nil, claims, txs, jan1998, 24, triangle.UnderwritingMonth)
	if err == nil {
		t.Fatal("want an error when a claim's policy is not in the book")
	}
	if !strings.Contains(err.Error(), "policy 1") {
		t.Errorf("error = %v, want it to name the missing policy", err)
	}
}

func TestMonthlyGridSkipsOriginsOutsideTheSpan(t *testing.T) {
	claims := []claim.Claim{
		{Record: claim.Record{ID: 1, PolicyID: 1, OccurrenceDate: shared.NewDate(1997, time.June, 1), ReportDate: shared.NewDate(1997, time.June, 1)}},
		{Record: claim.Record{ID: 2, PolicyID: 1, OccurrenceDate: shared.NewDate(2001, time.June, 1), ReportDate: shared.NewDate(2001, time.June, 1)}},
	}
	txs := []transaction.Transaction{
		{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.June, 1), Type: transaction.Payment, Amount: shared.FromDollars(900)},
		{ID: 2, ClaimID: 2, Date: shared.NewDate(2001, time.July, 1), Type: transaction.Payment, Amount: shared.FromDollars(900)},
	}
	// A 24-month span from January 1998 excludes both claims, so nothing is
	// placed and neither claim stretches the grid.
	g, err := triangle.BuildMonthlyGrid(nil, claims, txs, jan1998, 24, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	if g.DevPeriods != 1 {
		t.Errorf("DevPeriods = %d, want 1 when no claim is in the span", g.DevPeriods)
	}
	total := 0.0
	for _, row := range g.Paid {
		for _, v := range row {
			total += v
		}
	}
	if !approx(total, 0) {
		t.Errorf("total paid = %v, want 0", total)
	}
}

func TestMonthlyGridClampsDevelopmentBelowOne(t *testing.T) {
	// Hand-built input only: a payment before the claim's origin month cannot
	// arise in generated data, but must not index out of range.
	claims := []claim.Claim{{
		Record: claim.Record{
			ID:             1,
			PolicyID:       1,
			OccurrenceDate: shared.NewDate(1998, time.June, 1),
			ReportDate:     shared.NewDate(1998, time.June, 1),
		},
	}}
	txs := []transaction.Transaction{
		{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.February, 1), Type: transaction.Payment, Amount: shared.FromDollars(100)},
	}
	g, err := triangle.BuildMonthlyGrid(nil, claims, txs, jan1998, 24, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	if got := g.Cell(triangle.MeasurePaid, 5, 1); !approx(got, 100) {
		t.Errorf("paid at origin 5 dev 1 = %v, want 100 (clamped)", got)
	}
}

func TestMonthlyGridRejectsBadArguments(t *testing.T) {
	if _, err := triangle.BuildMonthlyGrid(nil, nil, nil, jan1998, 0, triangle.AccidentMonth); err == nil {
		t.Error("want an error for zero origin months")
	}
	if _, err := triangle.BuildMonthlyGrid(nil, nil, nil, jan1998, 12, triangle.OriginBasis("policy")); err == nil {
		t.Error("want an error for an unknown origin basis")
	}
}

func TestMonthlyGridEmptyDatasetIsOneColumnWide(t *testing.T) {
	g, err := triangle.BuildMonthlyGrid(nil, nil, nil, jan1998, 12, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	if g.DevPeriods != 1 || g.Origins() != 12 {
		t.Fatalf("grid is %dx%d, want 12x1", g.Origins(), g.DevPeriods)
	}
	if got := g.Cell(triangle.MeasurePaid, 0, 1); got != 0 {
		t.Errorf("cell = %v, want 0", got)
	}
}

func TestMonthlyGridCellOutOfRangeIsZero(t *testing.T) {
	g := buildGrid(t, triangle.AccidentMonth)
	for _, c := range []struct{ origin, dev int }{{-1, 1}, {99, 1}, {2, 0}, {2, 999}} {
		if got := g.Cell(triangle.MeasurePaid, c.origin, c.dev); got != 0 {
			t.Errorf("Cell(origin=%d, dev=%d) = %v, want 0", c.origin, c.dev, got)
		}
	}
}

// MR-9: pure IBNR books a claim's true cost in its occurrence month and
// releases it in its report month, so the annual total incurred carries the
// cost of claims occurred but not yet reported.
func TestIBNRCarriesUnreportedClaimsAtTrueCost(t *testing.T) {
	policies := []policy.Policy{{
		ID: 1, CoverStart: shared.NewDate(1998, time.January, 1), CoverEnd: shared.NewDate(1998, time.December, 31),
	}}
	claims := []claim.Claim{
		// Occurs December 1998, reported February 1999: IBNR at the 1998 year end.
		{
			Record: claim.Record{
				ID:             1,
				PolicyID:       1,
				OccurrenceDate: shared.NewDate(1998, time.December, 20),
				ReportDate:     shared.NewDate(1999, time.February, 10),
				CloseDate:      shared.NewDate(1999, time.June, 1),
			},
			Development: claim.Development{
				Ultimate: shared.FromDollars(4000),
			},
		},
		// Reported the month it occurs: never IBNR at a month end.
		{
			Record: claim.Record{
				ID:             2,
				PolicyID:       1,
				OccurrenceDate: shared.NewDate(1998, time.March, 1),
				ReportDate:     shared.NewDate(1998, time.March, 5),
				CloseDate:      shared.NewDate(1998, time.May, 1),
			},
			Development: claim.Development{
				Ultimate: shared.FromDollars(900),
			},
		},
		// A nil claim that reopens costs only its reopen episode.
		{
			Record: claim.Record{
				ID:             3,
				PolicyID:       1,
				OccurrenceDate: shared.NewDate(1998, time.April, 1),
				ReportDate:     shared.NewDate(1998, time.June, 1),
				CloseDate:      shared.NewDate(1999, time.March, 1),
			},
			Development: claim.Development{
				Ultimate:       shared.FromDollars(700),
				Nil:            true,
				ReopenDate:     shared.NewDate(1998, time.October, 1),
				ReopenUltimate: shared.FromDollars(250),
			},
		},
	}
	g, err := triangle.BuildMonthlyGrid(policies, claims, nil, jan1998, 12, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		origin, dev int
		want        float64
	}{
		{11, 1, 4000}, {11, 3, -4000}, // December origin: booked in dev 1, released in dev 3 (February)
		{2, 1, 0},                 // reported in its occurrence month
		{3, 1, 250}, {3, 3, -250}, // April origin, reported June: only the reopen cost
	}
	for _, c := range cases {
		if got := g.Cell(triangle.MeasureIBNR, c.origin, c.dev); !approx(got, c.want) {
			t.Errorf("IBNR origin %d dev %d = %v, want %v", c.origin, c.dev, got, c.want)
		}
	}
	annual := g.AnnualTriangles(2)
	if got := annual.TotalIncurred.Cells[0][0] - annual.Incurred.Cells[0][0]; !approx(got, 4000) {
		t.Errorf("1998 total incurred less incurred at age 1 = %v, want the unreported 4000", got)
	}
	if got := annual.TotalIncurred.Cells[0][1] - annual.Incurred.Cells[0][1]; !approx(got, 0) {
		t.Errorf("1998 total incurred less incurred at age 2 = %v, want 0 once reported", got)
	}
}
