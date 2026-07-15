package triangle

import (
	"fmt"
	"math"
	"strings"
)

// ReferenceSet is one reference company's observed triangles.
type ReferenceSet struct {
	Name          string
	Paid          Triangle
	Incurred      Triangle
	EarnedPremium []float64
}

// Comparison is a generated dataset's aggregates, ready to score.
type Comparison struct {
	Paid          Triangle
	Incurred      Triangle
	EarnedPremium []float64
}

// Band is the min-max range observed across reference companies.
type Band struct {
	Min, Max float64
}

func (b Band) contains(v float64) bool {
	return v >= b.Min && v <= b.Max
}

// ATABands returns, per development age, the band of volume-weighted
// age-to-age factors observed across the given triangles.
func ATABands(triangles []Triangle) []Band {
	var bands []Band
	for _, t := range triangles {
		for age, f := range t.ATAFactors() {
			if math.IsNaN(f) {
				continue
			}
			for age >= len(bands) {
				bands = append(bands, Band{Min: math.Inf(1), Max: math.Inf(-1)})
			}
			if f < bands[age].Min {
				bands[age].Min = f
			}
			if f > bands[age].Max {
				bands[age].Max = f
			}
		}
	}
	return bands
}

// AgeCheck scores one development age against a band.
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
	PaidATA     []AgeCheck
	IncurredATA []AgeCheck
	LossRatio   Check
}

// Pass reports whether every checked metric fell inside its band.
func (r Report) Pass() bool {
	for _, c := range append(r.PaidATA, r.IncurredATA...) {
		if !c.Within {
			return false
		}
	}
	return r.LossRatio.Within
}

func (r Report) String() string {
	var b strings.Builder
	writeChecks := func(name string, checks []AgeCheck) {
		for _, c := range checks {
			fmt.Fprintf(&b, "%s ATA age %d-%d: %.4f in [%.4f, %.4f] = %v\n",
				name, c.Age+1, c.Age+2, c.Value, c.Band.Min, c.Band.Max, c.Within)
		}
	}
	writeChecks("paid", r.PaidATA)
	writeChecks("incurred", r.IncurredATA)
	fmt.Fprintf(&b, "ultimate loss ratio: %.4f in [%.4f, %.4f] = %v\n",
		r.LossRatio.Value, r.LossRatio.Band.Min, r.LossRatio.Band.Max, r.LossRatio.Within)
	return b.String()
}

// CompareToReference scores the generated aggregates against the bands
// observed across the reference companies: volume-weighted age-to-age
// factors for paid and incurred, and the overall ultimate loss ratio.
// Only ages present in both generated and reference data are checked.
func CompareToReference(c Comparison, refs []ReferenceSet) Report {
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

	lrBand := Band{Min: math.Inf(1), Max: math.Inf(-1)}
	for _, r := range refs {
		lr, ok := lossRatio(r.Incurred, r.EarnedPremium)
		if !ok {
			continue
		}
		if lr < lrBand.Min {
			lrBand.Min = lr
		}
		if lr > lrBand.Max {
			lrBand.Max = lr
		}
	}
	value, _ := lossRatio(c.Incurred, c.EarnedPremium)
	report.LossRatio = Check{Value: value, Band: lrBand, Within: lrBand.contains(value)}
	return report
}

func checkAges(factors []float64, bands []Band) []AgeCheck {
	var checks []AgeCheck
	for age, f := range factors {
		if math.IsNaN(f) || age >= len(bands) {
			continue
		}
		checks = append(checks, AgeCheck{
			Age: age, Value: f, Band: bands[age], Within: bands[age].contains(f),
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
