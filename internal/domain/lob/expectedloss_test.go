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

func TestLimitedStopLossLognormal(t *testing.T) {
	const median, sigma = 5000.0, 1.0
	// With a cap it equals the difference of two stop-loss layers.
	excess, cap := 300.0, 20000.0
	want := stopLossLognormal(median, sigma, excess) - stopLossLognormal(median, sigma, cap)
	if got := limitedStopLossLognormal(median, sigma, excess, cap); math.Abs(got-want) > 1e-9 {
		t.Fatalf("layered form: got %.6f, want %.6f", got, want)
	}
	// A finite cap is strictly cheaper than the uncapped stop-loss.
	uncapped := stopLossLognormal(median, sigma, excess)
	if got := limitedStopLossLognormal(median, sigma, excess, cap); got >= uncapped {
		t.Fatalf("cap should reduce cost: capped %.6f, uncapped %.6f", got, uncapped)
	}
	// cap <= excess yields no layer.
	if got := limitedStopLossLognormal(median, sigma, 20000, 20000); got != 0 {
		t.Fatalf("cap == excess: got %.6f, want 0", got)
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
	base := c.ExpectedPolicyLoss(20000, 300, 1.0, 1.0, 1.0)
	if base <= 0 {
		t.Fatalf("expected positive loss, got %v", base)
	}
	// Doubling the risk factor doubles the expectation.
	if got := c.ExpectedPolicyLoss(20000, 300, 2.0, 1.0, 1.0); math.Abs(got-2*base) > 1e-6 {
		t.Fatalf("risk scaling: got %v, want %v", got, 2*base)
	}
	// Higher inflation raises the expectation.
	if got := c.ExpectedPolicyLoss(20000, 300, 1.0, 1.5, 1.0); got <= base {
		t.Fatalf("inflation scaling: got %v, want > %v", got, base)
	}
}

func TestExpectedPolicyLossRebasesAndCapsOwnDamage(t *testing.T) {
	c := ClaimParams{
		BaseFrequency: 0.12,
		Severity: SeverityParams{
			ThirdPartyWeight:        0, // pure own damage
			OwnDamageMedianFraction: 0.12,
			OwnDamageSigma:          1.0,
			ThirdPartyScale:         4000,
			ThirdPartyAlpha:         2.2,
		},
		Reopening: ReopeningParams{Probability: 0.04, EstimateFactor: 0.45},
	}
	// De-drift: a larger siDrift (same nominal SI) means a smaller base-year
	// severity, so the expected loss falls.
	full := c.ExpectedPolicyLoss(20000, 300, 1.0, 1.0, 1.0)
	deDrifted := c.ExpectedPolicyLoss(20000, 300, 1.0, 1.0, 2.0)
	if !(deDrifted < full) {
		t.Fatalf("siDrift should de-drift OD: siDrift=2 %.4f not < siDrift=1 %.4f", deDrifted, full)
	}
	// Cap: per-claim OD cannot exceed (sumInsured - excess); drive baseSI far
	// above the cap with a tiny siDrift and check the ceiling holds.
	const si, excess = 20000.0, 300.0
	reopenUplift := 1 + c.Reopening.Probability*c.Reopening.EstimateFactor
	ceiling := c.BaseFrequency * 1.0 * (si - excess) * reopenUplift
	if got := c.ExpectedPolicyLoss(si, excess, 1.0, 1.0, 0.01); got > ceiling {
		t.Fatalf("capped OD exceeds ceiling: got %.4f, ceiling %.4f", got, ceiling)
	}
}
