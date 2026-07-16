// Package refdata embeds the Schedule P reference datasets so the compiled
// binary can evaluate realism without access to the repository.
package refdata

import "embed"

//go:embed ppauto_pos98-07/*.json
var Files embed.FS
