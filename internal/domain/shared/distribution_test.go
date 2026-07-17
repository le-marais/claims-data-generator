package shared_test

import (
	"math"
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func TestMeanOneLogNormalZeroSigmaIsOne(t *testing.T) {
	if got := shared.MeanOneLogNormal(random.NewSource(1), 0); got != 1 {
		t.Fatalf("sigma 0: got %v, want 1", got)
	}
}

func TestMeanOneLogNormalMeanIsApproximatelyOne(t *testing.T) {
	src := random.NewSource(7)
	sum := 0.0
	const n = 20000
	for i := 0; i < n; i++ {
		sum += shared.MeanOneLogNormal(src, 0.3)
	}
	if mean := sum / n; math.Abs(mean-1) > 0.02 {
		t.Fatalf("empirical mean %v, want approximately 1", mean)
	}
}
