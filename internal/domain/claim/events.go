package claim

import (
	"sort"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// Event is one book-wide loss event, such as a hail storm: it starts on Start
// and lasts Days days. Every policy in force through it may be hit.
type Event struct {
	Start shared.Date
	Days  int
}

// End is the day after the event's last day.
func (e Event) End() shared.Date { return e.Start.AddDays(e.Days) }

// EventSet holds each section's events, by section index in the line of
// business's sections, in start order. A section without events has none.
type EventSet [][]Event

// DrawEvents draws the event set of every events section over the run
// window [startYear, startYear+years). Each section draws from its own root
// sub-stream, labelled by its name ("<name>-events"), so adding or changing
// an events section never moves another stage's draws. Each year draws a
// Poisson count of events at PerYear, and each event a start day uniform
// through the year's season.
func DrawEvents(src shared.RandomSource, sections []lob.SectionParams, startYear, years int) EventSet {
	set := make(EventSet, len(sections))
	for i, sec := range sections {
		if !sec.Events.Enabled() {
			continue
		}
		stream := src.Split(sec.Name + "-events")
		for y := startYear; y < startYear+years; y++ {
			first, days := season(sec.Events, y)
			n := stream.Poisson(sec.Events.PerYear)
			for range n {
				set[i] = append(set[i], Event{
					Start: first.AddDays(int(stream.Uniform() * float64(days))),
					Days:  int(sec.Events.DurationDays),
				})
			}
		}
		sort.SliceStable(set[i], func(a, b int) bool { return set[i][a].Start.Before(set[i][b].Start) })
	}
	return set
}

// season is the first day and the number of days of the year's event
// season: the whole year, or SeasonMonths from the first of SeasonStartMonth,
// running into the next year past December.
func season(e lob.EventParams, year int) (first shared.Date, days int) {
	if e.SeasonMonths == 0 {
		first = shared.NewDate(year, time.January, 1)
		return first, shared.DaysBetween(first, shared.NewDate(year+1, time.January, 1))
	}
	start := time.Month(e.SeasonStartMonth)
	first = shared.NewDate(year, start, 1)
	return first, shared.DaysBetween(first, shared.NewDate(year, start+time.Month(e.SeasonMonths), 1))
}
