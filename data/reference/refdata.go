// Package refdata embeds the Schedule P reference datasets so the compiled
// binary can evaluate realism without access to the repository.
package refdata

import "embed"

//go:embed "schedule p/dec2025/ppauto_pos98-07/*.json"
var Files embed.FS

// PersonalMotorDir is the embedded dataset backing the personal motor
// reference pool. dec2025 spans accident years 1998-2007; the companies are
// hand-curated (see data/reference/gr-code-list.md).
const PersonalMotorDir = "schedule p/dec2025/ppauto_pos98-07"
