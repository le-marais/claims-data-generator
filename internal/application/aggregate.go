package application

import (
	"fmt"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// developmentYears is the depth of the annual triangles the realism gate and
// the UI read: Schedule P triangles have ten development years.
const developmentYears = 10

// Aggregates is everything derived from a dataset by pure aggregation: the
// monthly grid and exposure on the requested origin basis, plus the annual
// triangles and earned premium the realism gate and the UI read.
//
// Annual and EarnedPremium are always on the accident basis whatever Basis is.
// Schedule P is an accident-year presentation, so the realism comparison and
// the UI's triangle tab must not change grain when the origin-basis knob
// moves; the knob governs the monthly output.
type Aggregates struct {
	Basis         triangle.OriginBasis
	StartYear     int
	Years         int
	Grid          triangle.MonthlyGrid
	Exposure      []triangle.MonthExposure
	Annual        triangle.AnnualSet
	EarnedPremium []float64
}

// Aggregate aggregates a generated dataset. It draws no randomness and mutates
// nothing, so it cannot affect the reproducibility of a run.
func Aggregate(ds Dataset, startYear, years int, basis triangle.OriginBasis) (Aggregates, error) {
	if err := basis.Validate(); err != nil {
		return Aggregates{}, err
	}
	if years < 1 {
		return Aggregates{}, fmt.Errorf("years: must be at least 1, got %d", years)
	}
	start := shared.NewMonth(startYear, time.January)
	months := years * 12
	grid, err := triangle.BuildMonthlyGrid(ds.Policies, ds.Claims, ds.Transactions, start, months, basis)
	if err != nil {
		return Aggregates{}, err
	}
	accident := grid
	if basis != triangle.AccidentMonth {
		accident, err = triangle.BuildMonthlyGrid(ds.Policies, ds.Claims, ds.Transactions, start, months, triangle.AccidentMonth)
		if err != nil {
			return Aggregates{}, err
		}
	}
	return Aggregates{
		Basis:         basis,
		StartYear:     startYear,
		Years:         years,
		Grid:          grid,
		Exposure:      triangle.ExposureByMonth(ds.Policies, start, months, basis),
		Annual:        accident.AnnualTriangles(developmentYears),
		EarnedPremium: triangle.EarnedPremiumByYear(ds.Policies, startYear, years),
	}, nil
}
