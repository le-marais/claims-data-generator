package claim_test

import (
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// weather appends a hail section to params: three-day events, a few a year
// through April to September, each hitting about 1% of the book.
func weather(p lob.ClaimParams, events lob.EventParams) lob.ClaimParams {
	p.Sections = append(slices.Clone(p.Sections), lob.SectionParams{
		Name:       "weather",
		Severity:   lob.SeverityParams{Kind: lob.SumInsuredLognormal, MedianFraction: 0.15, Sigma: 0.8},
		ReportLag:  lob.ReportLagParams{Median: 3, Sigma: 1.0},
		CloseLag:   lob.CloseLagParams{Shape: 1.2, MeanDays: 45},
		Recoveries: true,
		Events:     events,
	})
	return p
}

var hail = lob.EventParams{PerYear: 4, HitShare: 0.01, DurationDays: 3, SeasonStartMonth: 4, SeasonMonths: 6}

func TestDrawEventsFallInTheSeasonAndLastTheirDuration(t *testing.T) {
	p := weather(params(), hail)
	set := claim.DrawEvents(random.NewSource(1), p.Sections, 1998, 50)
	if len(set) != 3 || set[ownDamage] != nil || set[thirdParty] != nil {
		t.Fatalf("only the events section draws events: %v", set)
	}
	events := set[2]
	if n := float64(len(events)) / 50; math.Abs(n-4) > 0.8 {
		t.Errorf("events a year = %v, want ~4", n)
	}
	for i, e := range events {
		if e.Days != 3 {
			t.Errorf("event %d lasts %d days, want 3", i, e.Days)
		}
		if m := e.Start.Month().Month(); m < time.April || m > time.September {
			t.Errorf("event %d starts %v, outside April-September", i, e.Start)
		}
		if i > 0 && e.Start.Before(events[i-1].Start) {
			t.Errorf("events out of start order at %d", i)
		}
	}
}

func TestDrawEventsSeasonWrapsIntoTheNextYear(t *testing.T) {
	e := hail
	e.PerYear, e.SeasonStartMonth, e.SeasonMonths = 20, 11, 3 // November to January
	set := claim.DrawEvents(random.NewSource(2), weather(params(), e).Sections, 2000, 5)
	for _, ev := range set[2] {
		if m := ev.Start.Month().Month(); m != time.November && m != time.December && m != time.January {
			t.Errorf("event starts %v, outside November-January", ev.Start)
		}
	}
}

// Events sections draw from their own root streams, so adding one moves no
// claim of another section.
func TestAnEventsSectionLeavesTheOtherSectionsUnchanged(t *testing.T) {
	book := fixedBook(5000, 20000, 300, 1.0)
	// Simulate splits per policy off the source it is given; give both runs
	// the same one.
	before := claim.NewClaimSimulator(params()).Simulate(random.NewSource(5).Split("claims"), book)
	p := weather(params(), hail)
	src := random.NewSource(5)
	after := claim.NewClaimSimulator(p).
		WithEvents(claim.DrawEvents(src, p.Sections, 1998, 2)).
		Simulate(src.Split("claims"), book)
	var kept []claim.Claim
	weatherClaims := 0
	for _, c := range after {
		if c.Section == 2 {
			weatherClaims++
			continue
		}
		kept = append(kept, c)
	}
	if weatherClaims == 0 {
		t.Fatal("no weather claims")
	}
	if len(kept) != len(before) {
		t.Fatalf("other sections: %d claims, want %d", len(kept), len(before))
	}
	for i := range kept {
		kept[i].ID = before[i].ID // IDs follow report order across every section
		if !reflect.DeepEqual(kept[i], before[i]) {
			t.Fatalf("claim %d moved:\n got %+v\nwant %+v", i, kept[i], before[i])
		}
	}
}

// Every weather claim occurs inside an event, many claims share each event,
// and the frequency is events a year times the hit share times the risk
// factor, the frequency pricing assumes.
func TestEventClaimsClusterInEventsAtThePricedFrequency(t *testing.T) {
	p := only(weather(params(), hail), 2, 0)
	for _, risk := range []float64{1, 2} {
		src := random.NewSource(7)
		events := claim.DrawEvents(src, p.Sections, 1998, 1)
		// Policies covering the whole of 1998, so each is in force for every
		// event, at excess 0 so every loss is reported.
		book := fixedBook(20000, 20000, 0, risk)
		for i := range book {
			book[i].CoverStart = shared.NewDate(1998, time.January, 1)
			book[i].CoverEnd = book[i].CoverStart.AddDays(364)
		}
		claims := claim.NewClaimSimulator(p).WithEvents(events).WithWindow(1998, 1).
			Simulate(src.Split("claims"), book)
		perEvent := make([]int, len(events[2]))
		for _, c := range claims {
			found := false
			for i, e := range events[2] {
				if !c.OccurrenceDate.Before(e.Start) && c.OccurrenceDate.Before(e.End()) {
					perEvent[i]++
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("claim on %v occurs outside every event", c.OccurrenceDate)
			}
		}
		if len(events[2]) == 0 {
			t.Fatal("no events drawn")
		}
		// Each event hits about hit share x risk factor of the book.
		for i, n := range perEvent {
			want := 20000 * hail.HitShare * risk
			if math.Abs(float64(n)-want) > 5*math.Sqrt(want) {
				t.Errorf("risk %v: event %d hit %d policies, want ~%v", risk, i, n, want)
			}
		}
	}
}

// A policy in force for part of an event is hit with that share of the
// chance: summed over the events, the hits per policy are hit share x risk
// factor x the event days it is in force over each event's duration, which
// averages to the priced frequency per policy-year in force.
func TestPartialEventCoverScalesTheHitChance(t *testing.T) {
	p := only(weather(params(), lob.EventParams{PerYear: 60, HitShare: 0.02, DurationDays: 7}), 2, 0)
	src := random.NewSource(11)
	events := claim.DrawEvents(src, p.Sections, 1998, 3)
	book := fixedBook(20000, 20000, 0, 1) // covers start through 1998 and end in 1999
	claims := claim.NewClaimSimulator(p).WithEvents(events).WithWindow(1998, 3).
		Simulate(src.Split("claims"), book)
	// The expected count, given the drawn events.
	want := 0.0
	for _, e := range events[2] {
		for _, pol := range book {
			from, to := e.Start, e.End()
			if from.Before(pol.CoverStart) {
				from = pol.CoverStart
			}
			if end := pol.CoverEnd.AddDays(1); end.Before(to) {
				to = end
			}
			if d := shared.DaysBetween(from, to); d > 0 {
				want += 0.02 * float64(d) / float64(e.Days)
			}
		}
	}
	if got := float64(len(claims)); math.Abs(got-want) > 4*math.Sqrt(want) {
		t.Errorf("weather claims = %v, want ~%v", got, want)
	}
}
