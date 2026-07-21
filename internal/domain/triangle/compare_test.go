package triangle

import (
	"math"
	"testing"
)

// flatTriangle builds a fully-developed incurred triangle whose every origin
// year has the same single cumulative value, and a matching earned premium.
func flatTriangle(years int, incurred, premium float64) (Triangle, []float64) {
	cells := make([][]float64, years)
	ep := make([]float64, years)
	for i := range cells {
		cells[i] = []float64{incurred}
		ep[i] = premium
	}
	return Triangle{StartYear: 1998, Cells: cells}, ep
}

func TestLossRatioDriftFlatIsNearOne(t *testing.T) {
	tri, ep := flatTriangle(10, 700, 1000)
	d, ok := lossRatioDrift(tri, ep)
	if !ok {
		t.Fatal("expected a drift value")
	}
	if math.Abs(d-1) > 1e-9 {
		t.Fatalf("flat drift = %v, want 1", d)
	}
}

func TestReportPassRequiresDriftWithin(t *testing.T) {
	report := Report{
		PaidATA:        []AgeCheck{{Within: true}},
		IncurredATA:    []AgeCheck{{Within: true}},
		LossRatio:      Check{Within: true},
		LossRatioDrift: Check{Within: false},
	}
	if report.Pass() {
		t.Fatal("Pass() = true with LossRatioDrift.Within = false, want false")
	}

	report.LossRatioDrift.Within = true
	if !report.Pass() {
		t.Fatal("Pass() = false with all checks Within = true, want true")
	}
}

func TestLossRatioDriftClimbingExceedsTolerance(t *testing.T) {
	years := 10
	cells := make([][]float64, years)
	ep := make([]float64, years)
	for i := range cells {
		// Loss ratio climbs from 0.70 to 1.06 across the decade.
		cells[i] = []float64{700 + float64(i)*40}
		ep[i] = 1000
	}
	tri := Triangle{StartYear: 1998, Cells: cells}
	d, ok := lossRatioDrift(tri, ep)
	if !ok {
		t.Fatal("expected a drift value")
	}
	if d <= driftTolerance {
		t.Fatalf("climbing drift = %v, want > %v", d, driftTolerance)
	}
}
