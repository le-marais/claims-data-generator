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
	Policies      int
}

// ExposureByMonth returns the exposure of each of the months origin months
// starting at startMonth. Exposure falling outside that span is not counted,
// so the last months of a run window are thin.
func ExposureByMonth(policies []policy.Policy, startMonth shared.Month, months int, basis OriginBasis) []MonthExposure {
	out := make([]MonthExposure, months)
	for i := range out {
		out[i].Month = startMonth.Add(i)
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
