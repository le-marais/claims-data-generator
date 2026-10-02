package application_test

import (
	"math"
	"slices"
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

// presetLossRatioBand is how far the shipped preset's simulated loss ratio
// may land from its target, as a factor of the target. The target sets
// premium, not experience: the realized loss ratio is emergent. It moves
// mostly with the simulated inflation path, which pricing knows only by its
// mean, and with each underwriting year's adequacy noise. Over seeds 1-40,
// at a 40k initial book, the preset landed between 0.94 and 1.08 times the
// target, and the spread barely narrows with book size. +/-15% leaves room
// for the preset's pricing and claims assumptions to drift apart by design.
const presetLossRatioBand = 0.15

// The preset's simulated loss ratio lands in a range around its target, not
// on it.
func TestPresetLossRatioLandsNearTarget(t *testing.T) {
	base, err := config.MotorPersonal()
	if err != nil {
		t.Fatalf("MotorPersonal: %v", err)
	}
	target := base.Pricing.TargetLossRatio
	lo, hi := target*(1-presetLossRatioBand), target*(1+presetLossRatioBand)
	req := application.GenerateRequest{LOB: base, StartYear: 1998, Years: 10, InitialBookSize: 4000}
	for _, seed := range []uint64{1, 42, 7} {
		ds, err := application.GenerateDataset(t.Context(), random.NewSource(seed), req)
		if err != nil {
			t.Fatalf("seed %d: generate: %v", seed, err)
		}
		if lr := pooledLossRatio(application.Summarize(ds, 1998, 10)); lr < lo || lr > hi {
			t.Errorf("seed %d: loss ratio %.3f outside [%.3f, %.3f] around target %.2f", seed, lr, lo, hi, target)
		}
	}
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
	under.Pricing.Sections = slices.Clone(base.Pricing.Sections)
	under.Pricing.Sections[ownDamage].Severity.MedianFraction *= 0.5
	under.Pricing.Sections[thirdPartyProperty].Severity.Median *= 0.5
	under.Pricing.Sections[thirdPartyInjury].Severity.Scale *= 0.5
	reqUnder := req
	reqUnder.LOB = under

	dsUnder, err := application.GenerateDataset(t.Context(), random.NewSource(1), reqUnder)
	if err != nil {
		t.Fatalf("underpriced generate: %v", err)
	}
	lrUnder := pooledLossRatio(application.Summarize(dsUnder, 1998, 10))

	if lrUnder <= lrBase {
		t.Fatalf("underpricing did not raise loss ratio: base %.3f, under %.3f", lrBase, lrUnder)
	}
	if lrUnder <= base.Pricing.TargetLossRatio+0.2 {
		t.Fatalf("underpriced loss ratio %.3f not clearly above target %.3f", lrUnder, base.Pricing.TargetLossRatio)
	}
}

// MR-3: the pricing formula is an accurate expectation of its own
// assumptions. With the pricing basis matched to the claims model, switching
// nil claims or claims inflation on must leave the loss ratio where it was,
// because pricing allows for both. Before the fix nil claims at 0.08 cut the
// loss ratio by 8%, and 4% inflation raised it by about 2%, because premium
// was trended to the start of the underwriting year while claims arise over
// the cover term. Own damage only, so the comparison is not swamped by
// third-party tail noise: the variants share the same claims, scaled or
// zeroed, so the ratios are tight. Which claims the nil flag lands on still
// moves a single seed's ratio by up to about 1%, so the loss ratio is pooled
// over four seeds.
func TestMatchedPricingAbsorbsNilClaimsAndInflation(t *testing.T) {
	base, err := config.MotorPersonal()
	if err != nil {
		t.Fatalf("MotorPersonal: %v", err)
	}
	// Own damage only, at the preset's total claim frequency.
	base.Claims.Sections[ownDamage].BaseFrequency, base.Pricing.Sections[ownDamage].BaseFrequency = 0.12, 0.12
	base.Claims.Sections[thirdPartyProperty].BaseFrequency, base.Pricing.Sections[thirdPartyProperty].BaseFrequency = 0, 0
	base.Claims.Sections[thirdPartyInjury].BaseFrequency, base.Pricing.Sections[thirdPartyInjury].BaseFrequency = 0, 0
	base.Claims.NilProbability, base.Pricing.NilProbability = 0, 0
	base.Claims.Inflation = lob.InflationParams{Mean: 1, Volatility: 0}
	base.Pricing.InflationMean = 1

	lossRatio := func(l lob.LineOfBusiness) float64 {
		t.Helper()
		req := application.GenerateRequest{LOB: l, StartYear: 1998, Years: 10, InitialBookSize: 4000}
		var paid, premium float64
		for seed := uint64(1); seed <= 4; seed++ {
			ds, err := application.GenerateDataset(t.Context(), random.NewSource(seed), req)
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			total := application.Summarize(ds, 1998, 10).Total
			paid, premium = paid+total.Paid, premium+total.EarnedPremium
		}
		return paid / premium
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
