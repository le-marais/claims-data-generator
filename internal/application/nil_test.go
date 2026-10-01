package application_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// TestNilClaimsDoNotShiftOtherStages proves the nil knob is shift-free: the
// nil Bernoulli is always drawn, so toggling NilProbability never reshuffles
// the dates or severities of any claim. Only the Nil flag itself may change.
func TestNilClaimsDoNotShiftOtherStages(t *testing.T) {
	off := request(t)
	off.LOB.Claims.NilProbability = 0
	dsOff, err := application.GenerateDataset(t.Context(), random.NewSource(13), off)
	if err != nil {
		t.Fatal(err)
	}
	dsOn, err := application.GenerateDataset(t.Context(), random.NewSource(13), request(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(dsOn.Claims) != len(dsOff.Claims) {
		t.Fatalf("claim count changed: %d vs %d", len(dsOn.Claims), len(dsOff.Claims))
	}
	// withoutNil copies a claim with every episode's nil flag cleared.
	withoutNil := func(c claim.Claim) claim.Claim {
		c.Episodes = slices.Clone(c.Episodes)
		for j := range c.Episodes {
			c.Episodes[j].Nil = false
		}
		return c
	}
	sawNil := false
	for i := range dsOn.Claims {
		if dsOn.Claims[i].Nil() {
			sawNil = true
		}
		if a, b := withoutNil(dsOn.Claims[i]), withoutNil(dsOff.Claims[i]); !reflect.DeepEqual(a, b) {
			t.Fatalf("claim %d shifted when nil toggled:\n on:  %+v\n off: %+v",
				dsOn.Claims[i].ID, dsOn.Claims[i], dsOff.Claims[i])
		}
	}
	if !sawNil {
		t.Fatal("expected at least one nil claim on the default run")
	}
}
