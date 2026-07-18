package application_test

import (
	"fmt"
	"testing"

	refdata "github.com/le-marais/claimsgen/data/reference"
	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
	"github.com/le-marais/claimsgen/internal/infrastructure/schedulep"
)

// TestDefaultPresetIsRealistic is the MVP realism gate: data generated with
// the shipped motor-personal preset must land inside the bands observed
// across the Schedule P reference companies.
func TestDefaultPresetIsRealistic(t *testing.T) {
	refs, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDirs...)
	if err != nil {
		t.Fatal(err)
	}
	req := request(t)
	req.StartYear = 1998
	req.Years = 10
	req.InitialBookSize = 10000
	// Run the gate on several seeds so a calibration that only happens to
	// pass on one seed is caught here.
	for _, seed := range []uint64{1, 42, 7} {
		seed := seed
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			ds, err := application.GenerateDataset(random.NewSource(seed), req)
			if err != nil {
				t.Fatal(err)
			}
			report := application.EvaluateRealism(ds, refs, req.StartYear, req.Years)
			if !report.Pass() {
				t.Errorf("generated data outside Schedule P bands:\n%s", report)
			}
		})
	}
}

func TestEvaluateRealismProducesChecksAtEveryAge(t *testing.T) {
	refs, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDirs...)
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
