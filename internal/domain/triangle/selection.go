package triangle

import (
	"fmt"
	"math"
)

// ReferenceCriteria picks the reference companies that make a fair benchmark:
// books that did not change their business or their reinsurance over the
// accident years, large enough that their factors are not mostly claim
// sampling noise. The two coefficients of variation are the tests Meyers used
// to select Schedule P triangles (CAS Monograph 1, 2015, appendix A). A zero
// bound switches its rule off.
type ReferenceCriteria struct {
	// MaxPremiumCV bounds the coefficient of variation of net earned premium
	// across the accident years. A book that grew or shrank sharply changed
	// its business.
	MaxPremiumCV float64
	// MaxNetToDirectCV bounds the coefficient of variation of each accident
	// year's net-to-direct earned premium ratio. A moving ratio is a changing
	// reinsurance programme, which moves net losses apart from the business
	// written.
	MaxNetToDirectCV float64
	// MinMeanPremium is the least mean net earned premium per accident year,
	// in the reference's units.
	MinMeanPremium float64
	// Exclude leaves companies out by judgement, keyed by Name, each with its
	// reason.
	Exclude map[string]string
}

// Reason says why c leaves r out, or returns "" when c selects r. Every
// accident year's net and direct premium must be positive, because the
// ratios and the loss ratio need them.
func (c ReferenceCriteria) Reason(r ReferenceSet) string {
	if why, ok := c.Exclude[r.Name]; ok {
		return why
	}
	if len(r.EarnedPremium) == 0 || len(r.DirectPremium) != len(r.EarnedPremium) {
		return "no net and direct premium for every accident year"
	}
	ratios := make([]float64, len(r.EarnedPremium))
	for i, net := range r.EarnedPremium {
		if net <= 0 || r.DirectPremium[i] <= 0 {
			return fmt.Sprintf("premium not positive in accident year %d", r.Paid.StartYear+i)
		}
		ratios[i] = net / r.DirectPremium[i]
	}
	if cv := coefficientOfVariation(r.EarnedPremium); c.MaxPremiumCV > 0 && cv >= c.MaxPremiumCV {
		return fmt.Sprintf("net premium varies too much (CV %.3f)", cv)
	}
	if cv := coefficientOfVariation(ratios); c.MaxNetToDirectCV > 0 && cv >= c.MaxNetToDirectCV {
		return fmt.Sprintf("net-to-direct ratio varies too much (CV %.3f)", cv)
	}
	if m := average(r.EarnedPremium); m < c.MinMeanPremium {
		return fmt.Sprintf("too small (mean net premium %.0f)", m)
	}
	return ""
}

// SelectReferences returns the companies c selects, in their input order.
func SelectReferences(refs []ReferenceSet, c ReferenceCriteria) []ReferenceSet {
	out := make([]ReferenceSet, 0, len(refs))
	for _, r := range refs {
		if c.Reason(r) == "" {
			out = append(out, r)
		}
	}
	return out
}

// coefficientOfVariation is the population standard deviation of xs over its
// mean, or +Inf when the mean is not positive.
func coefficientOfVariation(xs []float64) float64 {
	m := average(xs)
	if math.IsNaN(m) || m <= 0 {
		return math.Inf(1)
	}
	ss := 0.0
	for _, x := range xs {
		ss += (x - m) * (x - m)
	}
	return math.Sqrt(ss/float64(len(xs))) / m
}

func average(xs []float64) float64 {
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}
