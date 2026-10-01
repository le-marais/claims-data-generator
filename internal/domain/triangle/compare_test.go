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

// The loss ratio band comes from each company's developed incurred when it is
// available, not its immature latest diagonal (MR-4).
func TestLossRatioBandUsesDevelopedIncurred(t *testing.T) {
	ref := func(name string, latest, developed float64) ReferenceSet {
		return ReferenceSet{
			Name:              name,
			Paid:              Triangle{Cells: [][]float64{{100, 150}}},
			Incurred:          Triangle{Cells: [][]float64{{140, latest}}},
			DevelopedIncurred: Triangle{Cells: [][]float64{{140, latest, developed}}},
			EarnedPremium:     []float64{200},
		}
	}
	refs := []ReferenceSet{ref("a", 160, 100), ref("b", 170, 120)}
	report := CompareToReference(Comparison{
		Incurred:      Triangle{Cells: [][]float64{{110, 110}}},
		EarnedPremium: []float64{200},
	}, refs)
	if b := report.LossRatio.Band; b.Min != 0.5 || b.Max != 0.6 {
		t.Fatalf("loss ratio band [%v, %v], want the developed [0.5, 0.6]", b.Min, b.Max)
	}

	// Without later development the latest diagonal is used.
	for i := range refs {
		refs[i].DevelopedIncurred = Triangle{}
	}
	report = CompareToReference(Comparison{Incurred: Triangle{Cells: [][]float64{{110, 110}}}, EarnedPremium: []float64{200}}, refs)
	if b := report.LossRatio.Band; b.Min != 0.8 || b.Max != 0.85 {
		t.Fatalf("loss ratio band [%v, %v], want the latest-diagonal [0.8, 0.85]", b.Min, b.Max)
	}
}
