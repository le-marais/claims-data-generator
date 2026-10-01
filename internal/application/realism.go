package application

import (
	"fmt"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// EvaluateRealism scores the third-party liability section of a dataset
// against the bands observed across the reference companies (see
// LiabilityComparison). Used as a test gate; it also backs the UI's realism
// view.
func EvaluateRealism(ds Dataset, startYear, years int, refs []triangle.ReferenceSet) (triangle.Report, error) {
	c, err := LiabilityComparison(ds, startYear, years)
	if err != nil {
		return triangle.Report{}, err
	}
	return triangle.CompareToReference(c, refs), nil
}

// LiabilityComparison builds what the realism gate scores: the accident-year
// triangles and earned premium of the third-party liability section alone,
// its claims against each policy's third-party premium. The Schedule P
// private passenger auto reference is a liability line with no physical
// damage in it, so own-damage claims are left out rather than bent to
// liability development speed. Paid is net of salvage and subrogation to match
// Schedule P, which reports paid losses net of recoveries.
//
// The triangles are the monthly grid coarsened to annual, like every other
// aggregate view. Schedule P is an accident-year presentation, so the
// comparison is always on the accident basis.
func LiabilityComparison(ds Dataset, startYear, years int) (triangle.Comparison, error) {
	if years < 1 {
		return triangle.Comparison{}, fmt.Errorf("years: must be at least 1, got %d", years)
	}
	policies, claims := liabilitySection(ds)
	grid, err := triangle.BuildMonthlyGrid(policies, claims, ds.Transactions, shared.NewMonth(startYear, time.January), years*12, triangle.AccidentMonth)
	if err != nil {
		return triangle.Comparison{}, err
	}
	annual := grid.AnnualTriangles(developmentYears)
	return triangle.Comparison{
		Paid:          annual.NetPaid,
		Incurred:      annual.Incurred,
		EarnedPremium: triangle.EarnedPremiumByYear(policies, startYear, years),
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
