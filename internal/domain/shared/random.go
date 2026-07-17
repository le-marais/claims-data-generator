package shared

// RandomSource is the domain's view of randomness. Implementations live in
// infrastructure. Split derives an independent, reproducible sub-stream so
// that adding a consumer never reshuffles the draws of existing ones.
type RandomSource interface {
	Split(label string) RandomSource
	Uniform() float64 // in [0, 1)
	Bernoulli(p float64) bool
	Poisson(mean float64) int
	LogNormal(mu, sigma float64) float64
	Gamma(shape, scale float64) float64
	Pareto(xm, alpha float64) float64
	Beta(alpha, beta float64) float64
}
