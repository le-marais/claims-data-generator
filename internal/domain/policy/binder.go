package policy

import (
	"fmt"
	"math"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// Four coverholders renew on the first day of successive quarters.
const Coverholders = 4

type Binder struct {
	ID, CoverholderID, YearOfAccount int
	Inception, Expiry                shared.Date
	Estimate                         shared.Money
}

func annualBinder(year, holder int) Binder {
	inception := shared.NewDate(year, time.Month(1+3*(holder-1)), 1)
	return Binder{ID: year*10 + holder, CoverholderID: holder, YearOfAccount: year, Inception: inception, Expiry: inception.AddMonths(12).AddDays(-1)}
}

// BinderPlans forecasts the number of writings at budgeted growth and prices
// a representative sample of anticipated risks. Neither input comes from the
// simulated book. The estimate is fixed on the binder inception date.
func (s *BookSimulator) BinderPlans(src shared.RandomSource, startYear, years, initialSize int) []Binder {
	var plans []Binder
	for year := startYear - 1; year < startYear+years; year++ {
		age := float64(year - startYear)
		plannedCount := float64(initialSize) * math.Pow(s.book.GrowthFactor, age) / Coverholders
		drift := math.Pow(s.book.SumInsuredInflation, age)
		uw := underwritingYear{startYear: startYear, year: year, lossRatio: s.pricing.TargetLossRatio, medianSI: s.book.SumInsuredMedian * drift, siDrift: drift}
		for holder := 1; holder <= Coverholders; holder++ {
			b := annualBinder(year, holder)
			assumptions := src.Split(fmt.Sprintf("plan-%d", b.ID))
			const samples = 128
			var total float64
			for range samples {
				si, risk := s.drawVehicle(assumptions, uw.medianSI, 1)
				inception := b.Inception.AddDays(int(assumptions.Uniform() * float64(shared.DaysBetween(b.Inception, b.Expiry)+1)))
				total += s.price(0, 0, inception, si, risk, s.drawExcess(assumptions), uw).Premium.Dollars()
			}
			b.Estimate = shared.FromDollars(plannedCount * total / samples)
			plans = append(plans, b)
		}
	}
	return plans
}
