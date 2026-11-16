package claim_test

import (
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func day(y int, m time.Month, d int) shared.Date { return shared.NewDate(y, m, d) }

func mustSchedule(t *testing.T, pts ...lob.FrequencyChange) *claim.FrequencySchedule {
	t.Helper()
	s, err := claim.NewFrequencySchedule(pts)
	if err != nil || s == nil {
		t.Fatalf("schedule = %v, %v", s, err)
	}
	return s
}

func TestFrequencyScheduleFactor(t *testing.T) {
	ramp := mustSchedule(t, lob.FrequencyChange{Date: "2006-01-01", Factor: 1}, lob.FrequencyChange{Date: "2006-12-31", Factor: 1.75})
	jump := mustSchedule(t, lob.FrequencyChange{Date: "2006-01-01", Factor: 2})
	cases := []struct {
		name string
		s    *claim.FrequencySchedule
		d    shared.Date
		want float64
	}{
		{"ramp before first", ramp, day(2005, 12, 31), 1},
		{"ramp at first", ramp, day(2006, 1, 1), 1},
		{"ramp midway", ramp, day(2006, 7, 1), 1 + 0.75*181/364},
		{"ramp at last", ramp, day(2006, 12, 31), 1.75},
		{"ramp after last", ramp, day(2007, 6, 1), 1.75},
		{"jump before", jump, day(2005, 12, 31), 1},
		{"jump at", jump, day(2006, 1, 1), 2},
	}
	for _, c := range cases {
		if got := c.s.Factor(c.d); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("%s: Factor(%v) = %v, want %v", c.name, c.d, got, c.want)
		}
	}
}

func TestFrequencyScheduleSum(t *testing.T) {
	jump := mustSchedule(t, lob.FrequencyChange{Date: "2006-01-01", Factor: 2})
	if got := jump.Sum(day(2005, 12, 30), 4); got != 1+1+2+2 {
		t.Errorf("Sum = %v, want 6", got)
	}
	leap := mustSchedule(t, lob.FrequencyChange{Date: "2004-01-01", Factor: 3})
	if got := leap.Sum(day(2004, 2, 28), 3); got != 9 { // 28, 29 Feb, 1 Mar
		t.Errorf("leap Sum = %v, want 9", got)
	}
}

func TestFrequencySchedulePick(t *testing.T) {
	// Factors 0, 0, 1, 3 over four days.
	s := mustSchedule(t,
		lob.FrequencyChange{Date: "2006-01-01", Factor: 0},
		lob.FrequencyChange{Date: "2006-01-02", Factor: 0},
		lob.FrequencyChange{Date: "2006-01-03", Factor: 1},
		lob.FrequencyChange{Date: "2006-01-04", Factor: 3},
	)
	first := day(2006, 1, 1)
	for u, want := range map[float64]int{0: 2, 0.24: 2, 0.26: 3, 0.999: 3} {
		if got := s.Pick(first, 4, u); got != want {
			t.Errorf("Pick(u=%v) = %d, want %d", u, got, want)
		}
	}
}

func TestNewFrequencySchedule(t *testing.T) {
	pt := func(d string, f float64) lob.FrequencyChange { return lob.FrequencyChange{Date: d, Factor: f} }
	cases := []struct {
		name    string
		pts     []lob.FrequencyChange
		wantNil bool
		wantErr bool
	}{
		{"nil", nil, true, false},
		{"empty", []lob.FrequencyChange{}, true, false},
		{"all ones", []lob.FrequencyChange{pt("2006-01-01", 1), pt("2006-06-01", 1)}, true, false},
		{"zero factor", []lob.FrequencyChange{pt("2006-01-01", 0)}, false, false},
		{"equal dates", []lob.FrequencyChange{pt("2006-01-01", 1), pt("2006-01-01", 2)}, false, true},
		{"decreasing dates", []lob.FrequencyChange{pt("2006-02-01", 1), pt("2006-01-01", 2)}, false, true},
		{"bad date", []lob.FrequencyChange{pt("01/02/2006", 2)}, false, true},
		{"NaN", []lob.FrequencyChange{pt("2006-01-01", math.NaN())}, false, true},
		{"Inf", []lob.FrequencyChange{pt("2006-01-01", math.Inf(1))}, false, true},
		{"negative", []lob.FrequencyChange{pt("2006-01-01", -0.5)}, false, true},
	}
	for _, c := range cases {
		s, err := claim.NewFrequencySchedule(c.pts)
		if (err != nil) != c.wantErr || (s == nil) != (c.wantNil || c.wantErr) {
			t.Errorf("%s: got %v, %v", c.name, s, err)
		}
	}
}

// withSchedule returns params whose section carries the schedule.
func withSchedule(p lob.ClaimParams, section int, pts ...lob.FrequencyChange) lob.ClaimParams {
	p.Sections = slices.Clone(p.Sections)
	p.Sections[section].FrequencyChanges = pts
	return p
}

// threeYearBook has policies starting through 1998 to 2000, so each calendar
// year of 1999 and 2000 is covered by about the same number of policies.
func threeYearBook(n int) []policy.Policy {
	book := fixedBook(n, 20000, 0, 1.0)
	for i := range book {
		book[i].CoverStart = day(1998, 1, 1).AddDays(i % 1095)
		book[i].CoverEnd = book[i].CoverStart.AddDays(364)
	}
	return book
}

func inYear(claims []claim.Claim, year int) int {
	n := 0
	for _, c := range claims {
		if c.OccurrenceDate.Year() == year {
			n++
		}
	}
	return n
}

func TestFrequencyIdentityScheduleMatchesNoSchedule(t *testing.T) {
	book := fixedBook(3000, 20000, 300, 1.0)
	plain := claim.NewClaimSimulator(params()).Simulate(random.NewSource(3), book)
	ones := withSchedule(params(), ownDamage,
		lob.FrequencyChange{Date: "1998-01-01", Factor: 1}, lob.FrequencyChange{Date: "1999-01-01", Factor: 1})
	got := claim.NewClaimSimulator(ones).Simulate(random.NewSource(3), book)
	if !reflect.DeepEqual(plain, got) {
		t.Fatal("an all-ones schedule changed the claims")
	}
}

func TestFrequencyJumpScalesOccurrences(t *testing.T) {
	p := only(params(), ownDamage, 0.3)
	book := threeYearBook(20000)
	base := claim.NewClaimSimulator(p).WithWindow(1998, 3).Simulate(random.NewSource(4), book)
	jumped := claim.NewClaimSimulator(withSchedule(p, ownDamage, lob.FrequencyChange{Date: "2000-01-01", Factor: 2})).
		WithWindow(1998, 3).Simulate(random.NewSource(4), book)
	ratio := (float64(inYear(jumped, 2000)) / float64(inYear(jumped, 1999))) /
		(float64(inYear(base, 2000)) / float64(inYear(base, 1999)))
	if ratio < 1.9 || ratio > 2.1 {
		t.Errorf("2000 over 1999 ratio is %v times the unscheduled ratio, want 1.9-2.1", ratio)
	}
}

func TestFrequencyZeroFactorDaysHaveNoClaims(t *testing.T) {
	p := withSchedule(only(params(), ownDamage, 0.5), ownDamage,
		lob.FrequencyChange{Date: "1999-01-01", Factor: 0}, lob.FrequencyChange{Date: "1999-07-01", Factor: 0})
	claims := claim.NewClaimSimulator(p).WithWindow(1998, 3).Simulate(random.NewSource(5), threeYearBook(6000))
	if len(claims) == 0 {
		t.Fatal("no claims")
	}
	for _, c := range claims {
		if !c.OccurrenceDate.Before(day(1999, 1, 1)) && c.OccurrenceDate.Before(day(1999, 7, 1)) {
			t.Fatalf("claim occurred %v inside the zero-factor window", c.OccurrenceDate)
		}
	}
}

func TestFrequencyScheduleIsolatedToItsSection(t *testing.T) {
	book := fixedBook(3000, 20000, 300, 1.0)
	plain := claim.NewClaimSimulator(params()).Simulate(random.NewSource(6), book)
	sched := withSchedule(params(), thirdParty, lob.FrequencyChange{Date: "1998-06-01", Factor: 3})
	got := claim.NewClaimSimulator(sched).Simulate(random.NewSource(6), book)
	pick := func(cs []claim.Claim) []claim.Claim {
		var out []claim.Claim
		for _, c := range cs {
			if c.Section == ownDamage {
				c.ID = 0 // IDs follow report order across sections
				out = append(out, c)
			}
		}
		return out
	}
	if a, b := pick(plain), pick(got); len(a) == 0 || !reflect.DeepEqual(a, b) {
		t.Fatalf("own damage moved: %d vs %d claims", len(a), len(b))
	}
}

func TestFrequencyOnEventsSectionSaturatesAtTheCap(t *testing.T) {
	book := fixedBook(2000, 20000, 0, 1.0)
	for i := range book {
		book[i].CoverStart = day(1998, 1, 1)
		book[i].CoverEnd = book[i].CoverStart.AddDays(364)
	}
	run := func(factor float64) (claims []claim.Claim, events int) {
		p := only(weather(params(), hail), 2, 0)
		p = withSchedule(p, 2, lob.FrequencyChange{Date: "1990-01-01", Factor: factor})
		src := random.NewSource(7)
		set := claim.DrawEvents(src, p.Sections, 1998, 1)
		return claim.NewClaimSimulator(p).WithEvents(set).WithWindow(1998, 1).Simulate(src.Split("claims"), book), len(set[2])
	}
	hit, events := run(1000)
	if events == 0 || len(hit) != events*len(book) {
		t.Errorf("factor 1000: %d claims, want every policy hit by each of %d events", len(hit), events)
	}
	if none, _ := run(0); len(none) != 0 {
		t.Errorf("factor 0: %d event claims, want none", len(none))
	}
}
