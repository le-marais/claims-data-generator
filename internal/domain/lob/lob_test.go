package lob

import (
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
			PremiumRateFactor: 0.03,
		},
		Claims: ClaimParams{
			BaseFrequency:   0.15,
			ReportLagMedian: 2,
			ReportLagSigma:  1.0,
			Severity: SeverityParams{
				ThirdPartyWeight:        0.15,
				OwnDamageMedianFraction: 0.15,
				OwnDamageSigma:          1.0,
				ThirdPartyScale:         5000,
				ThirdPartyAlpha:         2.0,
			},
			CloseLag: CloseLagParams{
				Shape:          1.5,
				MeanDays:       60,
				SizeThreshold:  20000,
				SizeMultiplier: 4,
				RiskLoading:    0.5,
			},
			Inflation: InflationParams{Mean: 1.0, Volatility: 0.0},
		},
		Runoff: RunoffParams{
			CaseAdequacyMean:  1.0,
			CaseAdequacySigma: 0.3,
			PaymentsPerYear:   3,
			SettlementShare:   0.4,
			Concentration:     1.0,
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

func TestValidationNamesTheOffendingField(t *testing.T) {
	cases := []struct {
		field  string
		mutate func(*LineOfBusiness)
	}{
		{"name", func(l *LineOfBusiness) { l.Name = "" }},
		{"book.growth_factor", func(l *LineOfBusiness) { l.Book.GrowthFactor = 0 }},
		{"book.size_volatility", func(l *LineOfBusiness) { l.Book.SizeVolatility = -0.1 }},
		{"book.spread", func(l *LineOfBusiness) { l.Book.Spread = 0 }},
		{"book.sum_insured_median", func(l *LineOfBusiness) { l.Book.SumInsuredMedian = 0 }},
		{"book.sum_insured_inflation", func(l *LineOfBusiness) { l.Book.SumInsuredInflation = 0 }},
		{"book.excess_choices", func(l *LineOfBusiness) { l.Book.ExcessChoices = nil }},
		{"book.excess_choices", func(l *LineOfBusiness) { l.Book.ExcessChoices[0].Weight = -1 }},
		{"book.excess_choices", func(l *LineOfBusiness) { l.Book.ExcessChoices[0].Value = -100 }},
		{"book.premium_rate_factor", func(l *LineOfBusiness) { l.Book.PremiumRateFactor = 0 }},
		{"claims.base_frequency", func(l *LineOfBusiness) { l.Claims.BaseFrequency = 0 }},
		{"claims.report_lag_median", func(l *LineOfBusiness) { l.Claims.ReportLagMedian = 0 }},
		{"claims.report_lag_sigma", func(l *LineOfBusiness) { l.Claims.ReportLagSigma = 0 }},
		{"severity.third_party_weight", func(l *LineOfBusiness) { l.Claims.Severity.ThirdPartyWeight = 1.5 }},
		{"severity.own_damage_median_fraction", func(l *LineOfBusiness) { l.Claims.Severity.OwnDamageMedianFraction = 0 }},
		{"severity.own_damage_sigma", func(l *LineOfBusiness) { l.Claims.Severity.OwnDamageSigma = 0 }},
		{"severity.third_party_scale", func(l *LineOfBusiness) { l.Claims.Severity.ThirdPartyScale = 0 }},
		{"severity.third_party_alpha", func(l *LineOfBusiness) { l.Claims.Severity.ThirdPartyAlpha = 1.0 }},
		{"close_lag.shape", func(l *LineOfBusiness) { l.Claims.CloseLag.Shape = 0 }},
		{"close_lag.mean_days", func(l *LineOfBusiness) { l.Claims.CloseLag.MeanDays = 0 }},
		{"close_lag.size_threshold", func(l *LineOfBusiness) { l.Claims.CloseLag.SizeThreshold = -1 }},
		{"close_lag.size_multiplier", func(l *LineOfBusiness) { l.Claims.CloseLag.SizeMultiplier = 0.5 }},
		{"close_lag.risk_loading", func(l *LineOfBusiness) { l.Claims.CloseLag.RiskLoading = -0.1 }},
		{"runoff.case_adequacy_mean", func(l *LineOfBusiness) { l.Runoff.CaseAdequacyMean = 0 }},
		{"runoff.case_adequacy_sigma", func(l *LineOfBusiness) { l.Runoff.CaseAdequacySigma = -1 }},
		{"runoff.payments_per_year", func(l *LineOfBusiness) { l.Runoff.PaymentsPerYear = -1 }},
		{"runoff.settlement_share", func(l *LineOfBusiness) { l.Runoff.SettlementShare = 0 }},
		{"runoff.settlement_share", func(l *LineOfBusiness) { l.Runoff.SettlementShare = 1.5 }},
		{"runoff.concentration", func(l *LineOfBusiness) { l.Runoff.Concentration = 0 }},
		{"runoff.revisions_per_year", func(l *LineOfBusiness) { l.Runoff.RevisionsPerYear = -1 }},
		{"runoff.revision_sigma", func(l *LineOfBusiness) { l.Runoff.RevisionSigma = -1 }},
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
