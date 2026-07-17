// Package refdata embeds the Schedule P reference datasets so the compiled
// binary can evaluate realism without access to the repository.
package refdata

import "embed"

//go:embed "schedule p/dec2025/ppauto_pos98-07/*.json" "schedule p/sep2011/auto_personal/*.json"
var Files embed.FS

// PersonalMotorDirs lists the embedded datasets that make up the personal
// motor reference pool, in load order. Both vintages cover the same line of
// business: dec2025 spans accident years 1998-2007, sep2011 spans 1988-1997.
var PersonalMotorDirs = []string{
	"schedule p/dec2025/ppauto_pos98-07",
	"schedule p/sep2011/auto_personal",
}
