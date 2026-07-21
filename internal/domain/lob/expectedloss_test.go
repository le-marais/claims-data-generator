package lob

import (
	"math"
	"testing"
)

// monteCarloStopLoss estimates E[(X-excess)+] by sampling, to pin the closed
// forms to the distributions they model.
func monteCarloStopLoss(draw func() float64, excess float64, n int) float64 {
	sum := 0.0
	for i := 0; i < n; i++ {
		if x := draw() - excess; x > 0 {
			sum += x
		}
	}
	return sum / float64(n)
}

func TestStopLossLognormalMatchesMonteCarlo(t *testing.T) {
	median, sigma, excess := 3000.0, 1.0, 500.0
	mu := math.Log(median)
	// Deterministic LCG so the test never flakes.
	var state uint64 = 88172645463325252
	next := func() float64 {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		return float64(state>>11) / float64(1<<53)
	}
	normal := func() float64 {
		u1, u2 := next(), next()
		if u1 < 1e-12 {
			u1 = 1e-12
		}
		return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
	}
	draw := func() float64 { return math.Exp(mu + sigma*normal()) }
	want := monteCarloStopLoss(draw, excess, 2_000_000)
	got := stopLossLognormal(median, sigma, excess)
	if rel := math.Abs(got-want) / want; rel > 0.02 {
		t.Fatalf("stopLossLognormal = %.2f, monte carlo = %.2f (rel %.3f)", got, want, rel)
	}
}

func TestStopLossParetoClosedForm(t *testing.T) {
	scale, alpha := 4000.0, 2.2
	mean := scale * alpha / (alpha - 1)
	// Excess below the Pareto minimum: every loss exceeds it.
	if got := stopLossPareto(scale, alpha, 1000); math.Abs(got-(mean-1000)) > 1e-6 {
		t.Fatalf("excess below scale: got %.4f, want %.4f", got, mean-1000)
	}
	// Excess above the minimum: closed-form tail integral.
	excess := 10000.0
	want := (scale / (alpha - 1)) * math.Pow(scale/excess, alpha-1)
	if got := stopLossPareto(scale, alpha, excess); math.Abs(got-want) > 1e-6 {
		t.Fatalf("excess above scale: got %.4f, want %.4f", got, want)
	}
}

func TestExpectedPolicyLossScalesWithRiskAndInflation(t *testing.T) {
	c := ClaimParams{
		BaseFrequency: 0.12,
		Severity: SeverityParams{
			ThirdPartyWeight:        0.20,
			OwnDamageMedianFraction: 0.12,
			OwnDamageSigma:          1.0,
			ThirdPartyScale:         4000,
			ThirdPartyAlpha:         2.2,
		},
		Reopening: ReopeningParams{Probability: 0.04, EstimateFactor: 0.45},
	}
	base := c.ExpectedPolicyLoss(20000, 300, 1.0, 1.0)
	if base <= 0 {
		t.Fatalf("expected positive loss, got %v", base)
	}
	// Doubling the risk factor doubles the expectation.
	if got := c.ExpectedPolicyLoss(20000, 300, 2.0, 1.0); math.Abs(got-2*base) > 1e-6 {
		t.Fatalf("risk scaling: got %v, want %v", got, 2*base)
	}
	// Higher inflation raises the expectation.
	if got := c.ExpectedPolicyLoss(20000, 300, 1.0, 1.5); got <= base {
		t.Fatalf("inflation scaling: got %v, want > %v", got, base)
	}
}
