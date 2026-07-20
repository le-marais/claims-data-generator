package claim

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/lob"
)

func approxf(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

func TestCloseLagRegimeSelectsByComponent(t *testing.T) {
	cl := lob.CloseLagParams{
		Shape: 1.2, MeanDays: 120, SizeThreshold: 20000, SizeMultiplier: 6,
		RiskLoading: 0, ThirdPartyShape: 1.0, ThirdPartyMeanDays: 1200,
	}
	// Own damage, small: base params, no stretch.
	if s, m := closeLagRegime(cl, 5000, 1, true); !approxf(s, 1.2) || !approxf(m, 120) {
		t.Errorf("own-damage small = (%v, %v), want (1.2, 120)", s, m)
	}
	// Own damage, large: base shape, stretched mean.
	if s, m := closeLagRegime(cl, 50000, 1, true); !approxf(s, 1.2) || !approxf(m, 720) {
		t.Errorf("own-damage large = (%v, %v), want (1.2, 720)", s, m)
	}
	// Third party: long-tail params, no size stretch even when large.
	if s, m := closeLagRegime(cl, 50000, 1, false); !approxf(s, 1.0) || !approxf(m, 1200) {
		t.Errorf("third-party = (%v, %v), want (1.0, 1200)", s, m)
	}
}
