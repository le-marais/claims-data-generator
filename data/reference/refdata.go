// Package refdata embeds the Schedule P reference data so the compiled
// binary can evaluate realism without access to the repository.
package refdata

import "embed"

//go:embed "schedule p/ppauto_pos98-07.csv" "schedule p/comauto_pos_98-07.csv"
var Files embed.FS

// LineFiles names the embedded file of each Schedule P line the realism gate
// scores against, keyed by the application's reference line IDs: the CAS
// loss reserving database's private passenger auto and commercial auto
// liability companies, accident years 1998-2007 (see README.md).
var LineFiles = map[string]string{
	"private_passenger_auto": "schedule p/ppauto_pos98-07.csv",
	"commercial_auto":        "schedule p/comauto_pos_98-07.csv",
}
