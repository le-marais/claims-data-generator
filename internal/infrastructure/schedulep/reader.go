// Package schedulep reads the CAS loss reserving database's Schedule P
// files: one CSV per line of business, with one row per company, accident
// year and development lag. Losses are net of reinsurance; premium is direct
// and net. The reference sets built from them back the realism gate.
package schedulep

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// numeric are the CAS columns read as numbers.
var numeric = []string{"GRCODE", "AccidentYear", "DevelopmentLag", "IncurredLosses", "CumPaidLoss", "EarnedPremDIR", "EarnedPremNet"}

type cell struct {
	incurred, paid, direct, net float64
}

type company struct {
	code  int
	name  string
	cells map[[2]int]cell // keyed by accident year and development lag
}

// LoadFS reads one CAS Schedule P file from fsys. See parse.
func LoadFS(fsys fs.FS, name string) ([]triangle.ReferenceSet, error) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("reading reference file: %w", err)
	}
	return parseNamed(name, b)
}

// LoadFile reads one CAS Schedule P file from disk. See parse.
func LoadFile(path string) ([]triangle.ReferenceSet, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading reference file: %w", err)
	}
	return parseNamed(path, b)
}

func parseNamed(name string, b []byte) ([]triangle.ReferenceSet, error) {
	refs, err := parse(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return refs, nil
}

// parse reads the rows into one reference set per company that has every
// development lag of every accident year in the file, so both its triangle at
// the last accident year's valuation and its later development are known.
// Companies missing any cell are left out. The sets are sorted by company
// code.
func parse(r io.Reader) ([]triangle.ReferenceSet, error) {
	cr := csv.NewReader(r)
	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, errors.New("empty file")
	}
	if err != nil {
		return nil, err
	}
	col := make(map[string]int, len(header))
	for i, name := range header {
		col[name] = i
	}
	for _, name := range append([]string{"GRNAME"}, numeric...) {
		if _, ok := col[name]; !ok {
			return nil, fmt.Errorf("missing column %s", name)
		}
	}
	companies := map[int]*company{}
	firstYear, lastYear := math.MaxInt, math.MinInt
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		v := make(map[string]float64, len(numeric))
		for _, name := range numeric {
			x, err := strconv.ParseFloat(rec[col[name]], 64)
			if err != nil {
				return nil, fmt.Errorf("line %d: %s: %w", line, name, err)
			}
			v[name] = x
		}
		code, year, lag := int(v["GRCODE"]), int(v["AccidentYear"]), int(v["DevelopmentLag"])
		co := companies[code]
		if co == nil {
			co = &company{code: code, name: strings.TrimSpace(rec[col["GRNAME"]]), cells: map[[2]int]cell{}}
			companies[code] = co
		}
		key := [2]int{year, lag}
		if _, dup := co.cells[key]; dup {
			return nil, fmt.Errorf("line %d: duplicate row for company %d, accident year %d, lag %d", line, code, year, lag)
		}
		co.cells[key] = cell{incurred: v["IncurredLosses"], paid: v["CumPaidLoss"], direct: v["EarnedPremDIR"], net: v["EarnedPremNet"]}
		firstYear, lastYear = min(firstYear, year), max(lastYear, year)
	}
	if len(companies) == 0 {
		return nil, errors.New("no rows")
	}
	codes := make([]int, 0, len(companies))
	for code := range companies {
		codes = append(codes, code)
	}
	slices.Sort(codes)
	years := lastYear - firstYear + 1
	var refs []triangle.ReferenceSet
	for _, code := range codes {
		if ref, ok := companies[code].referenceSet(firstYear, years); ok {
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		return nil, errors.New("no company has every accident year to the last development lag")
	}
	return refs, nil
}

// referenceSet builds the company's paid and incurred triangles valued at the
// end of the last accident year, its incurred developed to the last lag, and
// its premium by accident year. ok is false when any cell is missing.
func (c *company) referenceSet(firstYear, years int) (triangle.ReferenceSet, bool) {
	ref := triangle.ReferenceSet{
		Name:              strconv.Itoa(c.code),
		Company:           c.name,
		Paid:              triangle.Triangle{StartYear: firstYear, Cells: make([][]float64, years)},
		Incurred:          triangle.Triangle{StartYear: firstYear, Cells: make([][]float64, years)},
		DevelopedIncurred: triangle.Triangle{StartYear: firstYear, Cells: make([][]float64, years)},
		EarnedPremium:     make([]float64, years),
		DirectPremium:     make([]float64, years),
	}
	for i := range years {
		for lag := 1; lag <= years; lag++ {
			v, ok := c.cells[[2]int{firstYear + i, lag}]
			if !ok {
				return triangle.ReferenceSet{}, false
			}
			if lag == 1 {
				ref.EarnedPremium[i], ref.DirectPremium[i] = v.net, v.direct
			}
			ref.DevelopedIncurred.Cells[i] = append(ref.DevelopedIncurred.Cells[i], v.incurred)
			if lag <= years-i {
				ref.Paid.Cells[i] = append(ref.Paid.Cells[i], v.paid)
				ref.Incurred.Cells[i] = append(ref.Incurred.Cells[i], v.incurred)
			}
		}
	}
	return ref, true
}
