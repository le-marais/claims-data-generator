package lob

import (
	"math"
	"strings"
	"testing"
)

// validMotor returns a fully valid parameter set resembling personal motor.
func validMotor() LineOfBusiness {
	return LineOfBusiness{
		Name: "motor-personal",
		Book: BookParams{
			GrowthFactor:        1.05,
			SizeVolatility:      0.05,
			Spread:              0.4,
			SumInsuredMedian:    20000,
			SumInsuredInflation: 1.03,
			ExcessChoices: []ExcessChoice{
				{Value: 0, Weight: 0.1},
				{Value: 100, Weight: 0.2},
				{Value: 300, Weight: 0.3},
				{Value: 500, Weight: 0.3},
				{Value: 1000, Weight: 0.1},
			},
		},
		Pricing: PricingParams{
			TargetLossRatio: 0.72,
			Sections: []PricingSectionParams{
				{Name: "own_damage", BaseFrequency: 0.1275, Severity: SeverityParams{Kind: SumInsuredLognormal, MedianFraction: 0.15, Sigma: 1.0}},
				{Name: "third_party", BaseFrequency: 0.0225, Severity: SeverityParams{Kind: Pareto, Scale: 5000, Alpha: 2.0}},
			},
			ReopenProbability:    0.04,
			ReopenEstimateFactor: 0.45,
			InflationMean:        1.0,
		},
		Claims: ClaimParams{
			Sections: []SectionParams{
				{
					Name:          "own_damage",
					BaseFrequency: 0.1275,
					Severity:      SeverityParams{Kind: SumInsuredLognormal, MedianFraction: 0.15, Sigma: 1.0},
					ReportLag:     ReportLagParams{Median: 2, Sigma: 1.0},
					CloseLag:      CloseLagParams{Shape: 1.5, MeanDays: 60, SizeReference: 3000, SizeElasticity: 0.2, RiskLoading: 0.5},
					Settlement:    SettlementParams{LumpSumProbability: 0.5, Share: 0.25, Concentration: 4},
					Recoveries:    true,
				},
				{
					Name:          "third_party",
					BaseFrequency: 0.0225,
					Severity:      SeverityParams{Kind: Pareto, Scale: 5000, Alpha: 2.0},
					ReportLag:     ReportLagParams{Median: 20, Sigma: 1.5},
					CloseLag:      CloseLagParams{Shape: 1.0, MeanDays: 900, RiskLoading: 0.5},
					Settlement:    SettlementParams{Share: 0.6, Concentration: 4},
				},
			},
			Inflation: InflationParams{Mean: 1.0, Volatility: 0.0},
			Recoveries: RecoveryParams{
				Salvage:     RecoveryTypeParams{Probability: 0.1, MeanShare: 0.15, Concentration: 10, LagMedianDays: 21, LagSigma: 0.5},
				Subrogation: RecoveryTypeParams{Probability: 0.2, MeanShare: 0.8, Concentration: 10, LagMedianDays: 180, LagSigma: 0.7},
			},
			Reopening: ReopeningParams{Probability: 0.04, EstimateFactor: 0.45, EstimateSigma: 0.5, LagMedianDays: 90, LagSigma: 0.7},
		},
		Runoff: RunoffParams{
			CaseAdequacyMean:  1.0,
			CaseAdequacySigma: 0.3,
			PaymentsPerYear:   3,
			Concentration:     4,
			MinPayment:        50,
			RevisionsPerYear:  4,
			RevisionSigma:     0.3,
		},
	}
}

func TestValidLineOfBusinessPasses(t *testing.T) {
	if err := validMotor().Validate(); err != nil {
		t.Fatalf("valid parameter set rejected: %v", err)
	}
}

// validFleet is a fleet book with every field set.
func validFleet() FleetParams {
	return FleetParams{Size: FleetSizeParams{Median: 3, Sigma: 1}, SumInsuredSigma: 0.5, RiskSpread: 0.4}
}

func TestFleetExpectedSize(t *testing.T) {
	if got := (FleetParams{}).ExpectedSize(); got != 1 {
		t.Errorf("fleets off: ExpectedSize = %v, want 1", got)
	}
	f := validFleet()
	if got, want := f.ExpectedSize(), 3*math.Exp(0.5); math.Abs(got-want) > 1e-12 {
		t.Errorf("ExpectedSize = %v, want the lognormal mean %v", got, want)
	}
}

func TestValidationNamesTheOffendingField(t *testing.T) {
	cases := []struct {
		field  string
		mutate func(*LineOfBusiness)
	}{
		{"name", func(l *LineOfBusiness) { l.Name = "" }},
		{"claims.sections[1].severity.median", func(l *LineOfBusiness) {
			l.Claims.Sections[1].Severity = SeverityParams{Kind: Lognormal, Sigma: 0.8}
		}},
		{"claims.sections[1].severity.sigma", func(l *LineOfBusiness) {
			l.Claims.Sections[1].Severity = SeverityParams{Kind: Lognormal, Median: 2000}
		}},
		{"claims.sections[1].severity.median", func(l *LineOfBusiness) {
			l.Claims.Sections[1].Severity = SeverityParams{Kind: LognormalPareto, Sigma: 1, Scale: 25000, Alpha: 2}
		}},
		{"claims.sections[1].severity.sigma", func(l *LineOfBusiness) {
			l.Claims.Sections[1].Severity = SeverityParams{Kind: LognormalPareto, Median: 4000, Scale: 25000, Alpha: 2}
		}},
		{"claims.sections[1].severity.scale", func(l *LineOfBusiness) {
			l.Claims.Sections[1].Severity = SeverityParams{Kind: LognormalPareto, Median: 4000, Sigma: 1, Alpha: 2}
		}},
		{"claims.sections[1].severity.alpha", func(l *LineOfBusiness) {
			l.Claims.Sections[1].Severity = SeverityParams{Kind: LognormalPareto, Median: 4000, Sigma: 1, Scale: 25000, Alpha: 1}
		}},
		{"book.growth_factor", func(l *LineOfBusiness) { l.Book.GrowthFactor = 0 }},
		{"seasonal_holiday.hemisphere", func(l *LineOfBusiness) { l.SeasonalHoliday.Hemisphere = "eastern" }},
		{"business_days.calendar", func(l *LineOfBusiness) { l.BusinessDays.Calendar = "fr" }},
		{"seasonal_holiday.report_share", func(l *LineOfBusiness) {
			l.SeasonalHoliday = SeasonalHolidayParams{Hemisphere: Northern, ReportShare: 1.1}
		}},
		{"seasonal_holiday.payment_share", func(l *LineOfBusiness) {
			l.SeasonalHoliday = SeasonalHolidayParams{Hemisphere: Southern, PaymentShare: -0.1}
		}},
		{"seasonal_holiday.report_share", func(l *LineOfBusiness) {
			l.SeasonalHoliday = SeasonalHolidayParams{Hemisphere: Northern, ReportShare: math.NaN()}
		}},
		{"book.size_volatility", func(l *LineOfBusiness) { l.Book.SizeVolatility = -0.1 }},
		{"book.spread", func(l *LineOfBusiness) { l.Book.Spread = 0 }},
		{"book.sum_insured_median", func(l *LineOfBusiness) { l.Book.SumInsuredMedian = 0 }},
		{"book.sum_insured_inflation", func(l *LineOfBusiness) { l.Book.SumInsuredInflation = 0 }},
		{"book.excess_choices", func(l *LineOfBusiness) { l.Book.ExcessChoices = nil }},
		{"book.excess_choices", func(l *LineOfBusiness) { l.Book.ExcessChoices[0].Weight = -1 }},
		{"book.excess_choices", func(l *LineOfBusiness) { l.Book.ExcessChoices[0].Value = -100 }},
		{"book.fleet.size.median", func(l *LineOfBusiness) { l.Book.Fleet = validFleet(); l.Book.Fleet.Size.Median = -1 }},
		{"book.fleet.size.median", func(l *LineOfBusiness) { l.Book.Fleet = validFleet(); l.Book.Fleet.Size.Median = 0.5 }},
		{"book.fleet.size.sigma", func(l *LineOfBusiness) { l.Book.Fleet = validFleet(); l.Book.Fleet.Size.Sigma = -0.1 }},
		{"book.fleet.sum_insured_sigma", func(l *LineOfBusiness) { l.Book.Fleet = validFleet(); l.Book.Fleet.SumInsuredSigma = -0.1 }},
		{"book.fleet.risk_spread", func(l *LineOfBusiness) { l.Book.Fleet = validFleet(); l.Book.Fleet.RiskSpread = -0.1 }},
		{"book.fleet.risk_spread", func(l *LineOfBusiness) { l.Book.Fleet = validFleet(); l.Book.Fleet.RiskSpread = math.NaN() }},
		{"pricing.target_loss_ratio", func(l *LineOfBusiness) { l.Pricing.TargetLossRatio = 0 }},
		{"pricing.adequacy_volatility", func(l *LineOfBusiness) { l.Pricing.AdequacyVolatility = -0.1 }},
		{"pricing.sections[0].base_frequency", func(l *LineOfBusiness) { l.Pricing.Sections[0].BaseFrequency = -0.1 }},
		{"pricing.sections[1].severity.alpha", func(l *LineOfBusiness) { l.Pricing.Sections[1].Severity.Alpha = 1.0 }},
		{"pricing.sections", func(l *LineOfBusiness) { l.Pricing.Sections = l.Pricing.Sections[:1] }},
		{"pricing.sections[1].name", func(l *LineOfBusiness) { l.Pricing.Sections[1].Name = "liability" }},
		{"pricing.nil_probability", func(l *LineOfBusiness) { l.Pricing.NilProbability = 1.0 }},
		{"pricing.nil_probability", func(l *LineOfBusiness) { l.Pricing.NilProbability = -0.1 }},
		{"pricing.reopen_probability", func(l *LineOfBusiness) { l.Pricing.ReopenProbability = 1.5 }},
		{"pricing.reopen_estimate_factor", func(l *LineOfBusiness) { l.Pricing.ReopenEstimateFactor = 0 }},
		{"pricing.inflation_mean", func(l *LineOfBusiness) { l.Pricing.InflationMean = 0 }},
		{"claims.sections", func(l *LineOfBusiness) { l.Claims.Sections, l.Pricing.Sections = nil, nil }},
		{"claims.sections", func(l *LineOfBusiness) {
			l.Claims.Sections[0].BaseFrequency, l.Claims.Sections[1].BaseFrequency = 0, 0
		}},
		{"claims.sections[0].name", func(l *LineOfBusiness) { l.Claims.Sections[0].Name, l.Pricing.Sections[0].Name = "", "" }},
		{"claims.sections[1].name", func(l *LineOfBusiness) {
			l.Claims.Sections[1].Name, l.Pricing.Sections[1].Name = "own_damage", "own_damage"
		}},
		{"claims.sections[0].base_frequency", func(l *LineOfBusiness) { l.Claims.Sections[0].BaseFrequency = -0.1 }},
		{"claims.sections[0].severity.kind", func(l *LineOfBusiness) { l.Claims.Sections[0].Severity.Kind = "weibull" }},
		{"claims.sections[0].severity.median_fraction", func(l *LineOfBusiness) { l.Claims.Sections[0].Severity.MedianFraction = 0 }},
		{"claims.sections[0].severity.sigma", func(l *LineOfBusiness) { l.Claims.Sections[0].Severity.Sigma = 0 }},
		{"claims.sections[1].severity.scale", func(l *LineOfBusiness) { l.Claims.Sections[1].Severity.Scale = 0 }},
		{"claims.sections[1].severity.alpha", func(l *LineOfBusiness) { l.Claims.Sections[1].Severity.Alpha = 1.0 }},
		{"claims.sections[0].report_lag.median", func(l *LineOfBusiness) { l.Claims.Sections[0].ReportLag.Median = 0 }},
		{"claims.sections[1].report_lag.sigma", func(l *LineOfBusiness) { l.Claims.Sections[1].ReportLag.Sigma = 0 }},
		{"claims.sections[0].close_lag.shape", func(l *LineOfBusiness) { l.Claims.Sections[0].CloseLag.Shape = 0 }},
		{"claims.sections[1].close_lag.mean_days", func(l *LineOfBusiness) { l.Claims.Sections[1].CloseLag.MeanDays = 0 }},
		{"claims.sections[0].close_lag.size_elasticity", func(l *LineOfBusiness) { l.Claims.Sections[0].CloseLag.SizeElasticity = -0.1 }},
		{"claims.sections[1].close_lag.size_reference", func(l *LineOfBusiness) { l.Claims.Sections[1].CloseLag.SizeElasticity = 0.3 }},
		{"claims.sections[0].close_lag.risk_loading", func(l *LineOfBusiness) { l.Claims.Sections[0].CloseLag.RiskLoading = -0.1 }},
		{"claims.sections[1].limit", func(l *LineOfBusiness) { l.Claims.Sections[1].Limit = -1 }},
		{"claims.sections[1].limit", func(l *LineOfBusiness) { l.Claims.Sections[1].Limit = math.Inf(1) }},
		{"claims.sections[0].limit", func(l *LineOfBusiness) { l.Claims.Sections[0].Limit = 50000 }},
		{"pricing.sections[1].limit", func(l *LineOfBusiness) { l.Pricing.Sections[1].Limit = -1 }},
		{"pricing.sections[1].limit", func(l *LineOfBusiness) { l.Pricing.Sections[1].Limit = math.NaN() }},
		{"pricing.sections[0].limit", func(l *LineOfBusiness) { l.Pricing.Sections[0].Limit = 50000 }},
		{"claims.sections[1].limit: must be 0 (unlimited) or at least 0.01", func(l *LineOfBusiness) { l.Claims.Sections[1].Limit = 0.004 }},
		{"pricing.sections[1].limit: must be 0 (unlimited) or at least 0.01", func(l *LineOfBusiness) { l.Pricing.Sections[1].Limit = 0.004 }},
		{"runoff.case_adequacy_mean", func(l *LineOfBusiness) { l.Runoff.CaseAdequacyMean = 0 }},
		{"runoff.case_adequacy_sigma", func(l *LineOfBusiness) { l.Runoff.CaseAdequacySigma = -1 }},
		{"runoff.payments_per_year", func(l *LineOfBusiness) { l.Runoff.PaymentsPerYear = -1 }},
		{"runoff.concentration", func(l *LineOfBusiness) { l.Runoff.Concentration = 0 }},
		{"runoff.min_payment", func(l *LineOfBusiness) { l.Runoff.MinPayment = -1 }},
		{"runoff.payment_delay_days", func(l *LineOfBusiness) { l.Runoff.PaymentDelayDays = -1 }},
		{"runoff.payment_delay_days", func(l *LineOfBusiness) { l.Runoff.PaymentDelayDays = 2.5 }},
		{"claims.sections[0].settlement.lump_sum_probability", func(l *LineOfBusiness) { l.Claims.Sections[0].Settlement.LumpSumProbability = -0.1 }},
		{"claims.sections[0].settlement.lump_sum_probability", func(l *LineOfBusiness) { l.Claims.Sections[0].Settlement.LumpSumProbability = 1.5 }},
		{"claims.sections[1].settlement.share", func(l *LineOfBusiness) { l.Claims.Sections[1].Settlement.Share = 0 }},
		{"claims.sections[1].settlement.share", func(l *LineOfBusiness) { l.Claims.Sections[1].Settlement.Share = 1.5 }},
		{"claims.sections[1].settlement.share", func(l *LineOfBusiness) { l.Claims.Sections[1].Settlement.Share = 1 }},
		{"claims.sections[1].settlement.concentration", func(l *LineOfBusiness) { l.Claims.Sections[1].Settlement.Concentration = -1 }},
		{"claims.sections[1].settlement.share", func(l *LineOfBusiness) { l.Claims.Sections[1].Settlement.Share = math.NaN() }},
		{"runoff.revisions_per_year", func(l *LineOfBusiness) { l.Runoff.RevisionsPerYear = -1 }},
		{"runoff.revision_sigma", func(l *LineOfBusiness) { l.Runoff.RevisionSigma = -1 }},
		{"claims.recoveries.salvage.probability", func(l *LineOfBusiness) { l.Claims.Recoveries.Salvage.Probability = 1.0 }},
		{"claims.recoveries.salvage.probability", func(l *LineOfBusiness) { l.Claims.Recoveries.Salvage.Probability = -0.1 }},
		{"claims.recoveries.salvage.mean_share", func(l *LineOfBusiness) { l.Claims.Recoveries.Salvage.MeanShare = 0 }},
		{"claims.recoveries.salvage.mean_share", func(l *LineOfBusiness) { l.Claims.Recoveries.Salvage.MeanShare = 1.0 }},
		{"claims.recoveries.salvage.concentration", func(l *LineOfBusiness) { l.Claims.Recoveries.Salvage.Concentration = 0 }},
		{"claims.recoveries.salvage.lag_median_days", func(l *LineOfBusiness) { l.Claims.Recoveries.Salvage.LagMedianDays = 0 }},
		{"claims.recoveries.salvage.lag_sigma", func(l *LineOfBusiness) { l.Claims.Recoveries.Salvage.LagSigma = -0.1 }},
		{"claims.recoveries.subrogation.probability", func(l *LineOfBusiness) { l.Claims.Recoveries.Subrogation.Probability = 1.5 }},
		{"claims.recoveries.subrogation.mean_share", func(l *LineOfBusiness) { l.Claims.Recoveries.Subrogation.MeanShare = -0.2 }},
		{"claims.reopening.probability", func(l *LineOfBusiness) { l.Claims.Reopening.Probability = 1.0 }},
		{"claims.reopening.probability", func(l *LineOfBusiness) { l.Claims.Reopening.Probability = -0.1 }},
		{"claims.reopening.estimate_factor", func(l *LineOfBusiness) { l.Claims.Reopening.EstimateFactor = 0 }},
		{"claims.reopening.estimate_sigma", func(l *LineOfBusiness) { l.Claims.Reopening.EstimateSigma = -0.1 }},
		{"claims.reopening.lag_median_days", func(l *LineOfBusiness) { l.Claims.Reopening.LagMedianDays = 0 }},
		{"claims.reopening.lag_sigma", func(l *LineOfBusiness) { l.Claims.Reopening.LagSigma = -0.1 }},
	}
	for _, c := range cases {
		l := validMotor()
		c.mutate(&l)
		err := l.Validate()
		if err == nil {
			t.Errorf("expected validation error mentioning %q, got nil", c.field)
			continue
		}
		if !strings.Contains(err.Error(), c.field) {
			t.Errorf("error %q does not name field %q", err.Error(), c.field)
		}
	}
}

func TestValidateRejectsNonFiniteFloat(t *testing.T) {
	l := validMotor()
	l.Book.GrowthFactor = math.NaN()
	err := l.Validate()
	if err == nil {
		t.Fatal("NaN growth_factor: want error, got nil")
	}
	if !strings.Contains(err.Error(), "book.growth_factor") {
		t.Errorf("error %q does not name field %q", err.Error(), "book.growth_factor")
	}
}

func TestExcessWeightsMustSumPositive(t *testing.T) {
	l := validMotor()
	for i := range l.Book.ExcessChoices {
		l.Book.ExcessChoices[i].Weight = 0
	}
	if err := l.Validate(); err == nil {
		t.Error("expected error for all-zero excess weights")
	}
}

func TestValidateRejectsNonPositiveInflationMean(t *testing.T) {
	l := validMotor()
	l.Claims.Inflation.Mean = 0
	if err := l.Validate(); err == nil {
		t.Fatal("inflation mean 0: want error, got nil")
	}
}

func TestValidateRejectsNegativeInflationVolatility(t *testing.T) {
	l := validMotor()
	l.Claims.Inflation.Volatility = -0.1
	if err := l.Validate(); err == nil {
		t.Fatal("negative inflation volatility: want error, got nil")
	}
}

func TestValidateRejectsNilProbabilityOutOfRange(t *testing.T) {
	for _, p := range []float64{-0.01, 1.0, 1.5} {
		l := validMotor()
		l.Claims.NilProbability = p
		if err := l.Validate(); err == nil {
			t.Fatalf("nil_probability %v: want error, got nil", p)
		}
	}
}

func TestValidateAcceptsIdentityInflationAndZeroNil(t *testing.T) {
	l := validMotor()
	l.Claims.Inflation = InflationParams{Mean: 1.0, Volatility: 0.0}
	l.Claims.NilProbability = 0
	if err := l.Validate(); err != nil {
		t.Fatalf("identity inflation and zero nil: want nil, got %v", err)
	}
}

// A sub-block that is switched off is never read, so a YAML author need not
// invent parameters for it (MF-3): every field the switch makes unused is
// left at its zero value here.
func TestValidateSkipsSwitchedOffBlocks(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*LineOfBusiness)
	}{
		{"lognormal-pareto third party", func(l *LineOfBusiness) {
			l.Claims.Sections[1].Severity = SeverityParams{Kind: LognormalPareto, Median: 4000, Sigma: 1, Scale: 25000, Alpha: 2}
			l.Pricing.Sections[1].Severity = SeverityParams{Kind: LognormalPareto, Median: 4000, Sigma: 1, Scale: 25000, Alpha: 2}
		}},
		{"lognormal third party", func(l *LineOfBusiness) {
			l.Claims.Sections[1].Severity = SeverityParams{Kind: Lognormal, Median: 2000, Sigma: 0.8}
			l.Pricing.Sections[1].Severity = SeverityParams{Kind: Lognormal, Median: 2000, Sigma: 0.8}
		}},
		{"seasonal holiday off with stray shares", func(l *LineOfBusiness) {
			l.SeasonalHoliday = SeasonalHolidayParams{Hemisphere: NoHoliday, ReportShare: 5, PaymentShare: math.NaN()}
		}},
		{"business days off with roll reports", func(l *LineOfBusiness) {
			l.BusinessDays = BusinessDayParams{Calendar: "none", RollReports: true}
		}},
		{"business days on", func(l *LineOfBusiness) { l.BusinessDays = BusinessDayParams{Calendar: "za"} }},
		{"seasonal holiday on", func(l *LineOfBusiness) {
			l.SeasonalHoliday = SeasonalHolidayParams{Hemisphere: Northern, ReportShare: 0.1, PaymentShare: 1}
		}},
		{"salvage off", func(l *LineOfBusiness) {
			l.Claims.Recoveries.Salvage = RecoveryTypeParams{}
		}},
		{"fleets", func(l *LineOfBusiness) { l.Book.Fleet = validFleet() }},
		{"fleets with no spreads", func(l *LineOfBusiness) {
			l.Book.Fleet = FleetParams{Size: FleetSizeParams{Median: 1}}
		}},
		{"fleets off", func(l *LineOfBusiness) {
			l.Book.Fleet = FleetParams{Size: FleetSizeParams{Sigma: -1}, SumInsuredSigma: -1, RiskSpread: -1}
		}},
		{"subrogation off", func(l *LineOfBusiness) {
			l.Claims.Recoveries.Subrogation = RecoveryTypeParams{}
		}},
		{"reopening off", func(l *LineOfBusiness) {
			l.Claims.Reopening = ReopeningParams{}
		}},
		{"pricing reopen off", func(l *LineOfBusiness) {
			l.Pricing.ReopenProbability = 0
			l.Pricing.ReopenEstimateFactor = 0
		}},
		{"third party off", func(l *LineOfBusiness) {
			l.Claims.Sections[1] = SectionParams{Name: "third_party"}
			l.Pricing.Sections[1] = PricingSectionParams{Name: "third_party"}
		}},
		{"limited pareto third party", func(l *LineOfBusiness) {
			l.Claims.Sections[1].Limit = 100000
			l.Pricing.Sections[1].Limit = 100000
		}},
		{"limited lognormal third party", func(l *LineOfBusiness) {
			l.Claims.Sections[1].Severity = SeverityParams{Kind: Lognormal, Median: 2000, Sigma: 0.8}
			l.Claims.Sections[1].Limit = 50000
			l.Pricing.Sections[1].Severity = SeverityParams{Kind: Lognormal, Median: 2000, Sigma: 0.8}
			l.Pricing.Sections[1].Limit = 50000
		}},
	}
	for _, c := range cases {
		l := validMotor()
		c.mutate(&l)
		if err := l.Validate(); err != nil {
			t.Errorf("%s: want nil, got %v", c.name, err)
		}
	}
}

// The skip is tied to the switch: the same zeroed fields are still rejected
// while the block is on.
func TestValidateChecksEnabledBlocks(t *testing.T) {
	cases := []struct {
		field  string
		mutate func(*LineOfBusiness)
	}{
		{"claims.recoveries.salvage.mean_share", func(l *LineOfBusiness) {
			l.Claims.Recoveries.Salvage = RecoveryTypeParams{Probability: 0.1}
		}},
		{"claims.reopening.estimate_factor", func(l *LineOfBusiness) {
			l.Claims.Reopening = ReopeningParams{Probability: 0.1}
		}},
		{"claims.sections[1].severity.kind", func(l *LineOfBusiness) {
			l.Claims.Sections[1] = SectionParams{Name: "third_party", BaseFrequency: 0.02}
		}},
		{"pricing.sections[1].severity.kind", func(l *LineOfBusiness) {
			l.Pricing.Sections[1] = PricingSectionParams{Name: "third_party", BaseFrequency: 0.02}
		}},
	}
	for _, c := range cases {
		l := validMotor()
		c.mutate(&l)
		err := l.Validate()
		if err == nil || !strings.Contains(err.Error(), c.field) {
			t.Errorf("want error naming %q, got %v", c.field, err)
		}
	}
}

// A sum-insured section's limit is its sum insured, so a separate limit on it
// is rejected rather than silently stacked on the cap.
func TestValidateRejectsALimitOnASumInsuredSection(t *testing.T) {
	l := validMotor()
	l.Claims.Sections[0].Limit = 50000
	want := "claims.sections[0].limit: must be 0 on a sum_insured_lognormal section, whose limit is its sum insured, got 50000"
	if err := l.Validate(); err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
}

// A section that always pays in one lump sum never reads its share, so the
// share is not required, and a fixed share of 1 is a valid way to pay
// everything at close.
func TestSettlementShareIsNotRequiredForAlwaysLumpSum(t *testing.T) {
	l := validMotor()
	l.Claims.Sections[1].Settlement = SettlementParams{LumpSumProbability: 1}
	if err := l.Validate(); err != nil {
		t.Errorf("lump_sum_probability 1 without a share: %v", err)
	}
	l.Claims.Sections[1].Settlement = SettlementParams{Share: 1}
	if err := l.Validate(); err != nil {
		t.Errorf("fixed share 1: %v", err)
	}
}
