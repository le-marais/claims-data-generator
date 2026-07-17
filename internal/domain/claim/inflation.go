package claim

import (
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// InflationIndex maps a claim's occurrence year to a cumulative
// claims-inflation factor. The index is 1.0 in the start year and compounds
// a simulated annual factor for each subsequent year of the run window. The
// zero value is the identity index: For returns 1.0 for every year.
type InflationIndex struct {
	startYear int
	// factors[i] is the cumulative index for startYear+i; factors[0] is 1.0.
	factors []float64
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
	return InflationIndex{startYear: startYear, factors: factors}
}

// For returns the cumulative inflation factor for an occurrence year. Years
// before the window clamp to the start-year index (1.0); years after clamp
// to the last simulated index. The zero-value index returns 1.0 everywhere.
func (x InflationIndex) For(year int) float64 {
	if len(x.factors) == 0 {
		return 1.0
	}
	i := year - x.startYear
	if i < 0 {
		i = 0
	}
	if i >= len(x.factors) {
		i = len(x.factors) - 1
	}
	return x.factors[i]
}
