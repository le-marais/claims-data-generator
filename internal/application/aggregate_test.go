package application_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func TestAggregateGridCountsEveryClaim(t *testing.T) {
	req := request(t)
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(3), req)
	if err != nil {
		t.Fatal(err)
	}
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, row := range ag.Grid.Reported {
		for _, v := range row {
			total += v
		}
	}
	// Occurrences are constrained to the window and development runs to full
	// runoff, so no claim falls off an edge of the grid.
	if total != len(ds.Claims) {
		t.Errorf("reported count total = %d, want %d claims", total, len(ds.Claims))
	}
}

func TestAggregateGridReconcilesWithTheSummary(t *testing.T) {
	req := request(t)
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(3), req)
	if err != nil {
		t.Fatal(err)
	}
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	summary := application.Summarize(ds, req.StartYear, req.Years)

	sum := func(cells [][]float64) float64 {
		total := 0.0
		for _, row := range cells {
			for _, v := range row {
				total += v
			}
		}
		return total
	}
	if diff := sum(ag.Grid.Paid) - summary.Total.Paid; diff > 0.01 || diff < -0.01 {
		t.Errorf("grid paid total = %v, summary paid = %v", sum(ag.Grid.Paid), summary.Total.Paid)
	}
	wantNet := summary.Total.Paid - summary.Total.Recovered
	if diff := sum(ag.Grid.PaidNet) - wantNet; diff > 0.01 || diff < -0.01 {
		t.Errorf("grid net paid total = %v, want %v", sum(ag.Grid.PaidNet), wantNet)
	}
}

func TestAggregateExposureMatchesTheYearlyPremium(t *testing.T) {
	req := request(t)
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(3), req)
	if err != nil {
		t.Fatal(err)
	}
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	if len(ag.Exposure) != req.Years*12 {
		t.Fatalf("got %d exposure months, want %d", len(ag.Exposure), req.Years*12)
	}
	for y := 0; y < req.Years; y++ {
		sum := 0.0
		for i := y * 12; i < (y+1)*12; i++ {
			sum += ag.Exposure[i].Premium
		}
		if diff := sum - ag.EarnedPremium[y]; diff > 0.01 || diff < -0.01 {
			t.Errorf("year %d: monthly premium sum = %v, yearly = %v", req.StartYear+y, sum, ag.EarnedPremium[y])
		}
	}
}

func TestAggregateUnderwritingBasisLeavesTheAnnualViewAlone(t *testing.T) {
	req := request(t)
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(3), req)
	if err != nil {
		t.Fatal(err)
	}
	accident, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	underwriting, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.UnderwritingMonth)
	if err != nil {
		t.Fatal(err)
	}
	// The realism gate and the UI must not change grain when the CSV basis
	// knob moves: Schedule P is an accident-year presentation.
	for o := range accident.Annual.Incurred.Cells {
		for d := range accident.Annual.Incurred.Cells[o] {
			a := accident.Annual.Incurred.Cells[o][d]
			u := underwriting.Annual.Incurred.Cells[o][d]
			if diff := a - u; diff > 1e-9 || diff < -1e-9 {
				t.Fatalf("annual incurred cell (%d, %d) moved with the basis: %v vs %v", o, d, a, u)
			}
		}
	}
	// The grid itself must differ, or the basis did nothing.
	if underwriting.Grid.Basis != triangle.UnderwritingMonth {
		t.Errorf("grid basis = %q, want underwriting", underwriting.Grid.Basis)
	}
}

func TestAggregateRejectsBadArguments(t *testing.T) {
	if _, err := application.Aggregate(application.Dataset{}, 1998, 0, triangle.AccidentMonth); err == nil {
		t.Error("want an error for zero years")
	}
	if _, err := application.Aggregate(application.Dataset{}, 1998, 3, triangle.OriginBasis("policy")); err == nil {
		t.Error("want an error for an unknown basis")
	}
}
