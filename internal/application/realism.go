package application

import (
	"fmt"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// EvaluateRealism scores the scored sections of a dataset against the bands
// observed across the reference companies (see SectionComparison). sections
// are the indices of the scored sections; empty scores the whole book. Used
// as a test gate; it also backs the UI's realism view.
func EvaluateRealism(ds Dataset, startYear, years int, sections []int, refs []triangle.ReferenceSet) (triangle.Report, error) {
	c, err := SectionComparison(ds, startYear, years, sections)
	if err != nil {
		return triangle.Report{}, err
	}
	return triangle.CompareToReference(c, refs), nil
}

// SectionComparison builds what the realism gate scores: the accident-year
// triangles and earned premium of the given sections of cover together, their
// claims against each policy's premium for those sections, or of the whole
// book when sections is empty. The motor preset scores its third-party
// sections, because the Schedule P private passenger auto reference is a
// liability line with no physical damage in it, so own-damage claims are left
// out rather than bent to liability development speed. Paid is net of salvage
// and subrogation to match Schedule P, which reports paid losses net of
// recoveries. Incurred is paid plus case, the counterpart of the reference's
// case incurred (MR-16).
//
// The triangles are the monthly grid coarsened to annual, like every other
// aggregate view. Schedule P is an accident-year presentation, so the
// comparison is always on the accident basis.
func SectionComparison(ds Dataset, startYear, years int, sections []int) (triangle.Comparison, error) {
	if years < 1 {
		return triangle.Comparison{}, fmt.Errorf("years: must be at least 1, got %d", years)
	}
	policies, claims := ds.Policies, ds.Claims
	if len(sections) > 0 {
		policies, claims = sectionsOf(ds, sections)
	}
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

// sectionsOf narrows a dataset to some sections of cover: every policy
// carrying only those sections' premium, and their claims. The grid builder
// skips transactions of claims it is not given, so the full ledger can be
// passed alongside.
func sectionsOf(ds Dataset, sections []int) ([]policy.Policy, []claim.Claim) {
	in := make(map[int]bool, len(sections))
	for _, s := range sections {
		in[s] = true
	}
	policies := make([]policy.Policy, len(ds.Policies))
	for i, p := range ds.Policies {
		p.Premium = 0
		for _, s := range sections {
			p.Premium += p.SectionPremiums[s]
		}
		policies[i] = p
	}
	var claims []claim.Claim
	for _, c := range ds.Claims {
		if in[c.Section] {
			claims = append(claims, c)
		}
	}
	return policies, claims
}

// PersonalMotorCriteria selects the private passenger auto reference pool
// (MR-15). The two coefficient-of-variation limits are Meyers' for personal
// auto (CAS Monograph 1, 2015, table 11): books with steady premium and a
// steady reinsurance programme. The $5m-a-year floor (Schedule P is in
// thousands) keeps companies whose factors are mostly claim sampling noise
// from setting the band edges; the gate's generated book earns about
// $20-45m a year on its scored sections. Reinsurers write assumed business,
// not a personal auto book. Of the 121 complete companies, 45 are selected.
func PersonalMotorCriteria() triangle.ReferenceCriteria {
	return triangle.ReferenceCriteria{
		MaxPremiumCV:     0.45,
		MaxNetToDirectCV: 0.125,
		MinMeanPremium:   5000,
		Exclude: map[string]string{
			"10019": "reinsurer (Overseas Partners Us Reins Co)",
			"23876": "reinsurer (Mapfre Reins Corp)",
			"33499": "reinsurer (Dorinco Rein Co)",
			"35408": "reinsurer (Sirius Amer Ins Co)",
			"42439": "reinsurer (Toa-Re Ins Co Of Amer)",
		},
	}
}
