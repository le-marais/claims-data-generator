package triangle_test

import (
	"reflect"
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// steadyRef is a two-year company with steady premium that keeps 80% of its
// direct premium every year.
func steadyRef(name string) triangle.ReferenceSet {
	return triangle.ReferenceSet{
		Name:          name,
		Paid:          triangle.Triangle{StartYear: 1998, Cells: [][]float64{{50, 80}, {55}}},
		EarnedPremium: []float64{100, 110},
		DirectPremium: []float64{125, 137.5},
	}
}

func TestReferenceCriteriaReason(t *testing.T) {
	c := triangle.ReferenceCriteria{
		MaxPremiumCV:     0.45,
		MaxNetToDirectCV: 0.125,
		MinMeanPremium:   50,
		Exclude:          map[string]string{"9": "reinsurer"},
	}
	for _, tc := range []struct {
		name string
		edit func(r *triangle.ReferenceSet)
		want string
	}{
		{"steady book", func(*triangle.ReferenceSet) {}, ""},
		{"excluded by name", func(r *triangle.ReferenceSet) { r.Name = "9" }, "reinsurer"},
		{"no direct premium", func(r *triangle.ReferenceSet) { r.DirectPremium = nil }, "no net and direct premium for every accident year"},
		{"zero net premium", func(r *triangle.ReferenceSet) { r.EarnedPremium[1] = 0 }, "premium not positive in accident year 1999"},
		// Net premium 100 then 300: mean 200, standard deviation 100.
		{"premium tripled", func(r *triangle.ReferenceSet) {
			r.EarnedPremium[1], r.DirectPremium[1] = 300, 375
		}, "net premium varies too much (CV 0.500)"},
		// Kept shares 0.8 then 0.5: mean 0.65, standard deviation 0.15.
		{"quota share started", func(r *triangle.ReferenceSet) { r.DirectPremium[1] = 220 }, "net-to-direct ratio varies too much (CV 0.231)"},
		{"small book", func(r *triangle.ReferenceSet) {
			r.EarnedPremium, r.DirectPremium = []float64{40, 44}, []float64{50, 55}
		}, "too small (mean net premium 42)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := steadyRef("1")
			tc.edit(&r)
			if got := c.Reason(r); got != tc.want {
				t.Fatalf("Reason = %q, want %q", got, tc.want)
			}
		})
	}
}

// A zero bound switches its rule off.
func TestZeroCriteriaSelectEveryPositivePremiumBook(t *testing.T) {
	var c triangle.ReferenceCriteria
	r := steadyRef("1")
	r.EarnedPremium, r.DirectPremium = []float64{10, 300}, []float64{100, 310}
	if got := c.Reason(r); got != "" {
		t.Fatalf("zero criteria Reason = %q, want selected", got)
	}
}

func TestSelectReferencesKeepsInputOrder(t *testing.T) {
	c := triangle.ReferenceCriteria{Exclude: map[string]string{"b": "reinsurer"}}
	refs := []triangle.ReferenceSet{steadyRef("c"), steadyRef("b"), steadyRef("a")}
	var got []string
	for _, r := range triangle.SelectReferences(refs, c) {
		got = append(got, r.Name)
	}
	if want := []string{"c", "a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected %v, want %v", got, want)
	}
}
