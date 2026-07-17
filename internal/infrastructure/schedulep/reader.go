// Package schedulep reads the Schedule P reference datasets (per-company
// paid and incurred triangles with earned premium) used to assess the
// realism of generated data.
package schedulep

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// errNoReferenceFiles lets LoadDir rewrite the location in the message.
var errNoReferenceFiles = errors.New("no reference files found")

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

// LoadFile reads one reference company file from disk.
func LoadFile(path string) (triangle.ReferenceSet, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return triangle.ReferenceSet{}, fmt.Errorf("reading reference file: %w", err)
	}
	return parse(filepath.Base(path), b)
}

func parse(name string, b []byte) (triangle.ReferenceSet, error) {
	var f fileJSON
	if err := json.Unmarshal(b, &f); err != nil {
		return triangle.ReferenceSet{}, fmt.Errorf("parsing %s: %w", name, err)
	}
	ep := make([]float64, 0, len(f.EarnedPremium))
	sort.Slice(f.EarnedPremium, func(i, j int) bool { return f.EarnedPremium[i].Year < f.EarnedPremium[j].Year })
	for _, p := range f.EarnedPremium {
		ep = append(ep, p.Amount)
	}
	return triangle.ReferenceSet{
		Name:          strings.TrimSuffix(name, ".json"),
		Paid:          toTriangle(f.Paid),
		Incurred:      toTriangle(f.Incurred),
		EarnedPremium: ep,
	}, nil
}

// LoadFS reads every reference company file in each dir of fsys, in the
// order the dirs are given, files sorted by name within each dir for
// determinism. Names are qualified with the dataset's vintage directory
// (the parent of dir), e.g. "dec2025/10007", so companies that appear in
// more than one vintage stay distinct.
func LoadFS(fsys fs.FS, dirs ...string) ([]triangle.ReferenceSet, error) {
	var refs []triangle.ReferenceSet
	for _, dir := range dirs {
		loaded, err := loadDirFS(fsys, dir, path.Base(path.Dir(dir)))
		if err != nil {
			return nil, err
		}
		refs = append(refs, loaded...)
	}
	return refs, nil
}

// LoadDir reads every reference company file in a directory on disk, sorted
// by file name for determinism, with names qualified by the vintage
// directory (the parent of dir).
func LoadDir(dir string) ([]triangle.ReferenceSet, error) {
	clean := filepath.Clean(dir)
	refs, err := loadDirFS(os.DirFS(clean), ".", filepath.Base(filepath.Dir(clean)))
	if errors.Is(err, errNoReferenceFiles) {
		return nil, fmt.Errorf("%w in %s", errNoReferenceFiles, dir)
	}
	return refs, err
}

func loadDirFS(fsys fs.FS, dir, vintage string) ([]triangle.ReferenceSet, error) {
	names, err := fs.Glob(fsys, path.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%w in %s", errNoReferenceFiles, dir)
	}
	sort.Strings(names)
	refs := make([]triangle.ReferenceSet, 0, len(names))
	for _, n := range names {
		b, err := fs.ReadFile(fsys, n)
		if err != nil {
			return nil, fmt.Errorf("reading reference file: %w", err)
		}
		ref, err := parse(path.Base(n), b)
		if err != nil {
			return nil, err
		}
		ref.Name = vintage + "/" + ref.Name
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
