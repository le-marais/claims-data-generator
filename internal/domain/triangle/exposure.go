package triangle

import (
	"time"

	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// daysPerYear converts policy-days into the policy-years an exposure unit is
// measured in.
const daysPerYear = 365.25

// MonthExposure is one origin month's exposure. On the accident basis the
// figures are earned in the month, day pro-rata; on the underwriting basis
// they are written in it, so a policy's whole premium, whole term and its
// count land in its inception month.
type MonthExposure struct {
	Month         shared.Month
	Premium       float64
	ExposureUnits float64 // policy-years
	// SectionPremiums splits Premium by section of cover, in the order of the
	// line of business's sections, on the same basis and with the same
	// pro-rating as Premium. It is empty when the policies carry no section
	// split. Each policy's sections are rounded to the cent separately from its
	// premium, so the split can differ from Premium by up to a cent per policy.
	SectionPremiums []float64
	// Policies is an in-force count on the accident basis - a policy counts
	// in every month it covers, so the column does not sum to a policy count -
	// and an inception count on the underwriting basis, where it does.
	Policies int
}

// ExposureByMonth returns the exposure of each of the origin months starting
// at startMonth. Exposure falling outside that span is not counted. The book
// writes a warm-up underwriting year before the run window, so on the accident
// basis the first months carry a full book in force, and the last months are
// full too, covered by the final underwriting year. On the underwriting basis
// a policy's whole premium and whole term are written in full at its inception
// month, while the claim occurrences scored against that exposure stop at the
// run window's end, so the final twelve origin months are immature by
// construction: incurred against premium there understates the eventual
// ratio. Warm-up policies incept before the window and are not counted on
// that basis.
func ExposureByMonth(policies []policy.Policy, startMonth shared.Month, months int, basis OriginBasis) []MonthExposure {
	out := make([]MonthExposure, months)
	sections := 0
	for _, p := range policies {
		sections = max(sections, len(p.SectionPremiums))
	}
	for i := range out {
		out[i].Month = startMonth.Add(i)
		out[i].SectionPremiums = make([]float64, sections)
	}
	for _, p := range policies {
		termDays := shared.DaysBetween(p.CoverStart, p.CoverEnd) + 1
		if termDays <= 0 {
			continue
		}
		if basis == UnderwritingMonth {
			i := shared.MonthsBetween(startMonth, p.CoverStart.Month())
			if i < 0 || i >= months {
				continue
			}
			out[i].Premium += p.Premium.Dollars()
			for s, sp := range p.SectionPremiums {
				out[i].SectionPremiums[s] += sp.Dollars()
			}
			out[i].ExposureUnits += float64(termDays) / daysPerYear
			out[i].Policies++
			continue
		}
		// Accident basis: spread the premium over the cover days and credit
		// each month with the days it holds. Only the months the cover can
		// touch are visited.
		perDay := p.Premium.Dollars() / float64(termDays)
		first := shared.MonthsBetween(startMonth, p.CoverStart.Month())
		if first < 0 {
			first = 0
		}
		last := shared.MonthsBetween(startMonth, p.CoverEnd.Month())
		if last > months-1 {
			last = months - 1
		}
		for i := first; i <= last; i++ {
			m := startMonth.Add(i)
			days := overlapDays(p.CoverStart, p.CoverEnd, m.Start(), m.End())
			if days <= 0 {
				continue
			}
			out[i].Premium += perDay * float64(days)
			for s, sp := range p.SectionPremiums {
				out[i].SectionPremiums[s] += sp.Dollars() / float64(termDays) * float64(days)
			}
			out[i].ExposureUnits += float64(days) / daysPerYear
			out[i].Policies++
		}
	}
	return out
}

// EarnedPremiumByYear spreads each policy's premium over its cover period and
// sums the portion earned in each calendar year of the window. It rolls up
// ExposureByMonth on the accident basis, so the yearly and monthly views agree
// by construction.
func EarnedPremiumByYear(policies []policy.Policy, startYear, years int) []float64 {
	monthly := ExposureByMonth(policies, shared.NewMonth(startYear, time.January), years*12, AccidentMonth)
	earned := make([]float64, years)
	for i, m := range monthly {
		earned[i/12] += m.Premium
	}
	return earned
}

// overlapDays counts the days of [start, end] falling inside
// [rangeStart, rangeEnd], all bounds inclusive.
func overlapDays(start, end, rangeStart, rangeEnd shared.Date) int {
	if start.Before(rangeStart) {
		start = rangeStart
	}
	if end.After(rangeEnd) {
		end = rangeEnd
	}
	days := shared.DaysBetween(start, end) + 1
	if days < 0 {
		return 0
	}
	return days
}
