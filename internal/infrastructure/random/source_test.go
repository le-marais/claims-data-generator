package random

import (
	"math"
	"testing"
)

func TestSameSeedSameDraws(t *testing.T) {
	a := NewSource(42)
	b := NewSource(42)
	for i := 0; i < 100; i++ {
		if a.Uniform() != b.Uniform() {
			t.Fatalf("draw %d differs for identical seeds", i)
		}
	}
}

func TestDifferentSeedsDifferentDraws(t *testing.T) {
	a := NewSource(1)
	b := NewSource(2)
	same := 0
	for i := 0; i < 100; i++ {
		if a.Uniform() == b.Uniform() {
			same++
		}
	}
	if same > 1 {
		t.Errorf("%d/100 identical draws across different seeds", same)
	}
}

func TestSplitIsDeterministicAndIndependentOfParentState(t *testing.T) {
	// Splitting must depend only on (seed, label), not on how much the
	// parent has already drawn.
	fresh := NewSource(42).Split("claims")
	parent := NewSource(42)
	parent.Uniform()
	parent.Uniform()
	late := parent.Split("claims")
	for i := 0; i < 100; i++ {
		if fresh.Uniform() != late.Uniform() {
			t.Fatalf("draw %d differs between fresh and late split", i)
		}
	}
}

func TestSplitDifferentLabelsDiffer(t *testing.T) {
	a := NewSource(42).Split("claims")
	b := NewSource(42).Split("policies")
	same := 0
	for i := 0; i < 100; i++ {
		if a.Uniform() == b.Uniform() {
			same++
		}
	}
	if same > 1 {
		t.Errorf("%d/100 identical draws across different labels", same)
	}
}

func TestNestedSplitsDiffer(t *testing.T) {
	a := NewSource(42).Split("book").Split("policy-1")
	b := NewSource(42).Split("book").Split("policy-2")
	if a.Uniform() == b.Uniform() {
		t.Error("nested splits with different labels gave identical draws")
	}
}

func TestUniformRange(t *testing.T) {
	src := NewSource(7)
	for i := 0; i < 10000; i++ {
		u := src.Uniform()
		if u < 0 || u >= 1 {
			t.Fatalf("Uniform() = %v out of [0,1)", u)
		}
	}
}

// checkMean draws n samples and asserts the sample mean is within tol
// (relative) of want.
func checkMean(t *testing.T, name string, n int, want, tol float64, draw func() float64) {
	t.Helper()
	sum := 0.0
	for i := 0; i < n; i++ {
		sum += draw()
	}
	got := sum / float64(n)
	if math.Abs(got-want)/want > tol {
		t.Errorf("%s sample mean = %v, want %v (±%v%%)", name, got, want, tol*100)
	}
}

func TestDistributionMeans(t *testing.T) {
	const n = 200000
	src := NewSource(99)
	checkMean(t, "Uniform", n, 0.5, 0.02, src.Uniform)
	checkMean(t, "LogNormal(0,0.5)", n, math.Exp(0.125), 0.02, func() float64 { return src.LogNormal(0, 0.5) })
	checkMean(t, "Gamma(2,3)", n, 6, 0.02, func() float64 { return src.Gamma(2, 3) })
	checkMean(t, "Pareto(100,3)", n, 150, 0.05, func() float64 { return src.Pareto(100, 3) })
	checkMean(t, "Poisson(4)", n, 4, 0.02, func() float64 { return float64(src.Poisson(4)) })
	checkMean(t, "Bernoulli(0.3)", n, 0.3, 0.02, func() float64 {
		if src.Bernoulli(0.3) {
			return 1
		}
		return 0
	})
}

func TestParetoNeverBelowScale(t *testing.T) {
	src := NewSource(5)
	for i := 0; i < 10000; i++ {
		if v := src.Pareto(100, 2); v < 100 {
			t.Fatalf("Pareto(100,2) = %v below scale", v)
		}
	}
}

func TestBetaMeanAndBounds(t *testing.T) {
	src := NewSource(9)
	const n = 20000
	sum := 0.0
	for i := 0; i < n; i++ {
		v := src.Beta(2, 8)
		if v <= 0 || v >= 1 {
			t.Fatalf("draw %d: Beta(2, 8) = %v, want strictly in (0, 1)", i, v)
		}
		sum += v
	}
	if mean := sum / n; math.Abs(mean-0.2) > 0.01 {
		t.Errorf("mean of Beta(2, 8) draws = %v, want ~0.2", mean)
	}
}

func TestBetaIsDeterministicPerSeed(t *testing.T) {
	a, b := NewSource(11), NewSource(11)
	for i := 0; i < 50; i++ {
		if a.Beta(3, 5) != b.Beta(3, 5) {
			t.Fatalf("draw %d differs for identical seeds", i)
		}
	}
}
