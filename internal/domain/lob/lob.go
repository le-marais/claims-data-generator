// Package lob defines the LineOfBusiness value object: the complete
// parameter set that makes the simulation engine reusable across classes
// of business.
package lob

import "fmt"

type LineOfBusiness struct {
	Name   string
	Book   BookParams
	Claims ClaimParams
	Runoff RunoffParams
}

// BookParams drives step 1, the policy book simulation.
type BookParams struct {
	// GrowthFactor is the year-on-year trend in policy count; each year's
	// book size is previous size x GrowthFactor x lognormal noise.
	GrowthFactor float64
	// SizeVolatility is the sigma of the mean-1 lognormal noise on book size.
	SizeVolatility float64
	// Spread is the heterogeneity knob: sigma of the sum insured lognormal
	// and the standard deviation of the mean-1 risk factor gamma.
	Spread float64
	// SumInsuredMedian is the year-1 median sum insured in dollars.
	SumInsuredMedian float64
	// SumInsuredInflation is the annual multiplicative drift of the median.
	SumInsuredInflation float64
	// ExcessChoices is the discrete set of available excesses with weights.
	ExcessChoices []ExcessChoice
	// PremiumRateFactor sets premium = sum insured x rate x risk factor.
	PremiumRateFactor float64
}

type ExcessChoice struct {
	Value  float64
	Weight float64
}

// ClaimParams drives step 2, claim event simulation.
type ClaimParams struct {
	// BaseFrequency is expected reported claims per policy-year (risk
	// factor 1).
	BaseFrequency float64
	// ReportLagMedian is the median occurrence-to-report lag in days.
	ReportLagMedian float64
	// ReportLagSigma is the sigma of the lognormal report lag.
	ReportLagSigma float64
	Severity       SeverityParams
	CloseLag       CloseLagParams
}

// SeverityParams is the ground-up loss mixture: own damage (lognormal
// scaled by sum insured) and third party liability (Pareto, uncapped).
type SeverityParams struct {
	// ThirdPartyWeight is the probability a claim is third party.
	ThirdPartyWeight float64
	// OwnDamageMedianFraction is the median loss as a fraction of sum insured.
	OwnDamageMedianFraction float64
	// OwnDamageSigma is the sigma of the own damage lognormal.
	OwnDamageSigma float64
	// ThirdPartyScale is the Pareto scale (minimum) in dollars.
	ThirdPartyScale float64
	// ThirdPartyAlpha is the Pareto tail index; must exceed 1 for a finite mean.
	ThirdPartyAlpha float64
}

// CloseLagParams is the gamma report-to-close lag with size and risk loadings.
type CloseLagParams struct {
	// Shape is the gamma shape; above 1 avoids mass at near-zero delays.
	Shape float64
	// MeanDays is the base mean close lag.
	MeanDays float64
	// SizeThreshold is the initial estimate (dollars) above which the mean
	// lag is stretched by SizeMultiplier.
	SizeThreshold float64
	// SizeMultiplier stretches the mean lag for claims above the threshold.
	SizeMultiplier float64
	// RiskLoading is the exponent applied to the policy risk factor.
	RiskLoading float64
}

// RunoffParams drives steps 3-4, the case estimate path and payments.
type RunoffParams struct {
	// CaseAdequacyMean is the mean of ultimate / initial estimate:
	// systematic over- or under-reserving.
	CaseAdequacyMean float64
	// CaseAdequacySigma is how wrong individual initial estimates are.
	CaseAdequacySigma float64
	// PaymentsPerYear is the Poisson intensity of interim payments over the
	// claim's open duration.
	PaymentsPerYear float64
	// SettlementShare is the fraction of ultimate reserved for the final
	// settlement payment at close.
	SettlementShare float64
	// Concentration is the Dirichlet concentration splitting the remainder
	// across interim payments.
	Concentration float64
	// RevisionsPerYear is the Poisson intensity of pure case revisions.
	RevisionsPerYear float64
	// RevisionSigma is the initial sigma of revision noise; it decays as the
	// claim ages.
	RevisionSigma float64
}

// Validate checks every parameter and names the offending field in errors.
func (l LineOfBusiness) Validate() error {
	if l.Name == "" {
		return fmt.Errorf("name: must not be empty")
	}
	if err := l.Book.validate(); err != nil {
		return err
	}
	if err := l.Claims.validate(); err != nil {
		return err
	}
	return l.Runoff.validate()
}

func (b BookParams) validate() error {
	if b.GrowthFactor <= 0 {
		return fmt.Errorf("book.growth_factor: must be positive, got %v", b.GrowthFactor)
	}
	if b.SizeVolatility < 0 {
		return fmt.Errorf("book.size_volatility: must not be negative, got %v", b.SizeVolatility)
	}
	if b.Spread <= 0 {
		return fmt.Errorf("book.spread: must be positive, got %v", b.Spread)
	}
	if b.SumInsuredMedian <= 0 {
		return fmt.Errorf("book.sum_insured_median: must be positive, got %v", b.SumInsuredMedian)
	}
	if b.SumInsuredInflation <= 0 {
		return fmt.Errorf("book.sum_insured_inflation: must be positive, got %v", b.SumInsuredInflation)
	}
	if len(b.ExcessChoices) == 0 {
		return fmt.Errorf("book.excess_choices: must not be empty")
	}
	totalWeight := 0.0
	for i, c := range b.ExcessChoices {
		if c.Value < 0 {
			return fmt.Errorf("book.excess_choices[%d].value: must not be negative, got %v", i, c.Value)
		}
		if c.Weight < 0 {
			return fmt.Errorf("book.excess_choices[%d].weight: must not be negative, got %v", i, c.Weight)
		}
		totalWeight += c.Weight
	}
	if totalWeight <= 0 {
		return fmt.Errorf("book.excess_choices: weights must sum to a positive value")
	}
	if b.PremiumRateFactor <= 0 {
		return fmt.Errorf("book.premium_rate_factor: must be positive, got %v", b.PremiumRateFactor)
	}
	return nil
}

func (c ClaimParams) validate() error {
	if c.BaseFrequency <= 0 {
		return fmt.Errorf("claims.base_frequency: must be positive, got %v", c.BaseFrequency)
	}
	if c.ReportLagMedian <= 0 {
		return fmt.Errorf("claims.report_lag_median: must be positive, got %v", c.ReportLagMedian)
	}
	if c.ReportLagSigma <= 0 {
		return fmt.Errorf("claims.report_lag_sigma: must be positive, got %v", c.ReportLagSigma)
	}
	if err := c.Severity.validate(); err != nil {
		return err
	}
	return c.CloseLag.validate()
}

func (s SeverityParams) validate() error {
	if s.ThirdPartyWeight < 0 || s.ThirdPartyWeight > 1 {
		return fmt.Errorf("severity.third_party_weight: must be in [0, 1], got %v", s.ThirdPartyWeight)
	}
	if s.OwnDamageMedianFraction <= 0 {
		return fmt.Errorf("severity.own_damage_median_fraction: must be positive, got %v", s.OwnDamageMedianFraction)
	}
	if s.OwnDamageSigma <= 0 {
		return fmt.Errorf("severity.own_damage_sigma: must be positive, got %v", s.OwnDamageSigma)
	}
	if s.ThirdPartyScale <= 0 {
		return fmt.Errorf("severity.third_party_scale: must be positive, got %v", s.ThirdPartyScale)
	}
	if s.ThirdPartyAlpha <= 1 {
		return fmt.Errorf("severity.third_party_alpha: must exceed 1 for a finite mean, got %v", s.ThirdPartyAlpha)
	}
	return nil
}

func (c CloseLagParams) validate() error {
	if c.Shape <= 0 {
		return fmt.Errorf("close_lag.shape: must be positive, got %v", c.Shape)
	}
	if c.MeanDays <= 0 {
		return fmt.Errorf("close_lag.mean_days: must be positive, got %v", c.MeanDays)
	}
	if c.SizeThreshold < 0 {
		return fmt.Errorf("close_lag.size_threshold: must not be negative, got %v", c.SizeThreshold)
	}
	if c.SizeMultiplier < 1 {
		return fmt.Errorf("close_lag.size_multiplier: must be at least 1, got %v", c.SizeMultiplier)
	}
	if c.RiskLoading < 0 {
		return fmt.Errorf("close_lag.risk_loading: must not be negative, got %v", c.RiskLoading)
	}
	return nil
}

func (r RunoffParams) validate() error {
	if r.CaseAdequacyMean <= 0 {
		return fmt.Errorf("runoff.case_adequacy_mean: must be positive, got %v", r.CaseAdequacyMean)
	}
	if r.CaseAdequacySigma < 0 {
		return fmt.Errorf("runoff.case_adequacy_sigma: must not be negative, got %v", r.CaseAdequacySigma)
	}
	if r.PaymentsPerYear < 0 {
		return fmt.Errorf("runoff.payments_per_year: must not be negative, got %v", r.PaymentsPerYear)
	}
	if r.SettlementShare <= 0 || r.SettlementShare > 1 {
		return fmt.Errorf("runoff.settlement_share: must be in (0, 1], got %v", r.SettlementShare)
	}
	if r.Concentration <= 0 {
		return fmt.Errorf("runoff.concentration: must be positive, got %v", r.Concentration)
	}
	if r.RevisionsPerYear < 0 {
		return fmt.Errorf("runoff.revisions_per_year: must not be negative, got %v", r.RevisionsPerYear)
	}
	if r.RevisionSigma < 0 {
		return fmt.Errorf("runoff.revision_sigma: must not be negative, got %v", r.RevisionSigma)
	}
	return nil
}
