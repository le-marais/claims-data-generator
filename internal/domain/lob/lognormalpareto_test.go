package lob

import (
	"math"
	"testing"
)

// The tail share is set so the density is continuous at scale: narrow
// equal-width bins just below and just above scale hold about as many losses
// (the density's own slope moves them about 1% apart; a step at scale would
// move them tens of percent), and the tail holds its share of them.
func TestLognormalParetoTailShareKeepsTheDensityContinuous(t *testing.T) {
	d := newLognormalPareto(4000, 1.0, 25000, 2.0)
	if d.tail < 0.035 || d.tail > 0.039 {
		t.Fatalf("tail share = %v, want about 0.037", d.tail)
	}
	const n = 4_000_000
	below, above, tail := 0, 0, 0
	for i := 0; i < n; i++ {
		x := d.quantile((float64(i) + 0.5) / n)
		switch {
		case x >= 24900 && x < 25000:
			below++
		case x >= 25000 && x < 25100:
			above++
		}
		if x > 25000 {
			tail++
		}
	}
	if rel := math.Abs(float64(below-above)) / float64(above); rel > 0.03 {
		t.Errorf("bins below and above scale hold %d and %d losses, want about equal", below, above)
	}
	if got := float64(tail) / n; math.Abs(got-d.tail) > 1e-4 {
		t.Errorf("share above scale = %v, want the tail share %v", got, d.tail)
	}
}

// The closed-form stop-loss agrees with the kind's own draws, at excesses
// below and above scale.
func TestLognormalParetoStopLossMatchesItsDraws(t *testing.T) {
	d := newLognormalPareto(4000, 1.0, 25000, 2.0)
	const n = 2_000_000
	for _, excess := range []float64{0, 500, 10000, 25000, 60000} {
		sum := 0.0
		for i := 0; i < n; i++ {
			if x := d.quantile((float64(i)+0.5)/n) - excess; x > 0 {
				sum += x
			}
		}
		want := sum / n
		if got := d.stopLoss(excess); math.Abs(got-want) > 0.01*want {
			t.Errorf("stopLoss(%v) = %.2f, draws give %.2f", excess, got, want)
		}
	}
}

// Scaling the distribution leaves its tail share alone, so trending median
// and scale by the same index trends the whole loss.
func TestLognormalParetoTailShareIsScaleFree(t *testing.T) {
	a := newLognormalPareto(4000, 1.0, 25000, 2.0)
	b := newLognormalPareto(1.3*4000, 1.0, 1.3*25000, 2.0)
	if math.Abs(a.tail-b.tail) > 1e-12 {
		t.Fatalf("tail share moved with scale: %v vs %v", a.tail, b.tail)
	}
	if got, want := b.stopLoss(1.3*500), 1.3*a.stopLoss(500); math.Abs(got-want) > 1e-9*want {
		t.Fatalf("scaled stop-loss = %v, want %v", got, want)
	}
}

// A lognormal_pareto section prices its layer from the spliced stop-loss,
// with median and scale trended by the assumed claims index and the limit
// nominal.
func TestExpectedSectionLossPricesALognormalPareto(t *testing.T) {
	p := motorPricing()
	p.Sections[1].Severity = SeverityParams{Kind: LognormalPareto, Median: 4000, Sigma: 1.0, Scale: 25000, Alpha: 2.0}
	p.Sections[1].Limit = 100000
	d := newLognormalPareto(1.1*4000, 1.0, 1.1*25000, 2.0)
	payout := 1 - p.NilProbability + p.ReopenProbability*p.ReopenEstimateFactor
	want := p.Sections[1].BaseFrequency * 1.3 * payout * (d.stopLoss(300) - d.stopLoss(100300))
	if got := p.ExpectedSectionLoss(1, 20000, 300, 1.3, 1.1, 1.05); math.Abs(got-want) > 1e-9*want {
		t.Fatalf("section loss = %v, want %v", got, want)
	}
}
