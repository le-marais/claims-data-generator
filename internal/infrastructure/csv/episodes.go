package csv

import (
	"fmt"

	"github.com/le-marais/claimsgen/internal/application"
)

// WriteEpisodes writes the opt-in claim_episodes.csv into dir, creating it if
// needed. It has one row per claim episode, in claims.csv order and then
// episode order: claim_id, the 1-based episode number, and the episode's open
// and close dates. A reopening is episode 2.
func WriteEpisodes(dir string, ds application.Dataset) error {
	open, err := dirOpener(dir)
	if err != nil {
		return err
	}
	return writeEpisodes(open, ds)
}

func writeEpisodes(open opener, ds application.Dataset) error {
	type row struct{ claim, episode int }
	var rows []row
	for ci, c := range ds.Claims {
		for ei := range c.Episodes {
			rows = append(rows, row{ci, ei})
		}
	}
	return writeFile(open, "claim_episodes.csv", "claim_id,episode,open_date,close_date",
		len(rows), func(i int) string {
			c := ds.Claims[rows[i].claim]
			ep := c.Episodes[rows[i].episode]
			return fmt.Sprintf("%d,%d,%s,%s", c.ID, rows[i].episode+1, ep.Open, ep.Close)
		})
}
