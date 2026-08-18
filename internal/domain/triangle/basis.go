package triangle

import "fmt"

// OriginBasis selects which period a claim's origin is keyed on, and with it
// which exposure measure pairs with that origin.
type OriginBasis string

const (
	// AccidentMonth keys a claim on the month it occurred, and exposure on
	// the exposure earned in each month.
	AccidentMonth OriginBasis = "accident"
	// UnderwritingMonth keys a claim on the inception month of its policy,
	// and exposure on the exposure written in each month, so a policy's whole
	// premium and whole term land in its inception month.
	UnderwritingMonth OriginBasis = "underwriting"
)

// Validate reports whether the basis is one of the known values.
func (b OriginBasis) Validate() error {
	switch b {
	case AccidentMonth, UnderwritingMonth:
		return nil
	default:
		return fmt.Errorf("origin basis: must be %q or %q, got %q", AccidentMonth, UnderwritingMonth, string(b))
	}
}
