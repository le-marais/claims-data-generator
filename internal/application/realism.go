package application

import (
	"fmt"
	"slices"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// RealismProfile says how the realism gate scores a line of business: the
// Schedule P reference line it is compared with, and the sections of cover
// scored together against it, by name. No sections scores the whole book.
// It is evaluation, not simulation, so it sits beside a preset in the preset
// registry rather than in the line of business's parameters.
type RealismProfile struct {
	Line     string
	Sections []string
}

// SectionIndices resolves the profile's sections to their indices among l's
// sections of cover, in the profile's order, or nil for the whole book. A
// name l has no section for is an error.
func (p RealismProfile) SectionIndices(l lob.LineOfBusiness) ([]int, error) {
	var out []int
	for _, name := range p.Sections {
		i := slices.IndexFunc(l.Claims.Sections, func(s lob.SectionParams) bool { return s.Name == name })
		if i < 0 {
			return nil, fmt.Errorf("scored section %q: %s has no such section", name, l.Name)
		}
		out = append(out, i)
	}
	return out, nil
}

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
// book when sections is empty. The motor presets score their third-party
// sections, because the Schedule P auto references are liability lines with
// no physical damage in them, so own-damage claims are left out rather than
// bent to liability development speed. Paid is net of salvage
// and subrogation to match Schedule P, which reports paid losses net of
// recoveries. Incurred is paid plus case, the counterpart of the reference's
// case incurred (MR-16).
//
// The triangles are the monthly grid coarsened to annual, like every other
// aggregate view. Schedule P is an accident-year presentation, so the
// comparison is always on the accident basis. It values every company at age
// 10, so development after age 10 is dropped rather than folded into the last
// age as the UI's triangles do (MR-18): the last factors, the paid shares and
// the loss ratio then compare the same age on both sides, on a long-tail line
// as on a short one.
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
	annual := grid.Coarsen(triangle.Annual, developmentYears, false)
	return triangle.Comparison{
		Paid:          annual.Cumulative(triangle.MeasurePaidNet),
		Incurred:      annual.Cumulative(triangle.MeasureIncurred),
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

// Reference line IDs: the Schedule P lines the realism gate scores against.
const (
	PrivatePassengerAuto = "private_passenger_auto"
	CommercialAuto       = "commercial_auto"
)

// ReferenceLine is a Schedule P line the realism gate scores against: what
// to call it and the rules that select its reference companies.
type ReferenceLine struct {
	ID       string
	Label    string
	Criteria triangle.ReferenceCriteria
}

// ReferenceLines lists the lines the realism gate can score against.
func ReferenceLines() []ReferenceLine {
	return []ReferenceLine{
		{ID: PrivatePassengerAuto, Label: "private passenger auto liability", Criteria: PersonalMotorCriteria()},
		{ID: CommercialAuto, Label: "commercial auto liability", Criteria: CommercialAutoCriteria()},
	}
}

// ReferencePool is a reference line's selected companies, the bands the
// realism gate scores against.
type ReferencePool struct {
	Line ReferenceLine
	Refs []triangle.ReferenceSet
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

// CommercialAutoCriteria selects the commercial auto reference pool. The two
// coefficient-of-variation limits are Meyers' for commercial auto (CAS
// Monograph 1, 2015, table 11). Commercial auto companies are smaller than
// personal auto ones: of the 54 inside the limits, a $5m floor keeps 25, too
// few for steady P5-P95 bands, so the floor is $1m. No reinsurer passes the
// limits. Of the 137 complete companies, 42 are selected.
func CommercialAutoCriteria() triangle.ReferenceCriteria {
	return triangle.ReferenceCriteria{
		MaxPremiumCV:     0.399,
		MaxNetToDirectCV: 0.125,
		MinMeanPremium:   1000,
	}
}
