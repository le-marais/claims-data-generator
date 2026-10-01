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

func TestLossRatioDriftClimbingIsAboveOne(t *testing.T) {
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
	// First half 0.70-0.86 (mean 0.78), second half 0.90-1.06 (mean 0.98).
	if want := 0.98 / 0.78; math.Abs(d-want) > 1e-9 {
		t.Fatalf("climbing drift = %v, want %v", d, want)
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

// MR-13: the drift band is the reference companies' own drift on developed
// incurred, not a fixed tolerance.
func TestDriftBandComesFromReferenceDrift(t *testing.T) {
	ref := func(name string, secondHalf float64) ReferenceSet {
		cells := make([][]float64, 10)
		for i := range cells {
			v := 50.0 // loss ratio 0.5
			if i >= 5 {
				v = secondHalf
			}
			cells[i] = []float64{v}
		}
		ep := make([]float64, 10)
		for i := range ep {
			ep[i] = 100
		}
		return ReferenceSet{
			Name: name, Paid: Triangle{Cells: [][]float64{{1, 2}}},
			Incurred: Triangle{Cells: [][]float64{{1, 1}}}, DevelopedIncurred: Triangle{Cells: cells},
			EarnedPremium: ep,
		}
	}
	refs := []ReferenceSet{ref("shrinking", 40), ref("flat", 50), ref("growing", 75)}
	gen, ep := flatTriangle(10, 60, 100)
	report := CompareToReference(Comparison{Incurred: gen, EarnedPremium: ep}, refs)
	b := report.LossRatioDrift.Band
	if math.Abs(b.Min-0.8) > 1e-9 || math.Abs(b.Max-1.5) > 1e-9 {
		t.Fatalf("drift band min/max [%v, %v], want the reference drifts [0.8, 1.5]", b.Min, b.Max)
	}
	if !report.LossRatioDrift.Within {
		t.Fatalf("flat generated drift %v outside the reference band %+v", report.LossRatioDrift.Value, b)
	}
}
