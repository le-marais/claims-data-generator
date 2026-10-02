package lob

import "math"

// lognormalPareto is the spliced loss of the LognormalPareto kind: a
// lognormal body truncated at scale, and a Pareto tail from scale up, joined
// so the density is continuous at scale.
type lognormalPareto struct {
	mu, sigma, scale, alpha float64
	// bodyMass is the body lognormal's probability below scale.
	bodyMass float64
	// tail is the share of losses above scale, which continuity sets.
	tail float64
}

// newLognormalPareto builds the spliced loss from the body lognormal's
// median and sigma, and the tail's minimum, scale, and index, alpha. With z
// the standardised log of scale, continuity at scale needs
// (1 - tail) phi(z) / (sigma Phi(z)) = tail alpha, which sets the tail share.
// It depends on scale only through scale / median, so scaling every dollar
// amount by a factor scales the loss and leaves the tail share alone.
func newLognormalPareto(median, sigma, scale, alpha float64) lognormalPareto {
	mu := math.Log(median)
	z := (math.Log(scale) - mu) / sigma
	bodyMass := normCDF(z)
	r := math.Exp(-z*z/2) / math.Sqrt(2*math.Pi) / (sigma * alpha * bodyMass)
	return lognormalPareto{mu: mu, sigma: sigma, scale: scale, alpha: alpha, bodyMass: bodyMass, tail: r / (1 + r)}
}

// quantile is the loss at cumulative probability u in [0, 1): the body's
// quantile below the tail share, the tail's above it.
func (d lognormalPareto) quantile(u float64) float64 {
	if u < 1-d.tail {
		p := u / (1 - d.tail) * d.bodyMass
		return math.Exp(d.mu + d.sigma*math.Sqrt2*math.Erfinv(2*p-1))
	}
	return d.scale * math.Pow((1-u)/d.tail, -1/d.alpha)
}

// stopLoss is E[(X - t)+] for t >= 0. From scale up it is the tail's expected
// excess. Below scale it adds the integral of the survival function from t to
// scale, which the body lognormal's CDF and partial moments give in closed
// form.
func (d lognormalPareto) stopLoss(t float64) float64 {
	tailExcess := func(t float64) float64 {
		return d.tail * math.Pow(d.scale, d.alpha) * math.Pow(t, 1-d.alpha) / (d.alpha - 1)
	}
	if t >= d.scale {
		return tailExcess(t)
	}
	// partial is E[X; X <= a] for the body lognormal.
	partial := func(a float64) float64 {
		return math.Exp(d.mu+d.sigma*d.sigma/2) * normCDF((math.Log(a)-d.mu-d.sigma*d.sigma)/d.sigma)
	}
	tCDF := 0.0
	if t > 0 {
		tCDF = t * normCDF((math.Log(t)-d.mu)/d.sigma)
	}
	// The integral of the body CDF from t to scale.
	integral := d.scale*d.bodyMass - tCDF - (partial(d.scale) - partial(t))
	return (d.scale - t) - (1-d.tail)/d.bodyMass*integral + tailExcess(d.scale)
}

// LognormalParetoLoss is the LognormalPareto loss at cumulative probability
// u in [0, 1), in start-year dollars, so one uniform draw gives one loss.
func (s SeverityParams) LognormalParetoLoss(u float64) float64 {
	return newLognormalPareto(s.Median, s.Sigma, s.Scale, s.Alpha).quantile(u)
}
