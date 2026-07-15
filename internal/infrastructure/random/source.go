// Package random provides the gonum-backed implementation of the domain's
// RandomSource. Sub-streams are derived by hashing the parent stream's key
// with the split label, so a stream's draws depend only on the master seed
// and its label path - never on how much any ancestor has drawn.
package random

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand/v2"

	"gonum.org/v1/gonum/stat/distuv"

	"github.com/le-marais/claimsgen/internal/domain/shared"
)

type Source struct {
	key [32]byte
	rng *rand.Rand
}

var _ shared.RandomSource = (*Source)(nil)

func NewSource(seed uint64) *Source {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], seed)
	return fromKey(sha256.Sum256(buf[:]))
}

func fromKey(key [32]byte) *Source {
	return &Source{
		key: key,
		rng: rand.New(rand.NewPCG(
			binary.LittleEndian.Uint64(key[0:8]),
			binary.LittleEndian.Uint64(key[8:16]),
		)),
	}
}

func (s *Source) Split(label string) shared.RandomSource {
	h := sha256.New()
	h.Write(s.key[:])
	h.Write([]byte(label))
	var key [32]byte
	copy(key[:], h.Sum(nil))
	return fromKey(key)
}

func (s *Source) Uniform() float64 {
	return s.rng.Float64()
}

func (s *Source) Bernoulli(p float64) bool {
	return s.rng.Float64() < p
}

func (s *Source) Poisson(mean float64) int {
	if mean <= 0 {
		return 0
	}
	return int(distuv.Poisson{Lambda: mean, Src: s.rng}.Rand())
}

func (s *Source) LogNormal(mu, sigma float64) float64 {
	return distuv.LogNormal{Mu: mu, Sigma: sigma, Src: s.rng}.Rand()
}

// Gamma draws from a gamma distribution with the given shape and scale
// (distuv parameterizes by rate, the inverse of scale).
func (s *Source) Gamma(shape, scale float64) float64 {
	return distuv.Gamma{Alpha: shape, Beta: 1 / scale, Src: s.rng}.Rand()
}

func (s *Source) Pareto(xm, alpha float64) float64 {
	return distuv.Pareto{Xm: xm, Alpha: alpha, Src: s.rng}.Rand()
}
