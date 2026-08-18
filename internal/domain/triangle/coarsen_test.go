package triangle_test

import (
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// marchToJanuary is the case that separates calendar-period development from
// dividing monthly development by twelve: an accident in March 1998 paid in
// January 1999 is development year 2, because the payment falls in the next
// calendar year - ten months of development, but two calendar years.
func marchToJanuary(t *testing.T) triangle.MonthlyGrid {
	t.Helper()
	claims := []claim.Claim{{
		ID: 1, PolicyID: 1,
		OccurrenceDate: shared.NewDate(1998, time.March, 1),
		ReportDate:     shared.NewDate(1998, time.March, 1),
	}}
	txs := []transaction.Transaction{
		{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.April, 1), Type: transaction.Payment, Amount: shared.FromDollars(100)},
		{ID: 2, ClaimID: 1, Date: shared.NewDate(1999, time.January, 15), Type: transaction.Payment, Amount: shared.FromDollars(500)},
	}
	g, err := triangle.BuildMonthlyGrid(nil, claims, txs, jan1998, 24, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestCoarsenAnnualKeysOnTheCalendarYearOfTheEvent(t *testing.T) {
	set := marchToJanuary(t).Coarsen(triangle.Annual, 3, true)
	if len(set.Paid) != 2 {
		t.Fatalf("got %d origin years, want 2", len(set.Paid))
	}
	// Origin year 1998: 100 in development year 1, 500 in development year 2.
	// Dividing ten months of development by twelve would wrongly put the 500
	// in development year 1.
	if !approx(set.Paid[0][0], 100) {
		t.Errorf("1998 dev 1 = %v, want 100", set.Paid[0][0])
	}
	if !approx(set.Paid[0][1], 500) {
		t.Errorf("1998 dev 2 = %v, want 500", set.Paid[0][1])
	}
	if !approx(set.Paid[0][2], 0) {
		t.Errorf("1998 dev 3 = %v, want 0", set.Paid[0][2])
	}
}

func TestCoarsenPadsEveryRowToDevPeriods(t *testing.T) {
	set := marchToJanuary(t).Coarsen(triangle.Annual, 5, true)
	for i, row := range set.Paid {
		if len(row) != 5 {
			t.Errorf("row %d is %d wide, want 5", i, len(row))
		}
	}
	for i, row := range set.Reported {
		if len(row) != 5 {
			t.Errorf("reported row %d is %d wide, want 5", i, len(row))
		}
	}
}

func TestCoarsenFoldsOrDropsTheTail(t *testing.T) {
	folded := marchToJanuary(t).Coarsen(triangle.Annual, 1, true)
	if !approx(folded.Paid[0][0], 600) {
		t.Errorf("folded 1998 dev 1 = %v, want 600", folded.Paid[0][0])
	}
	dropped := marchToJanuary(t).Coarsen(triangle.Annual, 1, false)
	if !approx(dropped.Paid[0][0], 100) {
		t.Errorf("truncated 1998 dev 1 = %v, want 100", dropped.Paid[0][0])
	}
}

func TestCoarsenQuarterlyUsesCalendarQuarters(t *testing.T) {
	// March 1998 is Q1 1998; January 1999 is Q1 1999, four quarters later, so
	// development quarter 5. April 1998 is Q2, development quarter 2.
	set := marchToJanuary(t).Coarsen(triangle.Quarterly, 0, false)
	if !approx(set.Paid[0][1], 100) {
		t.Errorf("Q1 1998 dev 2 = %v, want 100", set.Paid[0][1])
	}
	if !approx(set.Paid[0][4], 500) {
		t.Errorf("Q1 1998 dev 5 = %v, want 500", set.Paid[0][4])
	}
	if len(set.Paid) != 8 {
		t.Errorf("got %d origin quarters, want 8 for a 24-month grid", len(set.Paid))
	}
}

func TestCoarsenMonthlyIsTheGridItself(t *testing.T) {
	g := marchToJanuary(t)
	set := g.Coarsen(triangle.Monthly, 0, false)
	if len(set.Paid) != g.Origins() {
		t.Fatalf("got %d origins, want %d", len(set.Paid), g.Origins())
	}
	for o := range set.Paid {
		for d := range set.Paid[o] {
			if !approx(set.Paid[o][d], g.Paid[o][d]) {
				t.Fatalf("cell (%d, %d) = %v, want %v", o, d, set.Paid[o][d], g.Paid[o][d])
			}
		}
	}
}

func TestCumulativeRunsTheRunningSum(t *testing.T) {
	tri := marchToJanuary(t).Coarsen(triangle.Annual, 3, true).Cumulative(triangle.MeasurePaid)
	want := []float64{100, 600, 600}
	for d, w := range want {
		if !approx(tri.Cells[0][d], w) {
			t.Errorf("cumulative dev %d = %v, want %v", d+1, tri.Cells[0][d], w)
		}
	}
	if tri.StartYear != 1998 {
		t.Errorf("StartYear = %d, want 1998", tri.StartYear)
	}
}

func TestCumulativeReportedCounts(t *testing.T) {
	tri := marchToJanuary(t).Coarsen(triangle.Annual, 3, true).Cumulative(triangle.MeasureReported)
	want := []float64{1, 1, 1}
	for d, w := range want {
		if !approx(tri.Cells[0][d], w) {
			t.Errorf("cumulative reported dev %d = %v, want %v", d+1, tri.Cells[0][d], w)
		}
	}
}

func TestCoarsenEmptyGrid(t *testing.T) {
	g, err := triangle.BuildMonthlyGrid(nil, nil, nil, jan1998, 12, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	set := g.Coarsen(triangle.Annual, 10, true)
	if len(set.Paid) != 1 {
		t.Fatalf("got %d origin years, want 1", len(set.Paid))
	}
	if len(set.Paid[0]) != 10 {
		t.Errorf("row width = %d, want 10", len(set.Paid[0]))
	}
}
