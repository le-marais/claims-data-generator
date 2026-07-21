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
// for one policy at the given claims-inflation factor (Inflation.Mean raised to
// the policy's year offset). It mirrors the severity model in the claim package
// so premium can be priced to a target loss ratio. It draws no randomness, so
// pricing never perturbs a sub-stream. Recoveries are excluded (gross basis).
func (c ClaimParams) ExpectedPolicyLoss(sumInsured, excess, riskFactor, inflationFactor float64) float64 {
	s := c.Severity
	odMedian := inflationFactor * sumInsured * s.OwnDamageMedianFraction
	od := stopLossLognormal(odMedian, s.OwnDamageSigma, excess)
	tpScale := inflationFactor * s.ThirdPartyScale
	tp := stopLossPareto(tpScale, s.ThirdPartyAlpha, excess)
	perClaim := s.ThirdPartyWeight*tp + (1-s.ThirdPartyWeight)*od
	reopenUplift := 1 + c.Reopening.Probability*c.Reopening.EstimateFactor
	return c.BaseFrequency * riskFactor * perClaim * reopenUplift
}
