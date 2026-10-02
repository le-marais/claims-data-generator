package schedulep

import (
	"fmt"
	"io/fs"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// LoadPools reads each reference line's file from fsys, named by files under
// the line's ID, and selects the line's pool with its criteria. The pools are
// keyed by line ID.
func LoadPools(fsys fs.FS, files map[string]string, lines []application.ReferenceLine) (map[string]application.ReferencePool, error) {
	pools := make(map[string]application.ReferencePool, len(lines))
	for _, line := range lines {
		name, ok := files[line.ID]
		if !ok {
			return nil, fmt.Errorf("reference line %q: no reference file", line.ID)
		}
		all, err := LoadFS(fsys, name)
		if err != nil {
			return nil, err
		}
		pools[line.ID] = application.ReferencePool{Line: line, Refs: triangle.SelectReferences(all, line.Criteria)}
	}
	return pools, nil
}
