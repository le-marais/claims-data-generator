package application_test

import (
	"math"
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/infrastructure/config"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func pooledLossRatio(r application.SummaryReport) float64 {
	lr, _ := r.Total.LossRatio()
	return lr
}

// Pricing and experience are independent: leaving the true claims model
// unchanged but assuming a cheaper loss cost underprices the book, so the
// realized loss ratio rises on its own.
func TestUnderpricingRaisesRealizedLossRatio(t *testing.T) {
	base, err := config.MotorPersonal()
	if err != nil {
		t.Fatalf("MotorPersonal: %v", err)
	}
	req := application.GenerateRequest{LOB: base, StartYear: 1998, Years: 10, InitialBookSize: 4000}

	dsBase, err := application.GenerateDataset(t.Context(), random.NewSource(1), req)
	if err != nil {
		t.Fatalf("baseline generate: %v", err)
	}
	lrBase := pooledLossRatio(application.Summarize(dsBase, 1998, 10))

	// Underprice: assume own-damage and third-party severity at half the truth.
	// Claims params are untouched, so actual losses are identical; only premium
	// (hence earned premium) falls, lifting the loss ratio.
	under := base
	under.Pricing.Severity.OwnDamageMedianFraction *= 0.5
	under.Pricing.Severity.ThirdPartyScale *= 0.5
	reqUnder := req
	reqUnder.LOB = under

	dsUnder, err := application.GenerateDataset(t.Context(), random.NewSource(1), reqUnder)
	if err != nil {
		t.Fatalf("underpriced generate: %v", err)
	}
	lrUnder := pooledLossRatio(application.Summarize(dsUnder, 1998, 10))

	if lrBase < 0.6 || lrBase > 0.85 {
		t.Fatalf("baseline loss ratio %.3f not near target %.3f (pricing should equal truth)", lrBase, base.Pricing.TargetLossRatio)
	}
	if lrUnder <= lrBase {
		t.Fatalf("underpricing did not raise loss ratio: base %.3f, under %.3f", lrBase, lrUnder)
	}
	if lrUnder <= base.Pricing.TargetLossRatio+0.2 {
		t.Fatalf("underpriced loss ratio %.3f not clearly above target %.3f", lrUnder, base.Pricing.TargetLossRatio)
	}
}

// MR-3: with the pricing basis matched to the claims model, switching nil
// claims or claims inflation on must leave the loss ratio where it was,
// because pricing allows for both. Before the fix nil claims at 0.08 cut the
// loss ratio by 8%, and 4% inflation raised it by about 2%, because premium
// was trended to the start of the underwriting year while claims arise over
// the cover term. Own damage only, so the comparison is not swamped by
// third-party tail noise: the variants share the same claims, scaled or
// zeroed, so the ratios are tight.
func TestMatchedPricingAbsorbsNilClaimsAndInflation(t *testing.T) {
	base, err := config.MotorPersonal()
	if err != nil {
		t.Fatalf("MotorPersonal: %v", err)
	}
	for _, sev := range []*lob.SeverityParams{&base.Claims.Severity, &base.Pricing.Severity} {
		sev.ThirdPartyWeight = 0
	}
	base.Claims.NilProbability, base.Pricing.NilProbability = 0, 0
	base.Claims.Inflation = lob.InflationParams{Mean: 1, Volatility: 0}
	base.Pricing.InflationMean = 1

	lossRatio := func(l lob.LineOfBusiness) float64 {
		t.Helper()
		req := application.GenerateRequest{LOB: l, StartYear: 1998, Years: 10, InitialBookSize: 4000}
		ds, err := application.GenerateDataset(t.Context(), random.NewSource(1), req)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		return pooledLossRatio(application.Summarize(ds, 1998, 10))
	}
	lrBase := lossRatio(base)

	withNil := base
	withNil.Claims.NilProbability, withNil.Pricing.NilProbability = 0.08, 0.08
	withInflation := base
	withInflation.Claims.Inflation = lob.InflationParams{Mean: 1.04, Volatility: 0}
	withInflation.Pricing.InflationMean = 1.04

	for name, l := range map[string]lob.LineOfBusiness{"nil claims": withNil, "inflation": withInflation} {
		if ratio := lossRatio(l) / lrBase; math.Abs(ratio-1) > 0.006 {
			t.Errorf("%s moved the matched loss ratio by a factor %.4f, want 1", name, ratio)
		}
	}
}
