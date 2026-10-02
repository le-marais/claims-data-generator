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

// motorPricing is a two-section pricing basis: own damage sized off the sum
// insured and a Pareto third-party section.
func motorPricing() PricingParams {
	return PricingParams{
		Sections: []PricingSectionParams{
			{Name: "own_damage", BaseFrequency: 0.096, Severity: SeverityParams{Kind: SumInsuredLognormal, MedianFraction: 0.12, Sigma: 1.0}},
			{Name: "third_party", BaseFrequency: 0.024, Severity: SeverityParams{Kind: Pareto, Scale: 4000, Alpha: 2.2}},
		},
		ReopenProbability:    0.04,
		ReopenEstimateFactor: 0.45,
	}
}

func TestExpectedPolicyLossScalesWithRiskAndInflation(t *testing.T) {
	p := motorPricing()
	base := p.ExpectedPolicyLoss(20000, 300, 1.0, 1.0, 1.0)
	if base <= 0 {
		t.Fatalf("expected positive loss, got %v", base)
	}
	// Doubling the risk factor doubles the expectation.
	if got := p.ExpectedPolicyLoss(20000, 300, 2.0, 1.0, 1.0); math.Abs(got-2*base) > 1e-6 {
		t.Fatalf("risk scaling: got %v, want %v", got, 2*base)
	}
	// Higher inflation raises the expectation.
	if got := p.ExpectedPolicyLoss(20000, 300, 1.0, 1.5, 1.0); got <= base {
		t.Fatalf("inflation scaling: got %v, want > %v", got, base)
	}
}

func TestExpectedSectionLossRebasesAndCapsSumInsuredSeverity(t *testing.T) {
	p := motorPricing()
	// De-drift: a larger siDrift (same nominal SI) means a smaller base-year
	// severity, so the expected loss falls.
	full := p.ExpectedSectionLoss(0, 20000, 300, 1.0, 1.0, 1.0)
	deDrifted := p.ExpectedSectionLoss(0, 20000, 300, 1.0, 1.0, 2.0)
	if !(deDrifted < full) {
		t.Fatalf("siDrift should de-drift the sum-insured section: siDrift=2 %.4f not < siDrift=1 %.4f", deDrifted, full)
	}
	// Cap: per-claim cost cannot exceed (sumInsured - excess); drive baseSI far
	// above the cap with a tiny siDrift and check the ceiling holds.
	const si, excess = 20000.0, 300.0
	reopenUplift := 1 + p.ReopenProbability*p.ReopenEstimateFactor
	ceiling := p.Sections[0].BaseFrequency * (si - excess) * reopenUplift
	if got := p.ExpectedSectionLoss(0, si, excess, 1.0, 1.0, 0.01); got > ceiling {
		t.Fatalf("capped section exceeds ceiling: got %.4f, ceiling %.4f", got, ceiling)
	}
}

func TestExpectedSectionLossesAddUpToThePolicyLoss(t *testing.T) {
	p := motorPricing()
	od := p.ExpectedSectionLoss(0, 20000, 300, 1.3, 1.1, 1.05)
	tp := p.ExpectedSectionLoss(1, 20000, 300, 1.3, 1.1, 1.05)
	if od <= 0 || tp <= 0 {
		t.Fatalf("both sections should carry loss: own damage %v, third party %v", od, tp)
	}
	if total := p.ExpectedPolicyLoss(20000, 300, 1.3, 1.1, 1.05); math.Abs(od+tp-total) > 1e-9*total {
		t.Fatalf("sections %v + %v do not add up to the policy loss %v", od, tp, total)
	}
	// A section's frequency scales its loss alone.
	p.Sections[1].BaseFrequency *= 2
	if got := p.ExpectedSectionLoss(1, 20000, 300, 1.3, 1.1, 1.05); math.Abs(got-2*tp) > 1e-9*tp {
		t.Fatalf("doubled third-party frequency: got %v, want %v", got, 2*tp)
	}
	if got := p.ExpectedSectionLoss(0, 20000, 300, 1.3, 1.1, 1.05); got != od {
		t.Fatalf("own damage moved with the third-party frequency: got %v, want %v", got, od)
	}
}

// A section priced at no frequency is skipped, so its unvalidated severity
// cannot turn the cost into NaN or infinity.
func TestExpectedSectionLossSkipsAZeroFrequencySection(t *testing.T) {
	p := motorPricing()
	p.Sections[1] = PricingSectionParams{Name: "third_party"}
	if got := p.ExpectedSectionLoss(1, 20000, 500, 1, 1, 1); got != 0 {
		t.Fatalf("zero-frequency section: got %v, want 0", got)
	}
	if got := p.ExpectedPolicyLoss(20000, 500, 1, 1, 1); !(got > 0) || math.IsInf(got, 0) {
		t.Fatalf("policy loss with one section off: got %v", got)
	}
}

// A nil claim pays nothing in its first episode but still pays a reopen, so
// the expected payout per claim is 1 - nil + reopen probability x factor
// (MR-3).
func TestExpectedPolicyLossAllowsForNilClaims(t *testing.T) {
	p := motorPricing()
	without := p.ExpectedPolicyLoss(20000, 300, 1.0, 1.0, 1.0)
	p.NilProbability = 0.08
	with := p.ExpectedPolicyLoss(20000, 300, 1.0, 1.0, 1.0)
	want := without * (1 - 0.08 + 0.04*0.45) / (1 + 0.04*0.45)
	if math.Abs(with-want) > 1e-9*want {
		t.Fatalf("with nil claims: got %v, want %v", with, want)
	}
}

// A lognormal section prices the uncapped stop-loss of a dollar lognormal,
// trended by the assumed claims index, ignoring the sum insured.
func TestExpectedSectionLossPricesADollarLognormal(t *testing.T) {
	p := motorPricing()
	p.Sections[1].Severity = SeverityParams{Kind: Lognormal, Median: 2000, Sigma: 0.8}
	payout := 1 - p.NilProbability + p.ReopenProbability*p.ReopenEstimateFactor
	want := p.Sections[1].BaseFrequency * 1.3 * payout * stopLossLognormal(1.1*2000, 0.8, 300)
	got := p.ExpectedSectionLoss(1, 20000, 300, 1.3, 1.1, 1.05)
	if math.Abs(got-want) > 1e-9*want {
		t.Fatalf("lognormal section loss = %v, want %v", got, want)
	}
	if other := p.ExpectedSectionLoss(1, 5000, 300, 1.3, 1.1, 1.05); other != got {
		t.Fatalf("lognormal section moved with the sum insured: %v vs %v", other, got)
	}
}

// A per-claim limit L prices E[min((X-d)+, L)] = stopLoss(d) - stopLoss(d+L),
// with the limit in nominal dollars, so it is not trended by the claims index.
func TestExpectedSectionLossAppliesTheLimit(t *testing.T) {
	const si, excess, risk, infl, drift, limit = 20000.0, 300.0, 1.3, 1.1, 1.05, 10000.0
	cases := []struct {
		name     string
		sev      SeverityParams
		stopLoss func(d float64) float64
	}{
		{"pareto", SeverityParams{Kind: Pareto, Scale: 4000, Alpha: 2.2},
			func(d float64) float64 { return stopLossPareto(infl*4000, 2.2, d) }},
		{"lognormal", SeverityParams{Kind: Lognormal, Median: 2000, Sigma: 0.8},
			func(d float64) float64 { return stopLossLognormal(infl*2000, 0.8, d) }},
	}
	for _, c := range cases {
		p := motorPricing()
		p.Sections[1].Severity = c.sev
		unlimited := p.ExpectedSectionLoss(1, si, excess, risk, infl, drift)
		payout := 1 - p.NilProbability + p.ReopenProbability*p.ReopenEstimateFactor
		perClaim := p.Sections[1].BaseFrequency * risk * payout
		if want := perClaim * c.stopLoss(excess); math.Abs(unlimited-want) > 1e-9*want {
			t.Errorf("%s: zero limit = %v, want the unlimited %v", c.name, unlimited, want)
		}
		p.Sections[1].Limit = limit
		got := p.ExpectedSectionLoss(1, si, excess, risk, infl, drift)
		want := perClaim * (c.stopLoss(excess) - c.stopLoss(excess+limit))
		if math.Abs(got-want) > 1e-9*want {
			t.Errorf("%s: limited loss = %v, want %v", c.name, got, want)
		}
		if !(got < unlimited) {
			t.Errorf("%s: limited loss %v not below unlimited %v", c.name, got, unlimited)
		}
	}
}

// A section that takes no excess prices as if the policy had none, on every
// severity kind, and a limit then caps the ground-up loss.
func TestExpectedSectionLossIgnoresTheExcessWhenSwitchedOff(t *testing.T) {
	const si, risk, infl, drift = 20000.0, 1.3, 1.1, 1.05
	for _, sev := range []SeverityParams{
		{Kind: SumInsuredLognormal, MedianFraction: 0.12, Sigma: 1.0},
		{Kind: Pareto, Scale: 4000, Alpha: 2.2},
		{Kind: Lognormal, Median: 2000, Sigma: 0.8},
	} {
		for _, limit := range []float64{0, 10000} {
			if sev.Kind == SumInsuredLognormal && limit > 0 {
				continue
			}
			p := motorPricing()
			p.Sections[1].Severity, p.Sections[1].Limit = sev, limit
			atZero := p.ExpectedSectionLoss(1, si, 0, risk, infl, drift)
			if withExcess := p.ExpectedSectionLoss(1, si, 1000, risk, infl, drift); withExcess >= atZero {
				t.Fatalf("%s limit %v: excess 1000 prices %v, want below the %v at excess 0", sev.Kind, limit, withExcess, atZero)
			}
			p.Sections[1].NoExcess = true
			if got := p.ExpectedSectionLoss(1, si, 1000, risk, infl, drift); got != atZero {
				t.Errorf("%s limit %v: no_excess at excess 1000 prices %v, want %v as at excess 0", sev.Kind, limit, got, atZero)
			}
		}
	}
}
