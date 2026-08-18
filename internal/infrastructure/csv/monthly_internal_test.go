package csv

import "testing"

// TestFormatUnitsRoundsTiesConsistently pins the tie point the negative-zero
// guard and FormatFloat used to disagree on: -5e-07 sits exactly halfway
// between 0 and -0.000001 at six decimal places. math.Round takes it away
// from zero to -0.000001 before formatting, so it must never render as
// "-0.000000".
func TestFormatUnitsRoundsTiesConsistently(t *testing.T) {
	if got, want := formatUnits(-5e-07), "-0.000001"; got != want {
		t.Errorf("formatUnits(-5e-07) = %q, want %q", got, want)
	}
}

// TestFormatAmountNormalisesNegativeZero covers the same guard at the money
// precision: a value that rounds to exactly zero must never print with a
// leading minus.
func TestFormatAmountNormalisesNegativeZero(t *testing.T) {
	if got, want := formatAmount(-1e-10), "0.00"; got != want {
		t.Errorf("formatAmount(-1e-10) = %q, want %q", got, want)
	}
}
