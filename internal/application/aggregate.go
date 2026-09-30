package application

import (
	"fmt"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/policy"
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
//
// Liability and LiabilityEarnedPremium are the same accident-year views of the
// third-party liability section alone: its claims against its share of
// premium. The realism gate scores these, because the Schedule P private
// passenger auto reference is a liability line with no own damage in it.
type Aggregates struct {
	Basis                  triangle.OriginBasis
	StartYear              int
	Years                  int
	Grid                   triangle.MonthlyGrid
	Exposure               []triangle.MonthExposure
	Annual                 triangle.AnnualSet
	EarnedPremium          []float64
	Liability              triangle.AnnualSet
	LiabilityEarnedPremium []float64
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
	liabilityPolicies, liabilityClaims := liabilitySection(ds)
	liability, err := triangle.BuildMonthlyGrid(liabilityPolicies, liabilityClaims, ds.Transactions, start, months, triangle.AccidentMonth)
	if err != nil {
		return Aggregates{}, err
	}
	return Aggregates{
		Basis:                  basis,
		StartYear:              startYear,
		Years:                  years,
		Grid:                   grid,
		Exposure:               triangle.ExposureByMonth(ds.Policies, start, months, basis),
		Annual:                 accident.AnnualTriangles(developmentYears),
		EarnedPremium:          triangle.EarnedPremiumByYear(ds.Policies, startYear, years),
		Liability:              liability.AnnualTriangles(developmentYears),
		LiabilityEarnedPremium: triangle.EarnedPremiumByYear(liabilityPolicies, startYear, years),
	}, nil
}

// liabilitySection narrows a dataset to its third-party liability section:
// every policy carrying only its third-party premium, and the third-party
// claims. The grid builder skips transactions of claims it is not given, so
// the full ledger can be passed alongside.
func liabilitySection(ds Dataset) ([]policy.Policy, []claim.Claim) {
	policies := make([]policy.Policy, len(ds.Policies))
	for i, p := range ds.Policies {
		p.Premium = p.ThirdPartyPremium
		policies[i] = p
	}
	var claims []claim.Claim
	for _, c := range ds.Claims {
		if !c.OwnDamage {
			claims = append(claims, c)
		}
	}
	return policies, claims
}
