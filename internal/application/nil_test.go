package application_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// TestNilClaimsDoNotShiftOtherStages proves the nil knob is shift-free: the
// nil Bernoulli is always drawn, so toggling NilProbability never reshuffles
// the dates or severities of any claim. Only the Nil flag itself may change,
// and with it a nil claim's dates: it pays nothing, so it closes without the
// payment delay, and a reopen follows that earlier close.
func TestNilClaimsDoNotShiftOtherStages(t *testing.T) {
	// The seasonal holiday may defer a paying close but never a nil one, and
	// business days roll each close on its own, so a nil claim and its
	// paying twin can differ by more than the payment delay; the test
	// switches both off to compare them.
	on := request(t)
	on.LOB.SeasonalHoliday = lob.SeasonalHolidayParams{}
	on.LOB.BusinessDays = lob.BusinessDayParams{}
	off := on
	off.LOB.Claims.NilProbability = 0
	dsOff, err := application.GenerateDataset(t.Context(), random.NewSource(13), off)
	if err != nil {
		t.Fatal(err)
	}
	dsOn, err := application.GenerateDataset(t.Context(), random.NewSource(13), on)
	if err != nil {
		t.Fatal(err)
	}
	if len(dsOn.Claims) != len(dsOff.Claims) {
		t.Fatalf("claim count changed: %d vs %d", len(dsOn.Claims), len(dsOff.Claims))
	}
	// withoutNil copies a claim with every episode's nil flag cleared and,
	// when its first episode is nil, the payment delay it skipped added back
	// to its dates.
	delay := int(off.LOB.Runoff.PaymentDelayDays)
	withoutNil := func(c claim.Claim) claim.Claim {
		c.Episodes = slices.Clone(c.Episodes)
		shift := 0
		if c.Episodes[0].Nil {
			shift = delay
		}
		for j := range c.Episodes {
			ep := &c.Episodes[j]
			ep.Nil = false
			if j > 0 {
				ep.Open = ep.Open.AddDays(shift)
			}
			ep.Close = ep.Close.AddDays(shift)
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
