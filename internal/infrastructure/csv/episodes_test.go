package csv_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	csvout "github.com/le-marais/claimsgen/internal/infrastructure/csv"
)

func TestWriteEpisodesOneRowPerEpisode(t *testing.T) {
	d := func(m time.Month, day int) shared.Date { return shared.NewDate(1998, m, day) }
	ds := application.Dataset{Claims: []claim.Claim{
		{ID: 1, Episodes: []claim.Episode{{Open: d(3, 5), Close: d(3, 5), Nil: true}}},
		{ID: 2, Episodes: []claim.Episode{{Open: d(3, 6), Close: d(4, 1)}, {Open: d(6, 2), Close: d(7, 3)}}},
	}}
	dir := t.TempDir()
	if err := csvout.WriteEpisodes(dir, ds); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "claim_episodes.csv"))
	if err != nil {
		t.Fatal(err)
	}
	want := "claim_id,episode,open_date,close_date\n" +
		"1,1,1998-03-05,1998-03-05\n" +
		"2,1,1998-03-06,1998-04-01\n" +
		"2,2,1998-06-02,1998-07-03\n"
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
