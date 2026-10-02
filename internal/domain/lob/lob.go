// Package lob defines the LineOfBusiness value object: the complete
// parameter set that makes the simulation engine reusable across classes
// of business.
package lob

import (
	"fmt"
	"math"
)

type LineOfBusiness struct {
	Name    string
	Book    BookParams
	Pricing PricingParams
	Claims  ClaimParams
	Runoff  RunoffParams
}

// BookParams drives the policy book simulation.
type BookParams struct {
	// GrowthFactor is the year-on-year trend in the number of fleets written,
	// which without a fleet book is the policy count; each year's book size is
	// previous size x GrowthFactor x lognormal noise.
	GrowthFactor float64
	// SizeVolatility is the sigma of the mean-1 lognormal noise on book size.
	SizeVolatility float64
	// Spread is the heterogeneity knob: sigma of the sum insured lognormal
	// and the standard deviation of the mean-1 risk factor gamma. With a
	// fleet book it is the heterogeneity of the vehicles within a fleet.
	Spread float64
	// SumInsuredMedian is the year-1 median sum insured in dollars.
	SumInsuredMedian float64
	// SumInsuredInflation is the annual multiplicative drift of the median.
	SumInsuredInflation float64
	// ExcessChoices is the discrete set of available excesses with weights.
	ExcessChoices []ExcessChoice
	// Fleet switches on a fleet book, in which each policy is one vehicle on
	// a fleet. The zero value writes every policy as its own fleet.
	Fleet FleetParams
}

// FleetParams drives a fleet book: fleets are written first, each with its
// own size, level of vehicle value and risk, and then each vehicle on a fleet
// is a policy drawn around the fleet's values. A fleet is one contract, so
// its vehicles share its cover dates and excess. A Size.Median of 0 switches
// fleets off, and the other fields are then not required.
type FleetParams struct {
	// Size is the lognormal number of vehicles on a fleet, rounded to a whole
	// number and at least 1.
	Size FleetSizeParams
	// SumInsuredSigma is the lognormal sigma of a fleet's median vehicle sum
	// insured around the book's SumInsuredMedian: how far fleets' vehicle
	// values differ, a courier's vans against a haulier's tractors.
	SumInsuredSigma float64
	// RiskSpread is the standard deviation of the mean-one gamma fleet risk
	// factor, which scales the risk factor of every vehicle on the fleet:
	// how far operators differ in drivers, routes and safety. 0 gives every
	// fleet a factor of 1.
	RiskSpread float64
}

// FleetSizeParams is the lognormal number of vehicles on a fleet.
type FleetSizeParams struct {
	Median float64
	Sigma  float64
}

// Enabled reports whether the book is written as fleets.
func (f FleetParams) Enabled() bool { return f.Size.Median > 0 }

// ExpectedSize is about the mean number of vehicles on a fleet: the
// lognormal mean, ignoring the rounding to whole vehicles, and 1 with fleets
// off.
func (f FleetParams) ExpectedSize() float64 {
	if !f.Enabled() {
		return 1
	}
	return math.Max(1, f.Size.Median*math.Exp(f.Size.Sigma*f.Size.Sigma/2))
}

type ExcessChoice struct {
	Value  float64
	Weight float64
}

// PricingParams drives premium pricing: the insurer's assumed loss cost,
// independent of the claims model. Each policy's premium is its assumed
// expected ultimate loss (ExpectedPolicyLoss) divided by TargetLossRatio.
// The target sets premium, never experience: the realized loss ratio emerges
// from the claims model. With these assumptions equal to the true claims
// values it lands around the target, moved by claim sampling and the
// simulated inflation path; deviating them models underpricing or adverse
// experience.
type PricingParams struct {
	// TargetLossRatio is the assumed loss ratio premium is priced to.
	TargetLossRatio float64
	// AdequacyVolatility is the sigma of mean-one lognormal noise on each
	// underwriting year's target loss ratio: how far a year's rates miss the
	// target, as in an underwriting cycle. Every policy written in the year
	// is priced to the same noisy target, so cohorts scatter around
	// TargetLossRatio while the expected loss ratio stays on it. 0 switches
	// it off.
	AdequacyVolatility float64
	// Sections is the assumed frequency and severity of each section of
	// cover, matching the claims sections by name and order.
	Sections []PricingSectionParams
	// NilProbability is the assumed share of reported claims that close
	// without payment. A nil claim still pays if it reopens.
	NilProbability float64
	// ReopenProbability and ReopenEstimateFactor are the assumed reopen uplift
	// inputs: expected extra development is ReopenProbability * ReopenEstimateFactor.
	ReopenProbability    float64
	ReopenEstimateFactor float64
	// InflationMean is the assumed mean annual claims-inflation trend. Each
	// policy's assumed loss cost is trended to the midpoint of its cover.
	InflationMean float64
}

// PricingSectionParams is the insurer's assumed loss cost for one section.
type PricingSectionParams struct {
	// Name matches the claims section the assumption prices.
	Name string
	// BaseFrequency is the assumed ground-up occurrence frequency per
	// policy-year at risk factor 1; 0 prices the section at nothing.
	BaseFrequency float64
	// Severity is the assumed ground-up loss distribution.
	Severity SeverityParams
	// Limit is the assumed most the policy pays on one claim, in nominal
	// dollars; 0 is unlimited. See SectionParams.Limit.
	Limit float64
	// NoExcess prices the section as taking no excess. See
	// SectionParams.NoExcess.
	NoExcess bool
}

// ClaimParams drives claim event simulation.
type ClaimParams struct {
	// Sections are the policy's sections of cover, such as own damage and
	// third-party liability. Each draws its own claims, with its own
	// frequency, severity, report lag and close lag.
	Sections []SectionParams
	// Inflation is the stochastic claims-inflation path applied by
	// occurrence date to every claim's ground-up loss.
	Inflation InflationParams
	// NilProbability is the chance a reported claim closes without payment;
	// 0 switches nil claims off.
	NilProbability float64
	// Recoveries drives salvage and subrogation: money coming back after
	// close on claims in sections that allow recoveries.
	Recoveries RecoveryParams
	// Reopening drives the single optional reopen episode: a closed claim
	// can reopen once, develop further, and close again.
	Reopening ReopeningParams
}

// SectionParams is one section of cover: the claims it produces and how
// they report and settle.
type SectionParams struct {
	// Name identifies the section; it labels the section's random sub-stream
	// and matches its pricing assumption.
	Name string
	// BaseFrequency is the ground-up occurrence frequency per policy-year at
	// risk factor 1. With a non-zero excess the realized reported frequency
	// is lower, because sub-excess claims are discarded rather than reported,
	// unless the section takes no excess (NoExcess).
	// 0 switches the section off.
	BaseFrequency float64
	Severity      SeverityParams
	// Limit is the most the policy pays on one claim over its whole life,
	// reopen included, in nominal dollars. It is a contract term, so claims
	// inflation does not trend it and erodes it over the years. 0 is
	// unlimited. A sum-insured severity is already limited by its sum
	// insured, so it must leave Limit at 0.
	Limit float64
	// NoExcess makes the section take no excess off a claim: every ground-up
	// loss is reported and paid from the first dollar, as liability cover
	// usually is. The zero value applies the policy's excess.
	NoExcess  bool
	ReportLag ReportLagParams
	CloseLag  CloseLagParams
	// Recoveries makes the section's claims eligible for salvage and
	// subrogation. Salvage further needs a total loss on a sum-insured
	// severity: a liability claim settled at its Limit leaves no wreck to
	// sell.
	Recoveries bool
}

// SeverityKind names a ground-up loss distribution.
type SeverityKind string

const (
	// SumInsuredLognormal is a lognormal fraction of the policy's sum insured
	// in start-year dollars, capped at the sum insured: a claim that reaches
	// the cap is a total loss.
	SumInsuredLognormal SeverityKind = "sum_insured_lognormal"
	// Pareto is a Pareto loss in start-year dollars with no cap of its own,
	// for liability; a section Limit caps what the policy pays.
	Pareto SeverityKind = "pareto"
	// Lognormal is a lognormal loss in start-year dollars with no cap of its
	// own, for claims sized independently of the insured's own cover, such
	// as third-party property damage; a section Limit caps what the policy
	// pays.
	Lognormal SeverityKind = "lognormal"
	// LognormalPareto is a lognormal body truncated at a threshold and a
	// Pareto tail above it, in start-year dollars, for liability claims with
	// many small losses and a heavy tail, such as bodily injury. The tail's
	// share of losses keeps the density continuous at the threshold. Like
	// Pareto, it has no cap of its own.
	LognormalPareto SeverityKind = "lognormal_pareto"
)

// SeverityParams is a section's ground-up loss distribution. Kind selects
// which of the other fields apply.
type SeverityParams struct {
	Kind SeverityKind
	// MedianFraction and Sigma parameterize SumInsuredLognormal: the median
	// loss as a fraction of sum insured, and the lognormal sigma. Sigma is
	// shared with Lognormal.
	MedianFraction float64
	Sigma          float64
	// Median and Sigma parameterize Lognormal: the median loss in start-year
	// dollars, and the lognormal sigma.
	Median float64
	// Scale and Alpha parameterize Pareto: the minimum loss in dollars, and
	// the tail index, which must exceed 1 for a finite mean.
	Scale float64
	Alpha float64
	// LognormalPareto reads all four: Median and Sigma for its body
	// lognormal, Scale for the threshold where its tail starts, and Alpha for
	// the tail index.
}

// ReportLagParams is the lognormal occurrence-to-report lag in days.
type ReportLagParams struct {
	Median float64
	Sigma  float64
}

// CloseLagParams is the gamma report-to-close lag. A claim costing s in
// start-year dollars has mean lag MeanDays x (s / SizeReference)^SizeElasticity
// x riskFactor^RiskLoading, so larger and riskier claims settle slower,
// smoothly. Costs are deflated by the claims inflation index first, so
// inflation does not slow settlement year on year.
type CloseLagParams struct {
	// Shape is the gamma shape; above 1 avoids mass at near-zero delays.
	Shape float64
	// MeanDays is the mean lag of a claim costing SizeReference.
	MeanDays float64
	// SizeReference is the claim cost, in start-year dollars, that settles
	// in MeanDays on average.
	SizeReference float64
	// SizeElasticity links settlement time to size; 0 switches the link off.
	SizeElasticity float64
	// RiskLoading is the exponent applied to the policy risk factor.
	RiskLoading float64
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
// attach only to paid claims in sections that allow recoveries, as money-in
// transactions dated after the close, and salvage only to total losses.
type RecoveryParams struct {
	Salvage     RecoveryTypeParams
	Subrogation RecoveryTypeParams
}

// RecoveryTypeParams parameterizes one recovery type.
type RecoveryTypeParams struct {
	// Probability is the chance an eligible claim yields this recovery: a
	// paid total loss for salvage, any paid claim in a section that allows
	// recoveries for subrogation. 0 switches the type off.
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
	// EstimateFactor is the mean additional cost of the reopen episode as a
	// factor of the claim's ultimate; it may exceed 1. Reopens on a
	// sum-insured or limited section are capped at the cover the claim has
	// left.
	EstimateFactor float64
	// EstimateSigma is the sigma of the mean-1 lognormal noise on the
	// reopen's additional cost.
	EstimateSigma float64
	// LagMedianDays is the median days from first close to reopen.
	LagMedianDays float64
	// LagSigma is the sigma of the lognormal close-to-reopen lag.
	LagSigma float64
}

// RunoffParams drives the case estimate path and payments.
type RunoffParams struct {
	// CaseAdequacyMean is the true ultimate over the expected opening case
	// estimate: above 1 cases open deficient (under-reserving), below 1
	// redundant. Each later revision aims at the true remaining cost times
	// CaseAdequacyMean^(u-1) at elapsed share u of the episode, so the bias
	// decays to parity at close rather than vanishing at the first revision.
	// It moves case reserves only, never the loss cost.
	CaseAdequacyMean float64
	// CaseAdequacySigma is the sigma of the mean-one lognormal noise on each
	// opening case estimate: how wrong individual estimates are.
	CaseAdequacySigma float64
	// PaymentsPerYear is the Poisson intensity of interim payments over the
	// claim's open duration.
	PaymentsPerYear float64
	// SettlementShare is the mean fraction of ultimate an episode with
	// interim payments reserves for its final settlement payment at close.
	SettlementShare float64
	// SettlementConcentration is the Beta concentration of each such
	// episode's settlement share, drawn around SettlementShare; higher keeps
	// the share closer to it. 0 fixes the share at SettlementShare.
	SettlementConcentration float64
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
	if err := l.Pricing.validate(); err != nil {
		return err
	}
	if err := l.Claims.validate(); err != nil {
		return err
	}
	if err := l.checkPricingSections(); err != nil {
		return err
	}
	return l.Runoff.validate()
}

// checkPricingSections requires the pricing assumptions to cover the claims
// sections one for one, by name and in order, so each section's premium is
// priced on its own assumption.
func (l LineOfBusiness) checkPricingSections() error {
	if len(l.Pricing.Sections) != len(l.Claims.Sections) {
		return fmt.Errorf("pricing.sections: must list the %d claims sections, got %d", len(l.Claims.Sections), len(l.Pricing.Sections))
	}
	for i, ps := range l.Pricing.Sections {
		if want := l.Claims.Sections[i].Name; ps.Name != want {
			return fmt.Errorf("pricing.sections[%d].name: must match claims.sections[%d].name %q, got %q", i, i, want, ps.Name)
		}
	}
	return nil
}

func (b BookParams) validate() error {
	if err := checkFinite(
		namedFloat{"book.growth_factor", b.GrowthFactor},
		namedFloat{"book.size_volatility", b.SizeVolatility},
		namedFloat{"book.spread", b.Spread},
		namedFloat{"book.sum_insured_median", b.SumInsuredMedian},
		namedFloat{"book.sum_insured_inflation", b.SumInsuredInflation},
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
	return b.Fleet.validate()
}

func (f FleetParams) validate() error {
	if err := checkFinite(
		namedFloat{"book.fleet.size.median", f.Size.Median},
		namedFloat{"book.fleet.size.sigma", f.Size.Sigma},
		namedFloat{"book.fleet.sum_insured_sigma", f.SumInsuredSigma},
		namedFloat{"book.fleet.risk_spread", f.RiskSpread},
	); err != nil {
		return err
	}
	if f.Size.Median < 0 {
		return fmt.Errorf("book.fleet.size.median: must not be negative, got %v", f.Size.Median)
	}
	if f.Size.Median == 0 {
		return nil // switched off: the rest of the block is never read
	}
	if f.Size.Median < 1 {
		return fmt.Errorf("book.fleet.size.median: must be 0 (no fleets) or at least 1, got %v", f.Size.Median)
	}
	if f.Size.Sigma < 0 {
		return fmt.Errorf("book.fleet.size.sigma: must not be negative, got %v", f.Size.Sigma)
	}
	if f.SumInsuredSigma < 0 {
		return fmt.Errorf("book.fleet.sum_insured_sigma: must not be negative, got %v", f.SumInsuredSigma)
	}
	if f.RiskSpread < 0 {
		return fmt.Errorf("book.fleet.risk_spread: must not be negative, got %v", f.RiskSpread)
	}
	return nil
}

func (p PricingParams) validate() error {
	if err := checkFinite(
		namedFloat{"pricing.target_loss_ratio", p.TargetLossRatio},
		namedFloat{"pricing.adequacy_volatility", p.AdequacyVolatility},
		namedFloat{"pricing.nil_probability", p.NilProbability},
		namedFloat{"pricing.reopen_probability", p.ReopenProbability},
		namedFloat{"pricing.reopen_estimate_factor", p.ReopenEstimateFactor},
		namedFloat{"pricing.inflation_mean", p.InflationMean},
	); err != nil {
		return err
	}
	if p.TargetLossRatio <= 0 {
		return fmt.Errorf("pricing.target_loss_ratio: must be positive, got %v", p.TargetLossRatio)
	}
	if p.AdequacyVolatility < 0 {
		return fmt.Errorf("pricing.adequacy_volatility: must not be negative, got %v", p.AdequacyVolatility)
	}
	for i, sec := range p.Sections {
		if err := sec.validate(fmt.Sprintf("pricing.sections[%d]", i)); err != nil {
			return err
		}
	}
	if p.NilProbability < 0 || p.NilProbability >= 1 {
		return fmt.Errorf("pricing.nil_probability: must be in [0, 1), got %v", p.NilProbability)
	}
	if p.ReopenProbability < 0 || p.ReopenProbability >= 1 {
		return fmt.Errorf("pricing.reopen_probability: must be in [0, 1), got %v", p.ReopenProbability)
	}
	if p.ReopenProbability > 0 && p.ReopenEstimateFactor <= 0 {
		return fmt.Errorf("pricing.reopen_estimate_factor: must be positive, got %v", p.ReopenEstimateFactor)
	}
	if p.InflationMean <= 0 {
		return fmt.Errorf("pricing.inflation_mean: must be positive, got %v", p.InflationMean)
	}
	return nil
}

func (s PricingSectionParams) validate(prefix string) error {
	if err := checkFinite(namedFloat{prefix + ".base_frequency", s.BaseFrequency}); err != nil {
		return err
	}
	if s.BaseFrequency < 0 {
		return fmt.Errorf("%s.base_frequency: must not be negative, got %v", prefix, s.BaseFrequency)
	}
	if s.BaseFrequency == 0 {
		return nil // priced at nothing: the severity is never read
	}
	if err := s.Severity.validate(prefix + ".severity"); err != nil {
		return err
	}
	return validateLimit(prefix, s.Limit, s.Severity.Kind)
}

// validateLimit checks a section's per-claim limit: finite, not negative, and
// 0 on a sum-insured severity, whose limit is already its sum insured.
func validateLimit(prefix string, limit float64, kind SeverityKind) error {
	if err := checkFinite(namedFloat{prefix + ".limit", limit}); err != nil {
		return err
	}
	if limit < 0 {
		return fmt.Errorf("%s.limit: must not be negative, got %v", prefix, limit)
	}
	// A limit below one cent rounds to a cover limit of 0, which is unlimited.
	if limit != 0 && limit < 0.01 {
		return fmt.Errorf("%s.limit: must be 0 (unlimited) or at least 0.01, got %v", prefix, limit)
	}
	if limit != 0 && kind == SumInsuredLognormal {
		return fmt.Errorf("%s.limit: must be 0 on a sum_insured_lognormal section, whose limit is its sum insured, got %v", prefix, limit)
	}
	return nil
}

func (c ClaimParams) validate() error {
	if err := checkFinite(namedFloat{"claims.nil_probability", c.NilProbability}); err != nil {
		return err
	}
	if len(c.Sections) == 0 {
		return fmt.Errorf("claims.sections: must not be empty")
	}
	names := map[string]bool{}
	active := 0
	for i, sec := range c.Sections {
		prefix := fmt.Sprintf("claims.sections[%d]", i)
		if sec.Name == "" {
			return fmt.Errorf("%s.name: must not be empty", prefix)
		}
		if names[sec.Name] {
			return fmt.Errorf("%s.name: %q is already used by another section", prefix, sec.Name)
		}
		names[sec.Name] = true
		if err := sec.validate(prefix); err != nil {
			return err
		}
		if sec.BaseFrequency > 0 {
			active++
		}
	}
	if active == 0 {
		return fmt.Errorf("claims.sections: at least one section must have a positive base_frequency")
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
	return c.Reopening.validate()
}

// validate checks one claims section. A section with a base frequency of 0
// draws no claims, so the rest of it is not required.
func (s SectionParams) validate(prefix string) error {
	if err := checkFinite(namedFloat{prefix + ".base_frequency", s.BaseFrequency}); err != nil {
		return err
	}
	if s.BaseFrequency < 0 {
		return fmt.Errorf("%s.base_frequency: must not be negative, got %v", prefix, s.BaseFrequency)
	}
	if s.BaseFrequency == 0 {
		return nil
	}
	if err := s.Severity.validate(prefix + ".severity"); err != nil {
		return err
	}
	if err := validateLimit(prefix, s.Limit, s.Severity.Kind); err != nil {
		return err
	}
	if err := s.ReportLag.validate(prefix + ".report_lag"); err != nil {
		return err
	}
	return s.CloseLag.validate(prefix + ".close_lag")
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
	if r.Probability == 0 {
		return nil // switched off: the rest of the block is never read
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
	if r.Probability == 0 {
		return nil // switched off: the rest of the block is never read
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

func (s SeverityParams) validate(prefix string) error {
	if err := checkFinite(
		namedFloat{prefix + ".median_fraction", s.MedianFraction},
		namedFloat{prefix + ".sigma", s.Sigma},
		namedFloat{prefix + ".median", s.Median},
		namedFloat{prefix + ".scale", s.Scale},
		namedFloat{prefix + ".alpha", s.Alpha},
	); err != nil {
		return err
	}
	switch s.Kind {
	case SumInsuredLognormal:
		if s.MedianFraction <= 0 {
			return fmt.Errorf("%s.median_fraction: must be positive, got %v", prefix, s.MedianFraction)
		}
		if s.Sigma <= 0 {
			return fmt.Errorf("%s.sigma: must be positive, got %v", prefix, s.Sigma)
		}
	case Pareto:
		if s.Scale <= 0 {
			return fmt.Errorf("%s.scale: must be positive, got %v", prefix, s.Scale)
		}
		if s.Alpha <= 1 {
			return fmt.Errorf("%s.alpha: must exceed 1 for a finite mean, got %v", prefix, s.Alpha)
		}
	case Lognormal:
		if s.Median <= 0 {
			return fmt.Errorf("%s.median: must be positive, got %v", prefix, s.Median)
		}
		if s.Sigma <= 0 {
			return fmt.Errorf("%s.sigma: must be positive, got %v", prefix, s.Sigma)
		}
	case LognormalPareto:
		if s.Median <= 0 {
			return fmt.Errorf("%s.median: must be positive, got %v", prefix, s.Median)
		}
		if s.Sigma <= 0 {
			return fmt.Errorf("%s.sigma: must be positive, got %v", prefix, s.Sigma)
		}
		if s.Scale <= 0 {
			return fmt.Errorf("%s.scale: must be positive, got %v", prefix, s.Scale)
		}
		if s.Alpha <= 1 {
			return fmt.Errorf("%s.alpha: must exceed 1 for a finite mean, got %v", prefix, s.Alpha)
		}
	default:
		return fmt.Errorf("%s.kind: must be %q, %q, %q or %q, got %q", prefix, SumInsuredLognormal, Pareto, Lognormal, LognormalPareto, s.Kind)
	}
	return nil
}

func (r ReportLagParams) validate(prefix string) error {
	if err := checkFinite(
		namedFloat{prefix + ".median", r.Median},
		namedFloat{prefix + ".sigma", r.Sigma},
	); err != nil {
		return err
	}
	if r.Median <= 0 {
		return fmt.Errorf("%s.median: must be positive, got %v", prefix, r.Median)
	}
	if r.Sigma <= 0 {
		return fmt.Errorf("%s.sigma: must be positive, got %v", prefix, r.Sigma)
	}
	return nil
}

func (c CloseLagParams) validate(prefix string) error {
	if err := checkFinite(
		namedFloat{prefix + ".shape", c.Shape},
		namedFloat{prefix + ".mean_days", c.MeanDays},
		namedFloat{prefix + ".size_reference", c.SizeReference},
		namedFloat{prefix + ".size_elasticity", c.SizeElasticity},
		namedFloat{prefix + ".risk_loading", c.RiskLoading},
	); err != nil {
		return err
	}
	if c.Shape <= 0 {
		return fmt.Errorf("%s.shape: must be positive, got %v", prefix, c.Shape)
	}
	if c.MeanDays <= 0 {
		return fmt.Errorf("%s.mean_days: must be positive, got %v", prefix, c.MeanDays)
	}
	if c.SizeElasticity < 0 {
		return fmt.Errorf("%s.size_elasticity: must not be negative, got %v", prefix, c.SizeElasticity)
	}
	if c.SizeElasticity > 0 && c.SizeReference <= 0 {
		return fmt.Errorf("%s.size_reference: must be positive, got %v", prefix, c.SizeReference)
	}
	if c.RiskLoading < 0 {
		return fmt.Errorf("%s.risk_loading: must not be negative, got %v", prefix, c.RiskLoading)
	}
	return nil
}

func (r RunoffParams) validate() error {
	if err := checkFinite(
		namedFloat{"runoff.case_adequacy_mean", r.CaseAdequacyMean},
		namedFloat{"runoff.case_adequacy_sigma", r.CaseAdequacySigma},
		namedFloat{"runoff.payments_per_year", r.PaymentsPerYear},
		namedFloat{"runoff.settlement_share", r.SettlementShare},
		namedFloat{"runoff.settlement_concentration", r.SettlementConcentration},
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
	if r.SettlementConcentration < 0 {
		return fmt.Errorf("runoff.settlement_concentration: must not be negative, got %v", r.SettlementConcentration)
	}
	if r.SettlementConcentration > 0 && r.SettlementShare == 1 {
		return fmt.Errorf("runoff.settlement_share: must be below 1 when settlement_concentration is above 0, got %v", r.SettlementShare)
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
