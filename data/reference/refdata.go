// Package refdata embeds the Schedule P reference datasets so the compiled
// binary can evaluate realism without access to the repository.
package refdata

import "embed"

//go:embed "schedule p/ppauto_pos98-07/*.json"
var Files embed.FS

// PersonalMotorDir is the embedded dataset backing the personal motor
// reference pool: private passenger auto, accident years 1998-2007, from the
// December 2025 Schedule P extract. The companies are hand-curated (see
// data/reference/gr-code-list.md).
const PersonalMotorDir = "schedule p/ppauto_pos98-07"
