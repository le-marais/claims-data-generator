package claim

import (
	"fmt"
	"math"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// FrequencySchedule is a piecewise-linear daily multiplier on a section's
// occurrence frequency. The factor of a day is 1 before the first point, the
// last point's factor after the last, and linear in whole days between points.
type FrequencySchedule struct {
	dates   []shared.Date
	factors []float64
}

// NewFrequencySchedule builds a schedule from its points. It returns nil, with
// no error, when the list is empty or every factor is exactly 1, so callers
// keep the unscheduled code path. Dates must be ISO, strictly increasing, and
// factors finite and not negative.
func NewFrequencySchedule(points []lob.FrequencyChange) (*FrequencySchedule, error) {
	s := &FrequencySchedule{}
	allOne := true
	for i, p := range points {
		t, err := time.Parse("2006-01-02", p.Date)
		if err != nil {
			return nil, fmt.Errorf("frequency_changes[%d].date: must be a YYYY-MM-DD date, got %q", i, p.Date)
		}
		d := shared.NewDate(t.Year(), t.Month(), t.Day())
		if i > 0 && !d.After(s.dates[i-1]) {
			return nil, fmt.Errorf("frequency_changes[%d].date: must be after the previous point, got %s", i, p.Date)
		}
		if math.IsNaN(p.Factor) || math.IsInf(p.Factor, 0) || p.Factor < 0 {
			return nil, fmt.Errorf("frequency_changes[%d].factor: must be finite and not negative, got %v", i, p.Factor)
		}
		allOne = allOne && p.Factor == 1
		s.dates = append(s.dates, d)
		s.factors = append(s.factors, p.Factor)
	}
	if allOne {
		return nil, nil
	}
	return s, nil
}

// Factor is the multiplier on day d.
func (s *FrequencySchedule) Factor(d shared.Date) float64 {
	n := len(s.dates)
	if d.Before(s.dates[0]) {
		return 1
	}
	if !d.Before(s.dates[n-1]) {
		return s.factors[n-1]
	}
	i := 0
	for i+1 < n && !d.Before(s.dates[i+1]) {
		i++
	}
	span := float64(shared.DaysBetween(s.dates[i], s.dates[i+1]))
	return s.factors[i] + (s.factors[i+1]-s.factors[i])*float64(shared.DaysBetween(s.dates[i], d))/span
}

// Sum is the total of the daily factors over days days from first.
func (s *FrequencySchedule) Sum(first shared.Date, days int) float64 {
	total := 0.0
	for k := 0; k < days; k++ {
		total += s.Factor(first.AddDays(k))
	}
	return total
}

// Pick maps a uniform u in [0, 1) to a day offset in [0, days), weighted by
// the daily factors. A zero-factor day is never picked.
func (s *FrequencySchedule) Pick(first shared.Date, days int, u float64) int {
	target := u * s.Sum(first, days)
	cum, last := 0.0, 0
	for k := 0; k < days; k++ {
		w := s.Factor(first.AddDays(k))
		if w <= 0 {
			continue
		}
		cum += w
		last = k
		if cum > target {
			return k
		}
	}
	return last // rounding: clamp to the last positive-weight day
}
