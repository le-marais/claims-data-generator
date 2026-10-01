package claim

import (
	"math"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// InflationIndex maps a claim's occurrence date to a cumulative
// claims-inflation factor. The run simulates one annual factor per year of
// the window and anchors the compounded index at the middle of each calendar
// year, 1.0 in the start year; between anchors the index moves geometrically,
// so it rises smoothly through the year rather than stepping each 1 January
// (MR-10). The zero value is the identity index: For returns 1.0 everywhere.
type InflationIndex struct {
	startYear int
	// factors[i] is the index at the middle of startYear+i; factors[0] is 1.0.
	factors []float64
	// mean is the annual trend the index follows outside its anchors: the
	// first half of the start year and the second half of the last year.
	mean float64
}

// NewInflationIndex simulates the inflation path over the run window. Each
// year past the first multiplies the running index by Mean times mean-1
// lognormal noise of sigma Volatility.
func NewInflationIndex(src shared.RandomSource, p lob.InflationParams, startYear, years int) InflationIndex {
	if years < 1 {
		return InflationIndex{}
	}
	factors := make([]float64, years)
	factors[0] = 1.0
	for i := 1; i < years; i++ {
		annual := p.Mean * shared.MeanOneLogNormal(src, p.Volatility)
		factors[i] = factors[i-1] * annual
	}
	return InflationIndex{startYear: startYear, factors: factors, mean: p.Mean}
}

// For returns the cumulative inflation factor at an occurrence date: the
// geometric interpolation between the anchors either side of it, or the first
// or last anchor trended at the mean rate when the date lies outside them.
// Each calendar year's average stays close to its anchor. The zero-value
// index returns 1.0 everywhere.
func (x InflationIndex) For(d shared.Date) float64 {
	if len(x.factors) == 0 {
		return 1.0
	}
	u := shared.TrendYears(d, x.startYear)
	last := len(x.factors) - 1
	if u <= 0 {
		return x.factors[0] * math.Pow(x.mean, u)
	}
	if u >= float64(last) {
		return x.factors[last] * math.Pow(x.mean, u-float64(last))
	}
	i := int(u)
	return x.factors[i] * math.Pow(x.factors[i+1]/x.factors[i], u-float64(i))
}
