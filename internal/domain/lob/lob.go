// Package lob defines the LineOfBusiness value object: the complete
// parameter set that makes the simulation engine reusable across classes
// of business.
package lob

import (
	"fmt"
	"math"
)

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
	// BaseFrequency is the ground-up occurrence frequency per policy-year at
	// risk factor 1. With a non-zero excess the realized reported frequency is
	// lower, because sub-excess claims are discarded rather than reported.
	BaseFrequency float64
	// ReportLagMedian is the median occurrence-to-report lag in days.
	ReportLagMedian float64
	// ReportLagSigma is the sigma of the lognormal report lag.
	ReportLagSigma float64
	Severity       SeverityParams
	CloseLag       CloseLagParams
	// Inflation is the stochastic claims-inflation path applied by
	// occurrence year to every claim's ground-up loss.
	Inflation InflationParams
	// NilProbability is the chance a reported claim closes without payment;
	// 0 switches nil claims off.
	NilProbability float64
	// Recoveries drives salvage and subrogation: money coming back on
	// own-damage claims after they close.
	Recoveries RecoveryParams
	// Reopening drives the single optional reopen episode: a closed claim
	// can reopen once, develop further, and close again.
	Reopening ReopeningParams
}

// InflationParams is the stochastic annual claims-inflation path: each
// calendar year's factor is Mean times mean-1 lognormal noise of sigma
// Volatility, compounded from an index of 1.0 in the start year.
type InflationParams struct {
	// Mean is the average annual claims inflation factor (1.0 = flat prices).
	Mean float64
	// Volatility is the sigma of the mean-1 lognormal noise on each year's
	// factor.
	Volatility float64
}

// RecoveryParams drives salvage (selling the insured vehicle's wreck) and
// subrogation (recovering the payout from an at-fault third party). Both
// attach only to own-damage claims that paid something, as money-in
// transactions dated after the close.
type RecoveryParams struct {
	Salvage     RecoveryTypeParams
	Subrogation RecoveryTypeParams
}

// RecoveryTypeParams parameterizes one recovery type.
type RecoveryTypeParams struct {
	// Probability is the chance an own-damage claim yields this recovery;
	// 0 switches the type off.
	Probability float64
	// MeanShare is the average recovery as a share of the claim's gross paid.
	MeanShare float64
	// Concentration is the Beta concentration of the share draw; higher
	// means shares cluster tighter around MeanShare.
	Concentration float64
	// LagMedianDays is the median days from close to receiving the money.
	LagMedianDays float64
	// LagSigma is the sigma of the lognormal close-to-receipt lag.
	LagSigma float64
}

// ReopeningParams parameterizes the single optional reopen episode.
type ReopeningParams struct {
	// Probability is the chance a closed claim reopens once; 0 switches
	// reopening off.
	Probability float64
	// EstimateFactor is the mean of the reopen case estimate as a factor of
	// the claim's original initial estimate; it may exceed 1.
	EstimateFactor float64
	// EstimateSigma is the sigma of the mean-1 lognormal noise on the
	// reopen estimate.
	EstimateSigma float64
	// LagMedianDays is the median days from first close to reopen.
	LagMedianDays float64
	// LagSigma is the sigma of the lognormal close-to-reopen lag.
	LagSigma float64
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
	// ThirdPartyShape and ThirdPartyMeanDays are the gamma parameters for
	// third-party (bodily-injury) claims, which settle far slower than own
	// damage; the size stretch does not apply to them.
	ThirdPartyShape    float64
	ThirdPartyMeanDays float64
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

type namedFloat struct {
	name string
	v    float64
}

// checkFinite rejects NaN and infinite values, which slip past ordinary range
// comparisons because every comparison with NaN is false.
func checkFinite(fields ...namedFloat) error {
	for _, f := range fields {
		if math.IsNaN(f.v) || math.IsInf(f.v, 0) {
			return fmt.Errorf("%s: must be a finite number, got %v", f.name, f.v)
		}
	}
	return nil
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
	if err := checkFinite(
		namedFloat{"book.growth_factor", b.GrowthFactor},
		namedFloat{"book.size_volatility", b.SizeVolatility},
		namedFloat{"book.spread", b.Spread},
		namedFloat{"book.sum_insured_median", b.SumInsuredMedian},
		namedFloat{"book.sum_insured_inflation", b.SumInsuredInflation},
		namedFloat{"book.premium_rate_factor", b.PremiumRateFactor},
	); err != nil {
		return err
	}
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
		if err := checkFinite(
			namedFloat{fmt.Sprintf("book.excess_choices[%d].value", i), c.Value},
			namedFloat{fmt.Sprintf("book.excess_choices[%d].weight", i), c.Weight},
		); err != nil {
			return err
		}
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
	if err := checkFinite(
		namedFloat{"claims.base_frequency", c.BaseFrequency},
		namedFloat{"claims.report_lag_median", c.ReportLagMedian},
		namedFloat{"claims.report_lag_sigma", c.ReportLagSigma},
		namedFloat{"claims.nil_probability", c.NilProbability},
	); err != nil {
		return err
	}
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
	if err := c.Inflation.validate(); err != nil {
		return err
	}
	if c.NilProbability < 0 || c.NilProbability >= 1 {
		return fmt.Errorf("claims.nil_probability: must be in [0, 1), got %v", c.NilProbability)
	}
	if err := c.Recoveries.Salvage.validate("claims.recoveries.salvage"); err != nil {
		return err
	}
	if err := c.Recoveries.Subrogation.validate("claims.recoveries.subrogation"); err != nil {
		return err
	}
	if err := c.Reopening.validate(); err != nil {
		return err
	}
	return c.CloseLag.validate()
}

func (i InflationParams) validate() error {
	if err := checkFinite(
		namedFloat{"claims.inflation.mean", i.Mean},
		namedFloat{"claims.inflation.volatility", i.Volatility},
	); err != nil {
		return err
	}
	if i.Mean <= 0 {
		return fmt.Errorf("claims.inflation.mean: must be positive, got %v", i.Mean)
	}
	if i.Volatility < 0 {
		return fmt.Errorf("claims.inflation.volatility: must not be negative, got %v", i.Volatility)
	}
	return nil
}

func (r RecoveryTypeParams) validate(prefix string) error {
	if err := checkFinite(
		namedFloat{prefix + ".probability", r.Probability},
		namedFloat{prefix + ".mean_share", r.MeanShare},
		namedFloat{prefix + ".concentration", r.Concentration},
		namedFloat{prefix + ".lag_median_days", r.LagMedianDays},
		namedFloat{prefix + ".lag_sigma", r.LagSigma},
	); err != nil {
		return err
	}
	if r.Probability < 0 || r.Probability >= 1 {
		return fmt.Errorf("%s.probability: must be in [0, 1), got %v", prefix, r.Probability)
	}
	if r.MeanShare <= 0 || r.MeanShare >= 1 {
		return fmt.Errorf("%s.mean_share: must be in (0, 1), got %v", prefix, r.MeanShare)
	}
	if r.Concentration <= 0 {
		return fmt.Errorf("%s.concentration: must be positive, got %v", prefix, r.Concentration)
	}
	if r.LagMedianDays <= 0 {
		return fmt.Errorf("%s.lag_median_days: must be positive, got %v", prefix, r.LagMedianDays)
	}
	if r.LagSigma < 0 {
		return fmt.Errorf("%s.lag_sigma: must not be negative, got %v", prefix, r.LagSigma)
	}
	return nil
}

func (r ReopeningParams) validate() error {
	if err := checkFinite(
		namedFloat{"claims.reopening.probability", r.Probability},
		namedFloat{"claims.reopening.estimate_factor", r.EstimateFactor},
		namedFloat{"claims.reopening.estimate_sigma", r.EstimateSigma},
		namedFloat{"claims.reopening.lag_median_days", r.LagMedianDays},
		namedFloat{"claims.reopening.lag_sigma", r.LagSigma},
	); err != nil {
		return err
	}
	if r.Probability < 0 || r.Probability >= 1 {
		return fmt.Errorf("claims.reopening.probability: must be in [0, 1), got %v", r.Probability)
	}
	if r.EstimateFactor <= 0 {
		return fmt.Errorf("claims.reopening.estimate_factor: must be positive, got %v", r.EstimateFactor)
	}
	if r.EstimateSigma < 0 {
		return fmt.Errorf("claims.reopening.estimate_sigma: must not be negative, got %v", r.EstimateSigma)
	}
	if r.LagMedianDays <= 0 {
		return fmt.Errorf("claims.reopening.lag_median_days: must be positive, got %v", r.LagMedianDays)
	}
	if r.LagSigma < 0 {
		return fmt.Errorf("claims.reopening.lag_sigma: must not be negative, got %v", r.LagSigma)
	}
	return nil
}

func (s SeverityParams) validate() error {
	if err := checkFinite(
		namedFloat{"claims.severity.third_party_weight", s.ThirdPartyWeight},
		namedFloat{"claims.severity.own_damage_median_fraction", s.OwnDamageMedianFraction},
		namedFloat{"claims.severity.own_damage_sigma", s.OwnDamageSigma},
		namedFloat{"claims.severity.third_party_scale", s.ThirdPartyScale},
		namedFloat{"claims.severity.third_party_alpha", s.ThirdPartyAlpha},
	); err != nil {
		return err
	}
	if s.ThirdPartyWeight < 0 || s.ThirdPartyWeight > 1 {
		return fmt.Errorf("claims.severity.third_party_weight: must be in [0, 1], got %v", s.ThirdPartyWeight)
	}
	if s.OwnDamageMedianFraction <= 0 {
		return fmt.Errorf("claims.severity.own_damage_median_fraction: must be positive, got %v", s.OwnDamageMedianFraction)
	}
	if s.OwnDamageSigma <= 0 {
		return fmt.Errorf("claims.severity.own_damage_sigma: must be positive, got %v", s.OwnDamageSigma)
	}
	if s.ThirdPartyScale <= 0 {
		return fmt.Errorf("claims.severity.third_party_scale: must be positive, got %v", s.ThirdPartyScale)
	}
	if s.ThirdPartyAlpha <= 1 {
		return fmt.Errorf("claims.severity.third_party_alpha: must exceed 1 for a finite mean, got %v", s.ThirdPartyAlpha)
	}
	return nil
}

func (c CloseLagParams) validate() error {
	if err := checkFinite(
		namedFloat{"claims.close_lag.shape", c.Shape},
		namedFloat{"claims.close_lag.mean_days", c.MeanDays},
		namedFloat{"claims.close_lag.size_threshold", c.SizeThreshold},
		namedFloat{"claims.close_lag.size_multiplier", c.SizeMultiplier},
		namedFloat{"claims.close_lag.risk_loading", c.RiskLoading},
		namedFloat{"claims.close_lag.third_party_shape", c.ThirdPartyShape},
		namedFloat{"claims.close_lag.third_party_mean_days", c.ThirdPartyMeanDays},
	); err != nil {
		return err
	}
	if c.Shape <= 0 {
		return fmt.Errorf("claims.close_lag.shape: must be positive, got %v", c.Shape)
	}
	if c.MeanDays <= 0 {
		return fmt.Errorf("claims.close_lag.mean_days: must be positive, got %v", c.MeanDays)
	}
	if c.SizeThreshold < 0 {
		return fmt.Errorf("claims.close_lag.size_threshold: must not be negative, got %v", c.SizeThreshold)
	}
	if c.SizeMultiplier < 1 {
		return fmt.Errorf("claims.close_lag.size_multiplier: must be at least 1, got %v", c.SizeMultiplier)
	}
	if c.RiskLoading < 0 {
		return fmt.Errorf("claims.close_lag.risk_loading: must not be negative, got %v", c.RiskLoading)
	}
	if c.ThirdPartyShape <= 0 {
		return fmt.Errorf("claims.close_lag.third_party_shape: must be positive, got %v", c.ThirdPartyShape)
	}
	if c.ThirdPartyMeanDays <= 0 {
		return fmt.Errorf("claims.close_lag.third_party_mean_days: must be positive, got %v", c.ThirdPartyMeanDays)
	}
	return nil
}

func (r RunoffParams) validate() error {
	if err := checkFinite(
		namedFloat{"runoff.case_adequacy_mean", r.CaseAdequacyMean},
		namedFloat{"runoff.case_adequacy_sigma", r.CaseAdequacySigma},
		namedFloat{"runoff.payments_per_year", r.PaymentsPerYear},
		namedFloat{"runoff.settlement_share", r.SettlementShare},
		namedFloat{"runoff.concentration", r.Concentration},
		namedFloat{"runoff.revisions_per_year", r.RevisionsPerYear},
		namedFloat{"runoff.revision_sigma", r.RevisionSigma},
	); err != nil {
		return err
	}
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
