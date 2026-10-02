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

// driftRef is a reference company whose developed loss ratio is 0.5 over the
// first five accident years and secondHalf/100 over the last five.
func driftRef(name string, secondHalf float64) ReferenceSet {
	cells := make([][]float64, 10)
	for i := range cells {
		v := 50.0
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

// MR-16: the incurred factors are scored against case incurred, Schedule P
// incurred less its bulk and IBNR reserves, the counterpart of the generated
// paid plus case, not against total incurred.
func TestIncurredBandUsesCaseIncurred(t *testing.T) {
	ref := func(name string, total, cased []float64) ReferenceSet {
		return ReferenceSet{
			Name:          name,
			Paid:          Triangle{Cells: [][]float64{{50, 90}}},
			Incurred:      Triangle{Cells: [][]float64{total}},
			CaseIncurred:  Triangle{Cells: [][]float64{cased}},
			EarnedPremium: []float64{200},
		}
	}
	// Total incurred falls as early IBNR is released; case incurred rises as
	// late claims are reported.
	refs := []ReferenceSet{
		ref("a", []float64{120, 100}, []float64{80, 100}),
		ref("b", []float64{130, 110}, []float64{90, 108}),
	}
	report := CompareToReference(Comparison{Incurred: Triangle{Cells: [][]float64{{80, 98}}}, EarnedPremium: []float64{200}}, refs)
	if len(report.IncurredATA) != 1 {
		t.Fatalf("got %d incurred checks, want 1", len(report.IncurredATA))
	}
	c := report.IncurredATA[0]
	if math.Abs(c.Band.Min-1.2) > 1e-9 || math.Abs(c.Band.Max-1.25) > 1e-9 {
		t.Fatalf("incurred band min/max [%v, %v], want the case factors [1.2, 1.25]", c.Band.Min, c.Band.Max)
	}
	if !c.Within {
		t.Fatalf("generated factor %v outside %+v", c.Value, c.Band)
	}
}

// MR-17: a paid pattern inside every age-to-age band can still be extreme as
// a whole. The paid share check scores the compounded pattern and catches it.
func TestPaidSharesCatchAPatternFastAtEveryAge(t *testing.T) {
	ref := func(name string, paid ...float64) ReferenceSet {
		tri := Triangle{StartYear: 1998, Cells: [][]float64{paid}}
		return ReferenceSet{Name: name, Paid: tri, DevelopedPaid: tri, Incurred: tri, CaseIncurred: tri, EarnedPremium: []float64{100}}
	}
	// Factors (2, 3), (2.5, 2.5) and (3, 2): fast or slow early, each company
	// reaches about six times its first-year paid.
	refs := []ReferenceSet{ref("a", 10, 20, 60), ref("b", 10, 25, 62.5), ref("c", 10, 30, 60)}
	// Factors (2.2, 2.2) sit inside both factor bands, [2.05, 2.95], but pay
	// a fifth of the total in the first year, against about a sixth.
	gen := Triangle{StartYear: 1998, Cells: [][]float64{{10, 22, 48.4}}}
	report := CompareToReference(Comparison{Paid: gen, Incurred: gen, EarnedPremium: []float64{80}}, refs)
	for _, c := range append(report.PaidATA, report.IncurredATA...) {
		if !c.Within {
			t.Fatalf("factor at age %d = %v outside %+v, want inside", c.Age, c.Value, c.Band)
		}
	}
	if !report.LossRatio.Within {
		t.Fatalf("loss ratio %v outside %+v, want inside", report.LossRatio.Value, report.LossRatio.Band)
	}
	if len(report.PaidShares) != 2 {
		t.Fatalf("got %d paid share checks, want 2", len(report.PaidShares))
	}
	if s := report.PaidShares[0]; s.Age != 1 || s.Within {
		t.Fatalf("paid share check %+v, want age 1 outside its band", s)
	}
	if report.Pass() {
		t.Fatal("Pass() = true with a paid share outside its band")
	}
}

// MR-13: the drift band is the reference companies' own drift on developed
// incurred, not a fixed tolerance.
func TestDriftBandComesFromReferenceDrift(t *testing.T) {
	refs := []ReferenceSet{driftRef("shrinking", 40), driftRef("flat", 50), driftRef("growing", 75)}
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

// MR-15: the drift band is the reference drifts over their median, so a level
// the reference companies share, such as a market cycle, does not set it.
func TestDriftBandIsRelativeToThePoolMedian(t *testing.T) {
	// Raw drifts 0.8, 0.9 and 1.2, around a median of 0.9.
	refs := []ReferenceSet{driftRef("improving", 40), driftRef("typical", 45), driftRef("worsening", 60)}
	flat, ep := flatTriangle(10, 60, 100)
	report := CompareToReference(Comparison{Incurred: flat, EarnedPremium: ep}, refs)
	b := report.LossRatioDrift.Band
	if math.Abs(b.Min-0.8/0.9) > 1e-9 || math.Abs(b.Max-1.2/0.9) > 1e-9 {
		t.Fatalf("drift band min/max [%v, %v], want the drifts over their median [%v, %v]", b.Min, b.Max, 0.8/0.9, 1.2/0.9)
	}
	if !report.LossRatioDrift.Within {
		t.Fatalf("flat generated drift outside the band %+v", b)
	}
	// A drift of 0.85 is inside the raw drifts' P5-P95, [0.81, 1.17], but
	// below the relative band, [0.90, 1.30].
	improving := make([][]float64, 10)
	for i := range improving {
		v := 60.0
		if i >= 5 {
			v = 51
		}
		improving[i] = []float64{v}
	}
	report = CompareToReference(Comparison{Incurred: Triangle{StartYear: 1998, Cells: improving}, EarnedPremium: ep}, refs)
	if report.LossRatioDrift.Within {
		t.Fatalf("generated drift %v inside %+v, want outside the relative band", report.LossRatioDrift.Value, report.LossRatioDrift.Band)
	}
}
