package shared

import (
	"fmt"
	"math"
)

// Money is an amount in whole cents.
type Money int64

// OneCent is the smallest representable positive amount.
const OneCent Money = 1

// FromDollars converts a dollar amount to Money, rounding to the nearest cent.
func FromDollars(d float64) Money {
	if math.IsNaN(d) || math.IsInf(d, 0) {
		return 0
	}
	return Money(math.Round(d * 100))
}

// Dollars returns the amount in dollars.
func (m Money) Dollars() float64 {
	return float64(m) / 100
}

// MulFloat scales the amount by f, rounding to the nearest cent.
func (m Money) MulFloat(f float64) Money {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return Money(math.Round(float64(m) * f))
}

// String formats the amount as dollars and cents, e.g. "1234.56".
func (m Money) String() string {
	sign := ""
	if m < 0 {
		sign = "-"
		m = -m
	}
	return fmt.Sprintf("%s%d.%02d", sign, m/100, m%100)
}
