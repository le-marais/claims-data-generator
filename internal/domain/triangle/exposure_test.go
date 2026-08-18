package triangle_test

import (
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// dollarADayPolicy covers 365 days from 1 October 1998 for a premium of $365,
// so a day of cover earns exactly one dollar and every expected figure below
// is a day count.
func dollarADayPolicy() policy.Policy {
	start := shared.NewDate(1998, time.October, 1)
	return policy.Policy{
		ID:         1,
		CoverStart: start,
		CoverEnd:   start.AddDays(364),
		Premium:    shared.FromDollars(365),
	}
}

func TestExposureByMonthEarnsPremiumByDay(t *testing.T) {
	exposure := triangle.ExposureByMonth(
		[]policy.Policy{dollarADayPolicy()},
		shared.NewMonth(1998, time.January), 24, triangle.AccidentMonth)

	if len(exposure) != 24 {
		t.Fatalf("got %d months, want 24", len(exposure))
	}
	// Index 8 is September 1998, before cover starts; 9 is October (31 days).
	if !approx(exposure[8].Premium, 0) {
		t.Errorf("September 1998 premium = %v, want 0", exposure[8].Premium)
	}
	if !approx(exposure[9].Premium, 31) {
		t.Errorf("October 1998 premium = %v, want 31", exposure[9].Premium)
	}
	if !approx(exposure[10].Premium, 30) {
		t.Errorf("November 1998 premium = %v, want 30", exposure[10].Premium)
	}
	// Index 20 is September 1999, cover's last month: 30 days to the 30th.
	if !approx(exposure[20].Premium, 30) {
		t.Errorf("September 1999 premium = %v, want 30", exposure[20].Premium)
	}
	if !approx(exposure[21].Premium, 0) {
		t.Errorf("October 1999 premium = %v, want 0", exposure[21].Premium)
	}
	if got, want := exposure[9].Month, shared.NewMonth(1998, time.October); got != want {
		t.Errorf("index 9 month = %v, want %v", got, want)
	}
}

func TestExposureByMonthCountsUnitsAndPolicies(t *testing.T) {
	exposure := triangle.ExposureByMonth(
		[]policy.Policy{dollarADayPolicy()},
		shared.NewMonth(1998, time.January), 24, triangle.AccidentMonth)

	if !approx(exposure[9].ExposureUnits, 31/365.25) {
		t.Errorf("October 1998 exposure units = %v, want %v", exposure[9].ExposureUnits, 31/365.25)
	}
	if exposure[9].Policies != 1 {
		t.Errorf("October 1998 policies = %d, want 1", exposure[9].Policies)
	}
	if exposure[8].Policies != 0 {
		t.Errorf("September 1998 policies = %d, want 0", exposure[8].Policies)
	}
}

func TestExposureByMonthUnderwritingBasisLandsEverythingAtInception(t *testing.T) {
	exposure := triangle.ExposureByMonth(
		[]policy.Policy{dollarADayPolicy()},
		shared.NewMonth(1998, time.January), 24, triangle.UnderwritingMonth)

	if !approx(exposure[9].Premium, 365) {
		t.Errorf("October 1998 premium = %v, want 365 (whole premium at inception)", exposure[9].Premium)
	}
	if !approx(exposure[9].ExposureUnits, 365/365.25) {
		t.Errorf("October 1998 exposure units = %v, want %v", exposure[9].ExposureUnits, 365/365.25)
	}
	if exposure[9].Policies != 1 {
		t.Errorf("October 1998 policies = %d, want 1", exposure[9].Policies)
	}
	for _, i := range []int{10, 11, 12, 20} {
		if !approx(exposure[i].Premium, 0) {
			t.Errorf("index %d premium = %v, want 0 on the underwriting basis", i, exposure[i].Premium)
		}
		if exposure[i].Policies != 0 {
			t.Errorf("index %d policies = %d, want 0 on the underwriting basis", i, exposure[i].Policies)
		}
	}
}

func TestExposureByMonthClipsToTheWindow(t *testing.T) {
	// Cover starts before the window and ends inside it: only the overlap counts.
	p := policy.Policy{
		ID:         1,
		CoverStart: shared.NewDate(1997, time.December, 1),
		CoverEnd:   shared.NewDate(1998, time.January, 30),
		Premium:    shared.FromDollars(61), // 61 cover days, one dollar each
	}
	exposure := triangle.ExposureByMonth(
		[]policy.Policy{p}, shared.NewMonth(1998, time.January), 12, triangle.AccidentMonth)
	if !approx(exposure[0].Premium, 30) {
		t.Errorf("January 1998 premium = %v, want 30", exposure[0].Premium)
	}
	if exposure[0].Policies != 1 {
		t.Errorf("January 1998 policies = %d, want 1", exposure[0].Policies)
	}
	total := 0.0
	for _, e := range exposure {
		total += e.Premium
	}
	if !approx(total, 30) {
		t.Errorf("total premium in window = %v, want 30", total)
	}
}

func TestExposureByMonthAddsUpPoliciesInForce(t *testing.T) {
	a := dollarADayPolicy()
	b := dollarADayPolicy()
	b.ID = 2
	exposure := triangle.ExposureByMonth(
		[]policy.Policy{a, b}, shared.NewMonth(1998, time.January), 24, triangle.AccidentMonth)
	if exposure[9].Policies != 2 {
		t.Errorf("October 1998 policies = %d, want 2", exposure[9].Policies)
	}
	if !approx(exposure[9].Premium, 62) {
		t.Errorf("October 1998 premium = %v, want 62", exposure[9].Premium)
	}
}

func TestEarnedPremiumByYearIsTheMonthlySumsRolledUp(t *testing.T) {
	policies := []policy.Policy{dollarADayPolicy()}
	yearly := triangle.EarnedPremiumByYear(policies, 1998, 2)
	monthly := triangle.ExposureByMonth(policies, shared.NewMonth(1998, time.January), 24, triangle.AccidentMonth)
	for y := 0; y < 2; y++ {
		sum := 0.0
		for i := y * 12; i < (y+1)*12; i++ {
			sum += monthly[i].Premium
		}
		if !approx(yearly[y], sum) {
			t.Errorf("year %d: yearly = %v, monthly sum = %v", 1998+y, yearly[y], sum)
		}
	}
}

func TestOriginBasisValidate(t *testing.T) {
	for _, b := range []triangle.OriginBasis{triangle.AccidentMonth, triangle.UnderwritingMonth} {
		if err := b.Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", b, err)
		}
	}
	if err := triangle.OriginBasis("policy").Validate(); err == nil {
		t.Error("Validate(\"policy\") = nil, want an error naming the allowed values")
	}
}
