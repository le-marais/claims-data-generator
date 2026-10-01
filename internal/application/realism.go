package application

import (
	"fmt"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// EvaluateRealism scores one section of a dataset against the bands observed
// across the reference companies (see SectionComparison). section is the
// index of the scored section, or -1 to score the whole book. Used as a test
// gate; it also backs the UI's realism view.
func EvaluateRealism(ds Dataset, startYear, years, section int, refs []triangle.ReferenceSet) (triangle.Report, error) {
	c, err := SectionComparison(ds, startYear, years, section)
	if err != nil {
		return triangle.Report{}, err
	}
	return triangle.CompareToReference(c, refs), nil
}

// SectionComparison builds what the realism gate scores: the accident-year
// triangles and earned premium of one section of cover alone, its claims
// against each policy's premium for that section, or of the whole book when
// section is -1. The motor preset scores its third-party section, because the
// Schedule P private passenger auto reference is a liability line with no
// physical damage in it, so own-damage claims are left out rather than bent
// to liability development speed. Paid is net of salvage and subrogation to
// match Schedule P, which reports paid losses net of recoveries.
//
// The triangles are the monthly grid coarsened to annual, like every other
// aggregate view. Schedule P is an accident-year presentation, so the
// comparison is always on the accident basis.
func SectionComparison(ds Dataset, startYear, years, section int) (triangle.Comparison, error) {
	if years < 1 {
		return triangle.Comparison{}, fmt.Errorf("years: must be at least 1, got %d", years)
	}
	policies, claims := ds.Policies, ds.Claims
	if section >= 0 {
		policies, claims = sectionOf(ds, section)
	}
	grid, err := triangle.BuildMonthlyGrid(policies, claims, ds.Transactions, shared.NewMonth(startYear, time.January), years*12, triangle.AccidentMonth)
	if err != nil {
		return triangle.Comparison{}, err
	}
	annual := grid.AnnualTriangles(developmentYears)
	return triangle.Comparison{
		Paid:          annual.NetPaid,
		Incurred:      annual.TotalIncurred,
		EarnedPremium: triangle.EarnedPremiumByYear(policies, startYear, years),
	}, nil
}

// sectionOf narrows a dataset to one section of cover: every policy carrying
// only that section's premium, and the section's claims. The grid builder
// skips transactions of claims it is not given, so the full ledger can be
// passed alongside.
func sectionOf(ds Dataset, section int) ([]policy.Policy, []claim.Claim) {
	policies := make([]policy.Policy, len(ds.Policies))
	for i, p := range ds.Policies {
		p.Premium = p.SectionPremiums[section]
		policies[i] = p
	}
	var claims []claim.Claim
	for _, c := range ds.Claims {
		if c.Section == section {
			claims = append(claims, c)
		}
	}
	return policies, claims
}
