package claim_test

import (
	"reflect"
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// claimKey identifies a claim across runs whose dates differ: deferral moves
// report and close dates, and so registration order and IDs, but never the
// occurrence or the cost.
type claimKey struct {
	policy, section int
	occurrence      shared.Date
	ultimate        shared.Money
}

func byKey(t *testing.T, claims []claim.Claim) map[claimKey]claim.Claim {
	t.Helper()
	out := make(map[claimKey]claim.Claim, len(claims))
	for _, c := range claims {
		k := claimKey{c.PolicyID, c.Section, c.OccurrenceDate, c.Episodes[0].Ultimate}
		if _, dup := out[k]; dup {
			t.Fatalf("duplicate claim key %+v", k)
		}
		out[k] = c
	}
	return out
}

// holidayRun simulates the same book with and without a seasonal holiday.
func holidayRun(t *testing.T, h lob.SeasonalHolidayParams) (base, deferred map[claimKey]claim.Claim) {
	t.Helper()
	p := params()
	p.NilProbability = 0.2
	book := fixedBook(6000, 20000, 0, 1.0)
	b := claim.NewClaimSimulator(p).WithPaymentDelay(7).Simulate(random.NewSource(61), book)
	d := claim.NewClaimSimulator(p).WithPaymentDelay(7).WithSeasonalHoliday(h).Simulate(random.NewSource(61), book)
	if len(b) != len(d) {
		t.Fatalf("deferral changed the claim count: %d, want %d", len(d), len(b))
	}
	return byKey(t, b), byKey(t, d)
}

func TestSeasonalHolidayDefersWindowReports(t *testing.T) {
	h := lob.SeasonalHolidayParams{Hemisphere: lob.Northern, ReportShare: 1}
	base, deferred := holidayRun(t, h)
	moved := 0
	for k, b := range base {
		d := deferred[k]
		want := b.ReportDate()
		if h.InWindow(want) {
			want = want.AddMonths(1)
			moved++
		}
		if d.ReportDate() != want {
			t.Fatalf("report %v deferred to %v, want %v", b.ReportDate(), d.ReportDate(), want)
		}
		// The close lag runs from the report, so the claim's timeline moves
		// with it.
		if got, wantLag := shared.DaysBetween(d.ReportDate(), d.CloseDate()), shared.DaysBetween(b.ReportDate(), b.CloseDate()); got != wantLag {
			t.Fatalf("report-to-close lag %d after deferral, want %d", got, wantLag)
		}
	}
	if moved == 0 {
		t.Fatal("no report fell in the window; the test proves nothing")
	}
}

func TestSeasonalHolidayDefersPayingWindowCloses(t *testing.T) {
	h := lob.SeasonalHolidayParams{Hemisphere: lob.Southern, PaymentShare: 1}
	base, deferred := holidayRun(t, h)
	moved, nilInWindow := 0, 0
	for k, b := range base {
		d := deferred[k]
		if d.ReportDate() != b.ReportDate() {
			t.Fatalf("report moved at report share 0: %v to %v", b.ReportDate(), d.ReportDate())
		}
		want := b.CloseDate()
		switch {
		case b.Nil() && h.InWindow(want):
			nilInWindow++
		case h.InWindow(want):
			want = want.AddMonths(1)
			moved++
		}
		if d.CloseDate() != want {
			t.Fatalf("close %v (nil %v) became %v, want %v", b.CloseDate(), b.Nil(), d.CloseDate(), want)
		}
	}
	if moved == 0 || nilInWindow == 0 {
		t.Fatalf("moved %d paying and kept %d nil closes; the test needs both", moved, nilInWindow)
	}
}

func TestSeasonalHolidayOffChangesNothing(t *testing.T) {
	p := params()
	book := fixedBook(2000, 20000, 0, 1.0)
	want := claim.NewClaimSimulator(p).Simulate(random.NewSource(62), book)
	off := lob.SeasonalHolidayParams{Hemisphere: lob.NoHoliday, ReportShare: 1, PaymentShare: 1}
	got := claim.NewClaimSimulator(p).WithSeasonalHoliday(off).Simulate(random.NewSource(62), book)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("a switched-off seasonal holiday changed the claims")
	}
	gotReopen := claim.NewReopenSimulator(reopeningParams()).WithSeasonalHoliday(off).Apply(random.NewSource(62), append([]claim.Claim(nil), want...))
	wantReopen := claim.NewReopenSimulator(reopeningParams()).Apply(random.NewSource(62), append([]claim.Claim(nil), want...))
	if !reflect.DeepEqual(gotReopen, wantReopen) {
		t.Fatal("a switched-off seasonal holiday changed the reopens")
	}
}

func TestSeasonalHolidayDefersWindowReopenCloses(t *testing.T) {
	p := reopeningParams()
	h := lob.SeasonalHolidayParams{Hemisphere: lob.Northern, PaymentShare: 1}
	claims := claim.NewClaimSimulator(p).Simulate(random.NewSource(63), fixedBook(6000, 20000, 0, 1.0))
	base := claim.NewReopenSimulator(p).Apply(random.NewSource(63), append([]claim.Claim(nil), claims...))
	deferred := claim.NewReopenSimulator(p).WithSeasonalHoliday(h).Apply(random.NewSource(63), append([]claim.Claim(nil), claims...))
	moved := 0
	for i, b := range base {
		d := deferred[i]
		if len(b.Episodes) != len(d.Episodes) {
			t.Fatalf("claim %d: deferral changed whether it reopens", b.ID)
		}
		if !b.Reopened() {
			continue
		}
		if d.Episodes[1].Open != b.Episodes[1].Open {
			t.Fatalf("claim %d: reopen date moved", b.ID)
		}
		want := b.CloseDate()
		if h.InWindow(want) {
			want = want.AddMonths(1)
			moved++
		}
		if d.CloseDate() != want {
			t.Fatalf("claim %d: second close %v became %v, want %v", b.ID, b.CloseDate(), d.CloseDate(), want)
		}
	}
	if moved == 0 {
		t.Fatal("no reopen closed in the window; the test proves nothing")
	}
}

func TestSeasonalHolidayDefersWindowReopens(t *testing.T) {
	p := reopeningParams()
	h := lob.SeasonalHolidayParams{Hemisphere: lob.Northern, ReportShare: 1}
	claims := claim.NewClaimSimulator(p).Simulate(random.NewSource(64), fixedBook(6000, 20000, 0, 1.0))
	base := claim.NewReopenSimulator(p).Apply(random.NewSource(64), append([]claim.Claim(nil), claims...))
	deferred := claim.NewReopenSimulator(p).WithSeasonalHoliday(h).Apply(random.NewSource(64), append([]claim.Claim(nil), claims...))
	moved := 0
	for i, b := range base {
		d := deferred[i]
		if len(b.Episodes) != len(d.Episodes) {
			t.Fatalf("claim %d: deferral changed whether it reopens", b.ID)
		}
		if !b.Reopened() {
			continue
		}
		bo, do := b.Episodes[1], d.Episodes[1]
		want := bo.Open
		if h.InWindow(want) {
			want = want.AddMonths(1)
			moved++
		}
		if do.Open != want {
			t.Fatalf("claim %d: reopen %v became %v, want %v", b.ID, bo.Open, do.Open, want)
		}
		// The second close lag runs from the reopen, so the episode moves
		// with it and its cost stays.
		if shared.DaysBetween(do.Open, do.Close) != shared.DaysBetween(bo.Open, bo.Close) || do.Ultimate != bo.Ultimate {
			t.Fatalf("claim %d: reopen episode changed beyond its dates", b.ID)
		}
	}
	if moved == 0 {
		t.Fatal("no reopen fell in the window; the test proves nothing")
	}
}
