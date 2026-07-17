// Package refdata embeds the Schedule P reference datasets so the compiled
// binary can evaluate realism without access to the repository.
package refdata

import "embed"

//go:embed "schedule p/dec2025/ppauto_pos98-07/*.json"
var Files embed.FS
