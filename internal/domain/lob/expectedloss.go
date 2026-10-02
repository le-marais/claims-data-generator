package lob

import "math"

// normCDF is the standard normal cumulative distribution function.
func normCDF(x float64) float64 {
	return 0.5 * math.Erfc(-x/math.Sqrt2)
}

// stopLossLognormal returns E[(X-excess)+] for X lognormal with the given
// median and sigma (ln X ~ Normal(ln median, sigma^2)). This is the standard
// undiscounted stop-loss form with the forward equal to the mean.
func stopLossLognormal(median, sigma, excess float64) float64 {
	mean := median * math.Exp(sigma*sigma/2)
	if excess <= 0 {
		return mean - excess
	}
	d1 := (math.Log(mean/excess) + sigma*sigma/2) / sigma
	d2 := d1 - sigma
	return mean*normCDF(d1) - excess*normCDF(d2)
}

// limitedStopLossLognormal returns E[(min(X, cap) - excess)+] for X lognormal
// with the given median and sigma: the expected excess-of-excess cost when
// losses are also capped at cap. For excess < cap it is the difference of two
// stop-loss layers; cap <= excess yields 0.
func limitedStopLossLognormal(median, sigma, excess, cap float64) float64 {
	if cap <= excess {
		return 0
	}
	return stopLossLognormal(median, sigma, excess) - stopLossLognormal(median, sigma, cap)
}

// stopLossPareto returns E[(X-excess)+] for X Pareto with the given scale
// (minimum) and alpha > 1. Below the minimum every loss exceeds the excess;
// above it, the closed-form tail integral applies.
func stopLossPareto(scale, alpha, excess float64) float64 {
	mean := scale * alpha / (alpha - 1)
	if excess <= scale {
		return mean - excess
	}
	return (scale / (alpha - 1)) * math.Pow(scale/excess, alpha-1)
}

// ExpectedPolicyLoss is the deterministic expected ultimate gross incurred loss
// for one policy under the pricing assumptions: the sum of its sections (see
// ExpectedSectionLoss). It draws no randomness, so pricing never perturbs a
// sub-stream. Recoveries are excluded (gross basis).
func (p PricingParams) ExpectedPolicyLoss(sumInsured, excess, riskFactor, inflationFactor, siDrift float64) float64 {
	total := 0.0
	for i := range p.Sections {
		total += p.ExpectedSectionLoss(i, sumInsured, excess, riskFactor, inflationFactor, siDrift)
	}
	return total
}

// ExpectedSectionLoss is the expected loss of one section of the policy, the
// section at index section of Sections. inflationFactor is the assumed claims
// index at the midpoint of the policy's cover.
//
// A sum-insured severity is expressed in base-year sum-insured terms (baseSI =
// sumInsured / siDrift), trended by the claims index only, and capped at the
// drifted sumInsured (a total loss). A Pareto, Lognormal or LognormalPareto
// severity keeps the claims index on its dollar amounts. With a Limit L its cost per claim is E[min((X-d)+, L)] =
// stopLoss(d) - stopLoss(d+L) for excess d; the limit is nominal, so it is not
// trended. With no limit it is the uncapped stop-loss. A section that takes
// no excess prices at d = 0.
//
// Each claim pays its cost unless it is nil, and pays a further
// ReopenEstimateFactor of that cost if it reopens, nil or not, so the expected
// payout per claim is the cost times 1 - NilProbability + ReopenProbability *
// ReopenEstimateFactor. The reopen term ignores the cap that holds a reopen
// within the cover left, on a sum-insured or a limited section alike, so
// claims near their limit are slightly overpriced.
func (p PricingParams) ExpectedSectionLoss(section int, sumInsured, excess, riskFactor, inflationFactor, siDrift float64) float64 {
	sec := p.Sections[section]
	// A section priced at no frequency is skipped rather than multiplied by
	// zero: its severity need not be valid (see PricingSectionParams.validate),
	// and zero times an infinite or NaN layer cost is NaN.
	if sec.BaseFrequency <= 0 {
		return 0
	}
	if sec.NoExcess {
		excess = 0
	}
	payout := 1 - p.NilProbability + p.ReopenProbability*p.ReopenEstimateFactor
	perClaim := sec.BaseFrequency * riskFactor * payout
	sev := sec.Severity
	switch sev.Kind {
	case SumInsuredLognormal:
		median := inflationFactor * sumInsured / siDrift * sev.MedianFraction
		return perClaim * limitedStopLossLognormal(median, sev.Sigma, excess, sumInsured)
	case Pareto:
		scale := inflationFactor * sev.Scale
		cost := stopLossPareto(scale, sev.Alpha, excess)
		if sec.Limit > 0 {
			cost -= stopLossPareto(scale, sev.Alpha, excess+sec.Limit)
		}
		return perClaim * cost
	case Lognormal:
		median := inflationFactor * sev.Median
		cost := stopLossLognormal(median, sev.Sigma, excess)
		if sec.Limit > 0 {
			cost -= stopLossLognormal(median, sev.Sigma, excess+sec.Limit)
		}
		return perClaim * cost
	case LognormalPareto:
		d := newLognormalPareto(inflationFactor*sev.Median, sev.Sigma, inflationFactor*sev.Scale, sev.Alpha)
		cost := d.stopLoss(excess)
		if sec.Limit > 0 {
			cost -= d.stopLoss(excess + sec.Limit)
		}
		return perClaim * cost
	}
	return 0
}
