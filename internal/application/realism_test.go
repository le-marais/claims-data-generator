package application_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
	"github.com/le-marais/claimsgen/internal/infrastructure/schedulep"
)

func TestEvaluateRealismProducesChecksAtEveryAge(t *testing.T) {
	refs, err := schedulep.LoadDir("../../data/reference/ppauto_pos98-07")
	if err != nil {
		t.Fatal(err)
	}
	req := request(t)
	req.Years = 10
	req.InitialBookSize = 2000
	ds, err := application.GenerateDataset(random.NewSource(1), req)
	if err != nil {
		t.Fatal(err)
	}
	report := application.EvaluateRealism(ds, refs, req.StartYear, req.Years)
	if len(report.PaidATA) != 9 {
		t.Errorf("paid ATA checks = %d, want 9 (10 development years)", len(report.PaidATA))
	}
	if len(report.IncurredATA) != 9 {
		t.Errorf("incurred ATA checks = %d, want 9", len(report.IncurredATA))
	}
	if report.LossRatio.Value <= 0 {
		t.Errorf("loss ratio = %v, want positive", report.LossRatio.Value)
	}
}
