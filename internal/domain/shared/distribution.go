package shared

// MeanOneLogNormal draws from a lognormal distribution with mean exactly 1;
// a sigma of 0 or less degenerates to the constant 1 with no draw.
func MeanOneLogNormal(src RandomSource, sigma float64) float64 {
	if sigma <= 0 {
		return 1
	}
	return src.LogNormal(-sigma*sigma/2, sigma)
}
