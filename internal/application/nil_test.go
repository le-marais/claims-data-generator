package application_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// TestNilClaimsDoNotShiftOtherStages proves the nil knob is shift-free: the
// nil Bernoulli is always drawn, so toggling NilProbability never reshuffles
// the dates or severities of any claim. Only the Nil flag itself may change.
func TestNilClaimsDoNotShiftOtherStages(t *testing.T) {
	off := request(t)
	off.LOB.Claims.NilProbability = 0
	dsOff, err := application.GenerateDataset(random.NewSource(13), off)
	if err != nil {
		t.Fatal(err)
	}
	dsOn, err := application.GenerateDataset(random.NewSource(13), request(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(dsOn.Claims) != len(dsOff.Claims) {
		t.Fatalf("claim count changed: %d vs %d", len(dsOn.Claims), len(dsOff.Claims))
	}
	sawNil := false
	for i := range dsOn.Claims {
		a, b := dsOn.Claims[i], dsOff.Claims[i]
		if a.Nil {
			sawNil = true
		}
		a.Nil, b.Nil = false, false
		if a != b {
			t.Fatalf("claim %d shifted when nil toggled:\n on:  %+v\n off: %+v",
				dsOn.Claims[i].ID, dsOn.Claims[i], dsOff.Claims[i])
		}
	}
	if !sawNil {
		t.Fatal("expected at least one nil claim on the default run")
	}
}
