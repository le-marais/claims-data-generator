// Package refdata embeds the Schedule P reference data so the compiled
// binary can evaluate realism without access to the repository.
package refdata

import "embed"

//go:embed "schedule p/ppauto_pos98-07.csv"
var Files embed.FS

// PersonalMotorFile is the embedded file backing the personal motor
// reference: the CAS loss reserving database's private passenger auto
// liability companies, accident years 1998-2007 (see README.md).
const PersonalMotorFile = "schedule p/ppauto_pos98-07.csv"
