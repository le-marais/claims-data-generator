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
	ClassID  int          `json:"ClassId"`
	Paid     triangleJSON `json:"PaidTriangle"`
	Incurred triangleJSON `json:"IncurredTriangle"`
	// FutureIncurred is the incurred development reported after the
	// triangle's valuation date, as incremental amounts per origin year.
	FutureIncurred []triangleRow `json:"FutureIncurred"`
	EarnedPremium  []premiumJSON `json:"EarnedPremium"`
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

// LoadFile reads one reference company file from disk, with a bare company
// name (the file stem).
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
	paid, err := toTriangle(f.Paid)
	if err != nil {
		return triangle.ReferenceSet{}, fmt.Errorf("paid triangle: %w", err)
	}
	incurred, err := toTriangle(f.Incurred)
	if err != nil {
		return triangle.ReferenceSet{}, fmt.Errorf("incurred triangle: %w", err)
	}
	developed, err := develop(incurred, f.FutureIncurred)
	if err != nil {
		return triangle.ReferenceSet{}, fmt.Errorf("future incurred: %w", err)
	}
	return triangle.ReferenceSet{
		Name:              strings.TrimSuffix(name, ".json"),
		Paid:              paid,
		Incurred:          incurred,
		EarnedPremium:     ep,
		DevelopedIncurred: developed,
	}, nil
}

// develop completes a cumulative triangle with later incremental development,
// returning a new triangle; the input is not modified. With no later
// development it returns the zero Triangle, which tells the comparison to
// fall back to the triangle itself.
func develop(tri triangle.Triangle, future []triangleRow) (triangle.Triangle, error) {
	if len(future) == 0 {
		return triangle.Triangle{}, nil
	}
	out := triangle.Triangle{StartYear: tri.StartYear, Cells: make([][]float64, len(tri.Cells))}
	for i, row := range tri.Cells {
		out.Cells[i] = append([]float64(nil), row...)
	}
	for _, f := range future {
		i := f.Year - tri.StartYear
		if i < 0 || i >= len(out.Cells) {
			return triangle.Triangle{}, fmt.Errorf("origin year %d is not in the triangle", f.Year)
		}
		row := out.Cells[i]
		if len(row) == 0 {
			return triangle.Triangle{}, fmt.Errorf("origin year %d has no valued development to extend", f.Year)
		}
		cum := row[len(row)-1]
		for _, v := range f.Values {
			cum += v
			row = append(row, cum)
		}
		out.Cells[i] = row
	}
	return out, nil
}

// LoadFS reads every reference company file in dir of fsys, files sorted by
// name for determinism. Company names are the bare file stem (for example
// "10007").
func LoadFS(fsys fs.FS, dir string) ([]triangle.ReferenceSet, error) {
	return loadDirFS(fsys, dir)
}

// LoadDir reads every reference company file in a directory on disk, sorted
// by file name for determinism, with bare company names.
func LoadDir(dir string) ([]triangle.ReferenceSet, error) {
	clean := filepath.Clean(dir)
	refs, err := loadDirFS(os.DirFS(clean), ".")
	if errors.Is(err, errNoReferenceFiles) {
		return nil, fmt.Errorf("%w in %s", errNoReferenceFiles, dir)
	}
	return refs, err
}

func loadDirFS(fsys fs.FS, dir string) ([]triangle.ReferenceSet, error) {
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
		refs = append(refs, ref)
	}
	return refs, nil
}

func toTriangle(t triangleJSON) (triangle.Triangle, error) {
	rows := t.TriangleValues
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Year < rows[j].Year })
	tri := triangle.Triangle{Cells: make([][]float64, len(rows))}
	if len(rows) > 0 {
		tri.StartYear = rows[0].Year
	}
	for i, r := range rows {
		if r.Year != rows[0].Year+i {
			return triangle.Triangle{}, fmt.Errorf("non-contiguous origin years: expected %d, got %d", rows[0].Year+i, r.Year)
		}
		tri.Cells[i] = r.Values
	}
	return tri, nil
}
