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
				Shape:              1.5,
				MeanDays:           60,
				SizeThreshold:      20000,
				SizeMultiplier:     4,
				RiskLoading:        0.5,
				ThirdPartyShape:    1.0,
				ThirdPartyMeanDays: 900,
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
		{"claims.severity.third_party_weight", func(l *LineOfBusiness) { l.Claims.Severity.ThirdPartyWeight = 1.5 }},
		{"claims.severity.own_damage_median_fraction", func(l *LineOfBusiness) { l.Claims.Severity.OwnDamageMedianFraction = 0 }},
		{"claims.severity.own_damage_sigma", func(l *LineOfBusiness) { l.Claims.Severity.OwnDamageSigma = 0 }},
		{"claims.severity.third_party_scale", func(l *LineOfBusiness) { l.Claims.Severity.ThirdPartyScale = 0 }},
		{"claims.severity.third_party_alpha", func(l *LineOfBusiness) { l.Claims.Severity.ThirdPartyAlpha = 1.0 }},
		{"claims.close_lag.shape", func(l *LineOfBusiness) { l.Claims.CloseLag.Shape = 0 }},
		{"claims.close_lag.mean_days", func(l *LineOfBusiness) { l.Claims.CloseLag.MeanDays = 0 }},
		{"claims.close_lag.size_threshold", func(l *LineOfBusiness) { l.Claims.CloseLag.SizeThreshold = -1 }},
		{"claims.close_lag.size_multiplier", func(l *LineOfBusiness) { l.Claims.CloseLag.SizeMultiplier = 0.5 }},
		{"claims.close_lag.risk_loading", func(l *LineOfBusiness) { l.Claims.CloseLag.RiskLoading = -0.1 }},
		{"claims.close_lag.third_party_shape", func(l *LineOfBusiness) { l.Claims.CloseLag.ThirdPartyShape = 0 }},
		{"claims.close_lag.third_party_mean_days", func(l *LineOfBusiness) { l.Claims.CloseLag.ThirdPartyMeanDays = 0 }},
		{"runoff.case_adequacy_mean", func(l *LineOfBusiness) { l.Runoff.CaseAdequacyMean = 0 }},
		{"runoff.case_adequacy_sigma", func(l *LineOfBusiness) { l.Runoff.CaseAdequacySigma = -1 }},
		{"runoff.payments_per_year", func(l *LineOfBusiness) { l.Runoff.PaymentsPerYear = -1 }},
		{"runoff.settlement_share", func(l *LineOfBusiness) { l.Runoff.SettlementShare = 0 }},
		{"runoff.settlement_share", func(l *LineOfBusiness) { l.Runoff.SettlementShare = 1.5 }},
		{"runoff.concentration", func(l *LineOfBusiness) { l.Runoff.Concentration = 0 }},
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

func TestValidateAcceptsZeroRecoveryProbabilities(t *testing.T) {
	l := validMotor()
	l.Claims.Recoveries.Salvage.Probability = 0
	l.Claims.Recoveries.Subrogation.Probability = 0
	if err := l.Validate(); err != nil {
		t.Fatalf("zero recovery probabilities (the off switch): want nil, got %v", err)
	}
}

func TestValidateAcceptsZeroReopeningProbability(t *testing.T) {
	l := validMotor()
	l.Claims.Reopening.Probability = 0
	if err := l.Validate(); err != nil {
		t.Fatalf("zero reopening probability (the off switch): want nil, got %v", err)
	}
}
