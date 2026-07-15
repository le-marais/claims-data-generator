// Package schedulep reads the Schedule P reference datasets (per-company
// paid and incurred triangles with earned premium) used to assess the
// realism of generated data.
package schedulep

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

type fileJSON struct {
	ClassID       int           `json:"ClassId"`
	Paid          triangleJSON  `json:"PaidTriangle"`
	Incurred      triangleJSON  `json:"IncurredTriangle"`
	EarnedPremium []premiumJSON `json:"EarnedPremium"`
}

type triangleJSON struct {
	TriangleValues []triangleRow `json:"TriangleValues"`
}

// triangleRow decodes the [year, [values...]] pair encoding.
type triangleRow struct {
	Year   int
	Values []float64
}

func (r *triangleRow) UnmarshalJSON(b []byte) error {
	var raw [2]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(raw[0], &r.Year); err != nil {
		return err
	}
	return json.Unmarshal(raw[1], &r.Values)
}

// premiumJSON decodes the [year, amount] pair encoding.
type premiumJSON struct {
	Year   int
	Amount float64
}

func (p *premiumJSON) UnmarshalJSON(b []byte) error {
	var raw [2]float64
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	p.Year = int(raw[0])
	p.Amount = raw[1]
	return nil
}

// LoadFile reads one reference company file.
func LoadFile(path string) (triangle.ReferenceSet, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return triangle.ReferenceSet{}, fmt.Errorf("reading reference file: %w", err)
	}
	var f fileJSON
	if err := json.Unmarshal(b, &f); err != nil {
		return triangle.ReferenceSet{}, fmt.Errorf("parsing %s: %w", filepath.Base(path), err)
	}
	ep := make([]float64, 0, len(f.EarnedPremium))
	sort.Slice(f.EarnedPremium, func(i, j int) bool { return f.EarnedPremium[i].Year < f.EarnedPremium[j].Year })
	for _, p := range f.EarnedPremium {
		ep = append(ep, p.Amount)
	}
	return triangle.ReferenceSet{
		Name:          strings.TrimSuffix(filepath.Base(path), ".json"),
		Paid:          toTriangle(f.Paid),
		Incurred:      toTriangle(f.Incurred),
		EarnedPremium: ep,
	}, nil
}

// LoadDir reads every reference company file in a directory, sorted by
// file name for determinism.
func LoadDir(dir string) ([]triangle.ReferenceSet, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no reference files found in %s", dir)
	}
	sort.Strings(paths)
	refs := make([]triangle.ReferenceSet, 0, len(paths))
	for _, p := range paths {
		ref, err := LoadFile(p)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func toTriangle(t triangleJSON) triangle.Triangle {
	rows := t.TriangleValues
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Year < rows[j].Year })
	tri := triangle.Triangle{Cells: make([][]float64, len(rows))}
	if len(rows) > 0 {
		tri.StartYear = rows[0].Year
	}
	for i, r := range rows {
		tri.Cells[i] = r.Values
	}
	return tri
}
