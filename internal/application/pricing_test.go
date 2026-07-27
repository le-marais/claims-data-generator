package application_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
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

	dsBase, err := application.GenerateDataset(random.NewSource(1), req)
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

	dsUnder, err := application.GenerateDataset(random.NewSource(1), reqUnder)
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
