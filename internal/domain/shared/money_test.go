package shared

import "testing"

func TestFromDollarsRoundsToNearestCent(t *testing.T) {
	cases := []struct {
		dollars float64
		want    Money
	}{
		{1234.56, Money(123456)},
		{1234.567, Money(123457)},
		{0.004, Money(0)},
		{-0.5, Money(-50)},
	}
	for _, c := range cases {
		if got := FromDollars(c.dollars); got != c.want {
			t.Errorf("FromDollars(%v) = %d, want %d", c.dollars, got, c.want)
		}
	}
}

func TestMoneyStringFormatsDollarsAndCents(t *testing.T) {
	cases := []struct {
		m    Money
		want string
	}{
		{Money(123456), "1234.56"},
		{Money(5), "0.05"},
		{Money(0), "0.00"},
		{Money(-50), "-0.50"},
		{Money(-5), "-0.05"},
	}
	for _, c := range cases {
		if got := c.m.String(); got != c.want {
			t.Errorf("Money(%d).String() = %q, want %q", c.m, got, c.want)
		}
	}
}

func TestMoneyMulFloatRounds(t *testing.T) {
	if got := Money(10000).MulFloat(1.5); got != Money(15000) {
		t.Errorf("Money(10000).MulFloat(1.5) = %d, want 15000", got)
	}
	if got := Money(333).MulFloat(0.5); got != Money(167) {
		t.Errorf("Money(333).MulFloat(0.5) = %d, want 167", got)
	}
}

func TestMoneyDollars(t *testing.T) {
	if got := Money(123456).Dollars(); got != 1234.56 {
		t.Errorf("Money(123456).Dollars() = %v, want 1234.56", got)
	}
}
