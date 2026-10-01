package triangle

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// ReferenceSet is one reference company's observed triangles.
//
// Incurred is Schedule P total incurred: paid, case, bulk and IBNR reserves.
// The generated incurred it is compared with is paid plus case plus pure IBNR
// held at its true value (AnnualSet.TotalIncurred), so unreported claims count
// on both sides. The generated side still has no bulk reserve: a reference
// company's IBNR held early and released later pulls its factors below 1,
// which a perfect IBNR does not, so the incurred check stays a loose bound.
type ReferenceSet struct {
	Name          string
	Paid          Triangle
	Incurred      Triangle
	EarnedPremium []float64
	// DevelopedIncurred is Incurred completed with the company's later
	// reported development, so every origin year is valued at the same, full
	// age. The loss ratio is scored on it. The zero value means the later
	// development is not available, and Incurred is used instead.
	DevelopedIncurred Triangle
}

// developedIncurred is the incurred triangle the loss ratio is scored on.
func (r ReferenceSet) developedIncurred() Triangle {
	if len(r.DevelopedIncurred.Cells) > 0 {
		return r.DevelopedIncurred
	}
	return r.Incurred
}

// Comparison is a generated dataset's aggregates, ready to score.
type Comparison struct {
	Paid          Triangle
	Incurred      Triangle
	EarnedPremium []float64
}

// Band is the range of an age-to-age factor or loss ratio observed across
// reference companies. Lo and Hi are the scored percentile bounds (P5-P95);
// Min and Max are the full observed extremes, kept for display context.
type Band struct {
	Lo, Hi   float64
	Min, Max float64
}

func (b Band) contains(v float64) bool {
	return v >= b.Lo && v <= b.Hi
}

// bandLoPercentile and bandHiPercentile define the scored band. Widening them
// (towards 0 and 100) loosens the realism gate.
const (
	bandLoPercentile = 5.0
	bandHiPercentile = 95.0
)

// Percentile returns the linearly-interpolated p-th percentile (p in [0,100])
// of xs, where p=0 is the minimum and p=100 the maximum. It does not modify
// xs. Returns NaN for empty xs.
func Percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	sorted := append([]float64(nil), xs...)
	sort.Float64s(sorted)
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := p / 100 * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	return sorted[lo] + (rank-float64(lo))*(sorted[hi]-sorted[lo])
}

// bandFromValues builds a Band from the values observed for one metric: the
// scored P5-P95 range plus the full min/max. Values with fewer than one entry
// yield a NaN scored band that contains nothing.
func bandFromValues(xs []float64) Band {
	min, max := math.Inf(1), math.Inf(-1)
	for _, v := range xs {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return Band{
		Lo:  Percentile(xs, bandLoPercentile),
		Hi:  Percentile(xs, bandHiPercentile),
		Min: min,
		Max: max,
	}
}

// ATABands returns, per development age, the band of volume-weighted
// age-to-age factors observed across the given triangles.
func ATABands(triangles []Triangle) []Band {
	var perAge [][]float64
	for _, t := range triangles {
		for age, f := range t.ATAFactors() {
			if math.IsNaN(f) {
				continue
			}
			for age >= len(perAge) {
				perAge = append(perAge, nil)
			}
			perAge[age] = append(perAge[age], f)
		}
	}
	bands := make([]Band, len(perAge))
	for age, xs := range perAge {
		bands[age] = bandFromValues(xs)
	}
	return bands
}

// AgeCheck scores one development age against a band. Age is the 1-based
// development period the factor develops from, so age 1 is the factor from
// development period 1 to 2.
type AgeCheck struct {
	Age    int
	Value  float64
	Band   Band
	Within bool
}

// Check scores one scalar metric against a band.
type Check struct {
	Value  float64
	Band   Band
	Within bool
}

// Report is the outcome of comparing generated data to the reference set.
type Report struct {
	PaidATA        []AgeCheck
	IncurredATA    []AgeCheck
	LossRatio      Check
	LossRatioDrift Check
}

// Pass reports whether every checked metric fell inside its band.
func (r Report) Pass() bool {
	for _, c := range append(r.PaidATA, r.IncurredATA...) {
		if !c.Within {
			return false
		}
	}
	return r.LossRatio.Within && r.LossRatioDrift.Within
}

func (r Report) String() string {
	var b strings.Builder
	writeChecks := func(name string, checks []AgeCheck) {
		for _, c := range checks {
			fmt.Fprintf(&b, "%s ATA age %d-%d: %.4f in [%.4f, %.4f] = %v\n",
				name, c.Age, c.Age+1, c.Value, c.Band.Lo, c.Band.Hi, c.Within)
		}
	}
	writeChecks("paid", r.PaidATA)
	writeChecks("incurred", r.IncurredATA)
	fmt.Fprintf(&b, "ultimate loss ratio: %.4f in [%.4f, %.4f] = %v\n",
		r.LossRatio.Value, r.LossRatio.Band.Lo, r.LossRatio.Band.Hi, r.LossRatio.Within)
	fmt.Fprintf(&b, "loss ratio drift (2nd half / 1st half): %.4f in [%.4f, %.4f] = %v\n",
		r.LossRatioDrift.Value, r.LossRatioDrift.Band.Lo, r.LossRatioDrift.Band.Hi, r.LossRatioDrift.Within)
	return b.String()
}

// usableRefs drops reference companies that carry no scorable signal: no
// earned premium, or an all-zero incurred triangle. Percentile bands handle
// ordinary outliers; this is a backstop for degenerate data (for example
// future un-curated per-line-of-business references).
func usableRefs(refs []ReferenceSet) []ReferenceSet {
	out := make([]ReferenceSet, 0, len(refs))
	for _, r := range refs {
		totalEP := 0.0
		for _, ep := range r.EarnedPremium {
			totalEP += ep
		}
		if totalEP <= 0 {
			continue
		}
		latest := 0.0
		for _, v := range r.Incurred.latestDiagonal() {
			latest += v
		}
		if latest <= 0 {
			continue
		}
		out = append(out, r)
	}
	return out
}

// CompareToReference scores the generated aggregates against the P5-P95 bands
// observed across the usable reference companies: volume-weighted age-to-age
// factors for paid and incurred, the overall ultimate loss ratio, and the
// loss-ratio drift between the two halves of the accident years. Only ages
// present in both generated and reference data are checked.
//
// The drift band is the reference companies' own drift, like every other
// band (MR-13). Real books drift a lot - P5-P95 roughly 0.54 to 1.47 on the
// embedded companies - so this check is a realism bound, not a guard against
// systematic drift in the model; that guard is a test that switches the
// model's noise off (TestPresetHasNoSystematicLossRatioDrift).
//
// The generated triangles run to full development, so the loss ratio is
// scored against each company's developed incurred rather than its latest
// diagonal, whose recent accident years are still immature (MR-4).
func CompareToReference(c Comparison, refs []ReferenceSet) Report {
	refs = usableRefs(refs)
	paidRef := make([]Triangle, len(refs))
	incRef := make([]Triangle, len(refs))
	for i, r := range refs {
		paidRef[i] = r.Paid
		incRef[i] = r.Incurred
	}
	report := Report{
		PaidATA:     checkAges(c.Paid.ATAFactors(), ATABands(paidRef)),
		IncurredATA: checkAges(c.Incurred.ATAFactors(), ATABands(incRef)),
	}

	var lrs []float64
	for _, r := range refs {
		if lr, ok := lossRatio(r.developedIncurred(), r.EarnedPremium); ok {
			lrs = append(lrs, lr)
		}
	}
	lrBand := bandFromValues(lrs)
	value, ok := lossRatio(c.Incurred, c.EarnedPremium)
	report.LossRatio = Check{Value: value, Band: lrBand, Within: ok && lrBand.contains(value)}

	var drifts []float64
	for _, r := range refs {
		if d, ok := lossRatioDrift(r.developedIncurred(), r.EarnedPremium); ok {
			drifts = append(drifts, d)
		}
	}
	driftBand := bandFromValues(drifts)
	drift, driftOK := lossRatioDrift(c.Incurred, c.EarnedPremium)
	report.LossRatioDrift = Check{Value: drift, Band: driftBand, Within: !driftOK || driftBand.contains(drift)}
	return report
}

func checkAges(factors []float64, bands []Band) []AgeCheck {
	var checks []AgeCheck
	for age, f := range factors {
		if math.IsNaN(f) || age >= len(bands) {
			continue
		}
		checks = append(checks, AgeCheck{
			Age: age + 1, Value: f, Band: bands[age], Within: bands[age].contains(f),
		})
	}
	return checks
}

// lossRatio is total latest incurred over total earned premium.
func lossRatio(incurred Triangle, earnedPremium []float64) (float64, bool) {
	totalIncurred := 0.0
	for _, v := range incurred.latestDiagonal() {
		totalIncurred += v
	}
	totalEP := 0.0
	for _, ep := range earnedPremium {
		totalEP += ep
	}
	if totalEP <= 0 {
		return 0, false
	}
	return totalIncurred / totalEP, true
}

// lossRatioDrift measures loss-ratio drift across accident years: the ratio of
// the second-half aggregate loss ratio to the first-half one, on each origin
// year's latest value. A value near 1 means a flat loss-ratio trend. Scored on
// developed values for the reference, so neither side is immature. ok is false
// when there are too few years or no first-half signal.
func lossRatioDrift(incurred Triangle, earnedPremium []float64) (float64, bool) {
	latest := incurred.latestDiagonal()
	n := len(latest)
	if n < 2 || len(earnedPremium) < n {
		return 0, false
	}
	half := n / 2
	sum := func(lo, hi int) (inc, ep float64) {
		for i := lo; i < hi; i++ {
			inc += latest[i]
			ep += earnedPremium[i]
		}
		return
	}
	inc1, ep1 := sum(0, half)
	inc2, ep2 := sum(n-half, n)
	if ep1 <= 0 || ep2 <= 0 || inc1 <= 0 {
		return 0, false
	}
	return (inc2 / ep2) / (inc1 / ep1), true
}
