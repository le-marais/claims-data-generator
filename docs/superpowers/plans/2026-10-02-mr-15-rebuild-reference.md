> **OUT OF CONTEXT - do not read (2026-10-02) unless you are executing this plan:** implementation plan for MR-15, deleted once the work ships. It is not a source of truth for how the system works; for that see `README.md`, `AGENTS.md` and `docs/architecture.md`.

# Rebuild the Schedule P reference - implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close MR-15. The realism gate should score net losses against net premium, on reference companies that make a fair benchmark, chosen by rules in code rather than by a hand-kept list.

**Architecture:** Five steps, each leaving the gate green on the branch:

1. Replace the JSON reference files with the unmodified CAS CSVs, and rewrite the `schedulep` reader to read them. `ReferenceSet` gains the company name and direct premium, and `EarnedPremium` becomes net premium. The gate then scores all 121 complete private passenger auto companies on net premium. Generated output is unchanged.
2. Add `triangle.ReferenceCriteria`, the selection rules, as pure domain code. Nothing calls it yet.
3. Score loss-ratio drift relative to the pool's median drift.
4. Select the gate's pool with `application.PersonalMotorCriteria()` (45 companies), recalibrate the preset's injury settlement, run the gate at a 100k book, and refresh the golden hashes. Only this task moves generated output.
5. Docs, screenshots and the review file.

**Tech stack:** Go 1.26.4+ (`encoding/csv`, `embed`), gonum, yaml.v3, plain JS UI with no build step. Tests use `go test`.

## Global constraints

- Reproducibility is a hard invariant: the same seed and config give byte-identical output. Tasks 1, 2, 3 and 5 must leave all three golden hashes in `internal/application/golden_test.go` unchanged. Task 4 changes the preset, and only after the realism gate passes may it set the hashes, which should come out as `wantHash = "08a8309a5144813d823784698587c5b3caf78d5da56a105ddd0672771a0df4b4"`, `wantAggregateHash = "38e23b038982528cd4792558caeab4bf0ee61a0b56d10a7804f48ecf7d29dabd"` and `wantAnnualHash = "e8bfa012a050ddaa7949744514e42bdcc34cd137312108d60cb2a8922499f199"`. If they differ, find out why before pasting anything.
- Dependency direction: `domain` imports nothing outside itself, `application` orchestrates, `infrastructure` adapts. The selection rules are domain code; the personal motor pool's values live in `application`; loading the embedded file stays at the edges (`cmd`, tests).
- No new dependencies. `encoding/csv` is standard library.
- CAS amounts are in thousands of dollars. `MinMeanPremium: 5000` is $5m a year.
- The pricing target is rough: premium follows `target_loss_ratio` and the loss ratio stays emergent. Do not tune `target_loss_ratio` or the `pricing` block to pass the gate.
- Docs and comments: sentence case headers, no em dashes (use ` - `), concise and factual.
- Branch `feature/mr-15-rebuild-reference`. Commit as you go. One PR, squash merged by the maintainer. Run `go test ./...`, `go vet ./...` and `gofmt -l .` before opening it, and say so in the body. Open the PR and stop: do not merge, approve or enable auto-merge.
- Out of scope: MR-16 (case incurred from `BulkLoss`), MR-17 (cumulative development check) and MR-18 (other lines and the age-10 fold). Do not read `BulkLoss` yet.

---

### Task 1: the CAS files and a reader for them

**Files:**
- Create: `data/reference/schedule p/ppauto_pos98-07.csv`, `comauto_pos_98-07.csv`, `wkcomp_pos_98-07.csv`, `othliab_pos_98-07.csv`, `prodliab_pos_98-07.csv`, `medmal_pos_98-07.csv`
- Create: `data/reference/README.md`
- Delete: the six JSON directories under `data/reference/schedule p/`, `data/reference/gr-code-list.md`, `tools/prune-dec2025.ps1`
- Modify: `.gitattributes`, `data/reference/refdata.go`
- Modify: `internal/domain/triangle/compare.go` (`ReferenceSet`)
- Rewrite: `internal/infrastructure/schedulep/reader.go`, `internal/infrastructure/schedulep/reader_test.go`
- Modify: `cmd/claimsgen/main.go:139`, `internal/application/realism_test.go` (three loads), `internal/infrastructure/web/server_test.go:29`

**Interfaces:**
- Produces: `refdata.PersonalMotorFile = "schedule p/ppauto_pos98-07.csv"`, replacing `refdata.PersonalMotorDir`.
- Produces: `func LoadFS(fsys fs.FS, name string) ([]triangle.ReferenceSet, error)` and `func LoadFile(path string) ([]triangle.ReferenceSet, error)`. Each reads one CAS file and returns one set per company that has every development lag of every accident year, sorted by company code. `LoadDir` is removed.
- Produces: `triangle.ReferenceSet` with `Name` (the GRCODE, for example `"10007"`), `Company` (the GRNAME), `Paid`, `Incurred`, `EarnedPremium` (net), `DirectPremium` and `DevelopedIncurred`.
- Produces: the test helper `personalMotorRefs(t *testing.T) []triangle.ReferenceSet` in `internal/application/realism_test.go`.

- [ ] **Step 1: Download the CAS files and check them**

```bash
d="data/reference/schedule p"
base=https://www.casact.org/sites/default/files/2026-03
curl -fsS -o "$d/ppauto_pos98-07.csv" "$base/ppauto_pos98-07%20%281%29.csv"
for f in comauto_pos_98-07 wkcomp_pos_98-07 othliab_pos_98-07 prodliab_pos_98-07 medmal_pos_98-07; do
  curl -fsS -o "$d/$f.csv" "$base/$f.csv"
done
shasum -a 256 "$d"/*.csv
```

Expected, exactly:

```
5012bd4c9048e300669e2f4fc915850449099e159481b4c3349b574f6f00afe1  data/reference/schedule p/comauto_pos_98-07.csv
50ea237914797562661b82996765e40b1fd09784a63130463bbd4972f74dbba3  data/reference/schedule p/medmal_pos_98-07.csv
f514136de4be7b5ac114346709c309cd2464861e9fbb683051849f9fa6eadc50  data/reference/schedule p/othliab_pos_98-07.csv
6e838f1e44c67218133ef1c8e28ca2b34b9f21ba4ee94de408c412638f798d96  data/reference/schedule p/ppauto_pos98-07.csv
f1070b5a95658bfeb6719fdf4ffdabc8b7e3f97771cdb4b3f503eb7d6e486f01  data/reference/schedule p/prodliab_pos_98-07.csv
8d0b02bed0939e932f9078f65266e9a398f580f90e5227cde053dd5b520affef  data/reference/schedule p/wkcomp_pos_98-07.csv
```

If a checksum differs, CAS has republished the data. Stop and report it to the maintainer: every figure in MR-15 and this plan was measured on these bytes.

- [ ] **Step 2: Keep their bytes**

The files have CRLF line endings, and the repository normalises text to LF. Append this to `.gitattributes`:

```
# The CAS reference files are kept byte for byte, CRLF included, so they
# match the checksums in data/reference/README.md.
data/reference/**/*.csv -text
```

- [ ] **Step 3: Delete the old reference data**

```bash
git rm -r -q "data/reference/schedule p/"*_pos*98-07/ data/reference/gr-code-list.md tools/prune-dec2025.ps1
```

Then run `ls "data/reference/schedule p"` and confirm that only the six CSVs remain.

- [ ] **Step 4: Write the provenance note**

Create `data/reference/README.md`:

```markdown
# Reference data

`schedule p/` holds the Casualty Actuarial Society's loss reserving database
for accident years 1998-2007 with ten development lags (the December 2025
update), downloaded unmodified on 2026-10-02 from
https://www.casact.org/publications-research/research/research-resources/loss-reserving-data-pulled-naic-schedule-p.
`.gitattributes` keeps their bytes, CRLF line endings included, so they match
these checksums:

| File | Schedule P line | SHA-256 |
| --- | --- | --- |
| `ppauto_pos98-07.csv` | private passenger auto liability/medical (Part 1B) | `6e838f1e44c67218133ef1c8e28ca2b34b9f21ba4ee94de408c412638f798d96` |
| `comauto_pos_98-07.csv` | commercial auto/truck liability/medical (Part 1C) | `5012bd4c9048e300669e2f4fc915850449099e159481b4c3349b574f6f00afe1` |
| `wkcomp_pos_98-07.csv` | workers compensation (Part 1D) | `8d0b02bed0939e932f9078f65266e9a398f580f90e5227cde053dd5b520affef` |
| `medmal_pos_98-07.csv` | medical malpractice, claims-made (Part 1F) | `50ea237914797562661b82996765e40b1fd09784a63130463bbd4972f74dbba3` |
| `othliab_pos_98-07.csv` | other liability, occurrence (Part 1H) | `f514136de4be7b5ac114346709c309cd2464861e9fbb683051849f9fa6eadc50` |
| `prodliab_pos_98-07.csv` | products liability, occurrence (Part 1R) | `f1070b5a95658bfeb6719fdf4ffdabc8b7e3f97771cdb4b3f503eb7d6e486f01` |

Each file has one row per company (`GRCODE`, `GRNAME`), accident year and
development lag, in thousands of dollars. Losses are net of reinsurance and
include defence and cost containment: `CumPaidLoss` is paid, and
`IncurredLosses` is incurred including `BulkLoss`, the bulk and IBNR reserves.
`EarnedPremDIR`, `EarnedPremCeded` and `EarnedPremNet` are the accident year's
direct and assumed, ceded and net earned premium, repeated on every lag. Lags
after the 2007 valuation come from later annual statements, so every accident
year is known to lag 10. `Single` is 1 for a single company and 0 for a group.

Only the private passenger auto file is embedded (`refdata.go`). The realism
gate scores against the companies `application.PersonalMotorCriteria` selects
from it. The other five lines are kept for future lines of business.
```

- [ ] **Step 5: Embed the private passenger auto file**

Replace `data/reference/refdata.go` with:

```go
// Package refdata embeds the Schedule P reference data so the compiled
// binary can evaluate realism without access to the repository.
package refdata

import "embed"

//go:embed "schedule p/ppauto_pos98-07.csv"
var Files embed.FS

// PersonalMotorFile is the embedded file backing the personal motor
// reference: the CAS loss reserving database's private passenger auto
// liability companies, accident years 1998-2007 (see README.md).
const PersonalMotorFile = "schedule p/ppauto_pos98-07.csv"
```

- [ ] **Step 6: Extend `ReferenceSet`**

In `internal/domain/triangle/compare.go`, replace the `ReferenceSet` struct and its comment with:

```go
// ReferenceSet is one reference company's observed triangles and premium.
//
// Paid and incurred are net of reinsurance, as Schedule P reports them, and
// EarnedPremium is net premium to match, so the loss ratio is net over net.
// Incurred is Schedule P total incurred: paid, case, bulk and IBNR reserves.
// The generated incurred it is compared with is paid plus case plus pure IBNR
// held at its true value (AnnualSet.TotalIncurred), so unreported claims count
// on both sides. The generated side still has no bulk reserve: a reference
// company's IBNR held early and released later pulls its factors below 1,
// which a perfect IBNR does not, so the incurred check stays a loose bound.
type ReferenceSet struct {
	// Name is the company's NAIC group or company code, for example "10007".
	Name string
	// Company is the company's name as Schedule P reports it.
	Company  string
	Paid     Triangle
	Incurred Triangle
	// EarnedPremium is net earned premium by accident year, the loss
	// ratio's denominator.
	EarnedPremium []float64
	// DirectPremium is direct and assumed earned premium by accident year.
	// Its ratio to EarnedPremium tracks the company's reinsurance.
	DirectPremium []float64
	// DevelopedIncurred is Incurred completed with the company's later
	// reported development, so every origin year is valued at the same, full
	// age. The loss ratio is scored on it. The zero value means the later
	// development is not available, and Incurred is used instead.
	DevelopedIncurred Triangle
}
```

- [ ] **Step 7: Write the failing reader tests**

Replace `internal/infrastructure/schedulep/reader_test.go` with:

```go
package schedulep_test

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	refdata "github.com/le-marais/claimsgen/data/reference"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
	"github.com/le-marais/claimsgen/internal/infrastructure/schedulep"
)

const refFile = "../../../data/reference/schedule p/ppauto_pos98-07.csv"

const header = "GRCODE,GRNAME,AccidentYear,DevelopmentYear,DevelopmentLag,IncurredLosses,CumPaidLoss,BulkLoss,EarnedPremDIR,EarnedPremCeded,EarnedPremNet,Single,PostedReserves2007\r\n"

// twoYears is a two-year file in the CAS layout. Company 1 has every cell of
// the square; company 2 lacks accident year 2001 at lag 2.
const twoYears = header +
	"1,One,2000,2000,1,60,20,10,100,10,90,1,0\r\n" +
	"1,One,2000,2001,2,55,45,2,100,10,90,1,0\r\n" +
	"1,One,2001,2001,1,70,25,12,120,12,108,1,0\r\n" +
	"1,One,2001,2002,2,66,50,3,120,12,108,1,0\r\n" +
	"2,Two,2000,2000,1,30,10,5,50,0,50,1,0\r\n" +
	"2,Two,2000,2001,2,28,22,1,50,0,50,1,0\r\n" +
	"2,Two,2001,2001,1,35,12,6,60,0,60,1,0\r\n"

func load(t *testing.T, data string) ([]triangle.ReferenceSet, error) {
	t.Helper()
	return schedulep.LoadFS(fstest.MapFS{"x.csv": {Data: []byte(data)}}, "x.csv")
}

func TestLoadBuildsTheValuationTriangleAndItsDevelopment(t *testing.T) {
	refs, err := load(t, twoYears)
	if err != nil {
		t.Fatal(err)
	}
	want := []triangle.ReferenceSet{{
		Name:              "1",
		Company:           "One",
		Paid:              triangle.Triangle{StartYear: 2000, Cells: [][]float64{{20, 45}, {25}}},
		Incurred:          triangle.Triangle{StartYear: 2000, Cells: [][]float64{{60, 55}, {70}}},
		DevelopedIncurred: triangle.Triangle{StartYear: 2000, Cells: [][]float64{{60, 55}, {70, 66}}},
		EarnedPremium:     []float64{90, 108},
		DirectPremium:     []float64{100, 120},
	}}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("got %+v\nwant %+v (company 2 lacks a cell and is left out)", refs, want)
	}
}

func TestLoadRejectsBadFiles(t *testing.T) {
	for _, tc := range []struct {
		name, data, want string
	}{
		{"empty", "", "empty"},
		{"header only", header, "no rows"},
		{"missing column", "GRCODE,GRNAME,AccidentYear,DevelopmentLag,IncurredLosses,CumPaidLoss,EarnedPremDIR\r\n1,One,2000,1,1,1,1\r\n", "EarnedPremNet"},
		{"bad number", header + "1,One,2000,2000,1,x,20,10,100,10,90,1,0\r\n", "line 2: IncurredLosses"},
		{"duplicate row", twoYears + "1,One,2000,2000,1,60,20,10,100,10,90,1,0\r\n", "duplicate"},
		{"no complete company", header + "2,Two,2000,2000,1,30,10,5,50,0,50,1,0\r\n2,Two,2001,2001,1,35,12,6,60,0,60,1,0\r\n", "no company"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, tc.data)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got error %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestLoadFSErrorsOnMissingFile(t *testing.T) {
	if _, err := schedulep.LoadFS(fstest.MapFS{}, "missing.csv"); err == nil {
		t.Fatal("LoadFS on a missing file: want error, got nil")
	}
}

// 143 companies are in the file; 22 lack a cell of the ten-by-ten square.
func TestLoadFileReadsEveryCompleteCompany(t *testing.T) {
	refs, err := schedulep.LoadFile(refFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 121 {
		t.Fatalf("loaded %d reference companies, want 121", len(refs))
	}
	for _, ref := range refs {
		for i, row := range ref.DevelopedIncurred.Cells {
			if len(row) != 10 || len(ref.Paid.Cells[i]) != 10-i || len(ref.Incurred.Cells[i]) != 10-i {
				t.Fatalf("%s origin %d: developed %d, paid %d, incurred %d ages; want 10, %d, %d",
					ref.Name, 1998+i, len(row), len(ref.Paid.Cells[i]), len(ref.Incurred.Cells[i]), 10-i, 10-i)
			}
		}
	}
}

func TestLoadKnownCompany(t *testing.T) {
	refs, err := schedulep.LoadFile(refFile)
	if err != nil {
		t.Fatal(err)
	}
	var ref triangle.ReferenceSet
	for _, r := range refs {
		if r.Name == "10007" {
			ref = r
		}
	}
	if ref.Company != "Nevada General Ins Co" {
		t.Fatalf("company 10007 = %q, want Nevada General Ins Co", ref.Company)
	}
	if ref.Paid.StartYear != 1998 {
		t.Errorf("Paid.StartYear = %d, want 1998", ref.Paid.StartYear)
	}
	if got := ref.Paid.Cells[0][0]; got != 1667 {
		t.Errorf("paid 1998 lag 1 = %v, want 1667", got)
	}
	if got := ref.Paid.Cells[0][9]; got != 3422 {
		t.Errorf("paid 1998 lag 10 = %v, want 3422", got)
	}
	if got := ref.Paid.Cells[9]; !reflect.DeepEqual(got, []float64{2357}) {
		t.Errorf("paid 2007 = %v, want [2357]", got)
	}
	if got := ref.Incurred.Cells[0][0]; got != 3938 {
		t.Errorf("incurred 1998 lag 1 = %v, want 3938", got)
	}
	// Net premium is the loss ratio's denominator; direct premium is kept
	// for the net-to-direct ratio.
	if got := ref.EarnedPremium[0]; got != 8971 {
		t.Errorf("net premium 1998 = %v, want 8971", got)
	}
	if got := ref.DirectPremium[0]; got != 9347 {
		t.Errorf("direct premium 1998 = %v, want 9347", got)
	}
	dev := ref.DevelopedIncurred
	if got := dev.Cells[1][9]; got != 4000 {
		t.Errorf("1999 developed = %v, want 4000", got)
	}
	if got := dev.Cells[9][0]; got != 5329 {
		t.Errorf("2007 lag 1 = %v, want the triangle's 5329", got)
	}
	if got := dev.Cells[9][9]; got != 5468 {
		t.Errorf("2007 developed = %v, want 5468", got)
	}
}

func TestLoadFSEmbeddedMatchesDisk(t *testing.T) {
	embedded, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorFile)
	if err != nil {
		t.Fatal(err)
	}
	disk, err := schedulep.LoadFile(refFile)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(embedded, disk) {
		t.Fatal("embedded reference sets differ from disk")
	}
}
```

- [ ] **Step 8: Run them and confirm they fail**

Run: `go test ./internal/infrastructure/schedulep/`
Expected: a build failure, for example `ref.Company undefined` or `refdata.PersonalMotorFile` undefined.

- [ ] **Step 9: Write the reader**

Replace `internal/infrastructure/schedulep/reader.go` with:

```go
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
```

- [ ] **Step 10: Run the reader tests**

Run: `go test ./internal/infrastructure/schedulep/`
Expected: PASS.

- [ ] **Step 11: Move the callers to the file**

In `cmd/claimsgen/main.go:139` and `internal/infrastructure/web/server_test.go:29`, replace `refdata.PersonalMotorDir` with `refdata.PersonalMotorFile`.

In `internal/application/realism_test.go`, add this helper below the imports:

```go
// personalMotorRefs is the realism gate's reference pool: the private
// passenger auto companies embedded in the binary.
func personalMotorRefs(t *testing.T) []triangle.ReferenceSet {
	t.Helper()
	refs, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorFile)
	if err != nil {
		t.Fatal(err)
	}
	return refs
}
```

Then, in `TestDefaultPresetIsRealistic`, `TestEvaluateRealismProducesChecksAtEveryAge` and `TestRealismScoresOnlyTheScoredSections`, replace each

```go
	refs, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDir)
	if err != nil {
		t.Fatal(err)
	}
```

with `refs := personalMotorRefs(t)`.

Run `git grep -n "PersonalMotorDir\|LoadDir\|gr-code-list\|prune-dec2025" -- '*.go'` and confirm that it prints nothing. The docs still name them; Task 5 fixes the docs.

- [ ] **Step 12: Run everything**

Run: `go test ./... && go vet ./... && gofmt -l .`
Expected: PASS with no gofmt output. The gate now scores all 121 complete companies on net premium. The preset passes it as it stands (loss-ratio band about [0.41, 0.92], drift [0.62, 1.18]), and the golden hashes hold, since generation is untouched.

- [ ] **Step 13: Commit**

```bash
git add -A .gitattributes data tools cmd internal
git commit -m "Read the CAS Schedule P files directly, with net and direct premium"
```

---

### Task 2: selection rules for reference companies

**Files:**
- Create: `internal/domain/triangle/selection.go`
- Create: `internal/domain/triangle/selection_test.go`

**Interfaces:**
- Consumes: `ReferenceSet.Name`, `EarnedPremium`, `DirectPremium` and `Paid.StartYear` (Task 1).
- Produces: `type ReferenceCriteria struct { MaxPremiumCV, MaxNetToDirectCV, MinMeanPremium float64; Exclude map[string]string }`, `func (c ReferenceCriteria) Reason(r ReferenceSet) string` and `func SelectReferences(refs []ReferenceSet, c ReferenceCriteria) []ReferenceSet`.

- [ ] **Step 1: Write the failing tests**

Create `internal/domain/triangle/selection_test.go`:

```go
package triangle_test

import (
	"reflect"
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// steadyRef is a two-year company with steady premium that keeps 80% of its
// direct premium every year.
func steadyRef(name string) triangle.ReferenceSet {
	return triangle.ReferenceSet{
		Name:          name,
		Paid:          triangle.Triangle{StartYear: 1998, Cells: [][]float64{{50, 80}, {55}}},
		EarnedPremium: []float64{100, 110},
		DirectPremium: []float64{125, 137.5},
	}
}

func TestReferenceCriteriaReason(t *testing.T) {
	c := triangle.ReferenceCriteria{
		MaxPremiumCV:     0.45,
		MaxNetToDirectCV: 0.125,
		MinMeanPremium:   50,
		Exclude:          map[string]string{"9": "reinsurer"},
	}
	for _, tc := range []struct {
		name string
		edit func(r *triangle.ReferenceSet)
		want string
	}{
		{"steady book", func(*triangle.ReferenceSet) {}, ""},
		{"excluded by name", func(r *triangle.ReferenceSet) { r.Name = "9" }, "reinsurer"},
		{"no direct premium", func(r *triangle.ReferenceSet) { r.DirectPremium = nil }, "no net and direct premium for every accident year"},
		{"zero net premium", func(r *triangle.ReferenceSet) { r.EarnedPremium[1] = 0 }, "premium not positive in accident year 1999"},
		// Net premium 100 then 300: mean 200, standard deviation 100.
		{"premium tripled", func(r *triangle.ReferenceSet) {
			r.EarnedPremium[1], r.DirectPremium[1] = 300, 375
		}, "net premium varies too much (CV 0.500)"},
		// Kept shares 0.8 then 0.5: mean 0.65, standard deviation 0.15.
		{"quota share started", func(r *triangle.ReferenceSet) { r.DirectPremium[1] = 220 }, "net-to-direct ratio varies too much (CV 0.231)"},
		{"small book", func(r *triangle.ReferenceSet) {
			r.EarnedPremium, r.DirectPremium = []float64{40, 44}, []float64{50, 55}
		}, "too small (mean net premium 42)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := steadyRef("1")
			tc.edit(&r)
			if got := c.Reason(r); got != tc.want {
				t.Fatalf("Reason = %q, want %q", got, tc.want)
			}
		})
	}
}

// A zero bound switches its rule off.
func TestZeroCriteriaSelectEveryPositivePremiumBook(t *testing.T) {
	var c triangle.ReferenceCriteria
	r := steadyRef("1")
	r.EarnedPremium, r.DirectPremium = []float64{10, 300}, []float64{100, 310}
	if got := c.Reason(r); got != "" {
		t.Fatalf("zero criteria Reason = %q, want selected", got)
	}
}

func TestSelectReferencesKeepsInputOrder(t *testing.T) {
	c := triangle.ReferenceCriteria{Exclude: map[string]string{"b": "reinsurer"}}
	refs := []triangle.ReferenceSet{steadyRef("c"), steadyRef("b"), steadyRef("a")}
	var got []string
	for _, r := range triangle.SelectReferences(refs, c) {
		got = append(got, r.Name)
	}
	if want := []string{"c", "a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./internal/domain/triangle/`
Expected: a build failure, `undefined: triangle.ReferenceCriteria`.

- [ ] **Step 3: Write the selection rules**

Create `internal/domain/triangle/selection.go`:

```go
package triangle

import (
	"fmt"
	"math"
)

// ReferenceCriteria picks the reference companies that make a fair benchmark:
// books that did not change their business or their reinsurance over the
// accident years, large enough that their factors are not mostly claim
// sampling noise. The two coefficients of variation are the tests Meyers used
// to select Schedule P triangles (CAS Monograph 1, 2015, appendix A). A zero
// bound switches its rule off.
type ReferenceCriteria struct {
	// MaxPremiumCV bounds the coefficient of variation of net earned premium
	// across the accident years. A book that grew or shrank sharply changed
	// its business.
	MaxPremiumCV float64
	// MaxNetToDirectCV bounds the coefficient of variation of each accident
	// year's net-to-direct earned premium ratio. A moving ratio is a changing
	// reinsurance programme, which moves net losses apart from the business
	// written.
	MaxNetToDirectCV float64
	// MinMeanPremium is the least mean net earned premium per accident year,
	// in the reference's units.
	MinMeanPremium float64
	// Exclude leaves companies out by judgement, keyed by Name, each with its
	// reason.
	Exclude map[string]string
}

// Reason says why c leaves r out, or returns "" when c selects r. Every
// accident year's net and direct premium must be positive, because the
// ratios and the loss ratio need them.
func (c ReferenceCriteria) Reason(r ReferenceSet) string {
	if why, ok := c.Exclude[r.Name]; ok {
		return why
	}
	if len(r.EarnedPremium) == 0 || len(r.DirectPremium) != len(r.EarnedPremium) {
		return "no net and direct premium for every accident year"
	}
	ratios := make([]float64, len(r.EarnedPremium))
	for i, net := range r.EarnedPremium {
		if net <= 0 || r.DirectPremium[i] <= 0 {
			return fmt.Sprintf("premium not positive in accident year %d", r.Paid.StartYear+i)
		}
		ratios[i] = net / r.DirectPremium[i]
	}
	if cv := coefficientOfVariation(r.EarnedPremium); c.MaxPremiumCV > 0 && cv >= c.MaxPremiumCV {
		return fmt.Sprintf("net premium varies too much (CV %.3f)", cv)
	}
	if cv := coefficientOfVariation(ratios); c.MaxNetToDirectCV > 0 && cv >= c.MaxNetToDirectCV {
		return fmt.Sprintf("net-to-direct ratio varies too much (CV %.3f)", cv)
	}
	if m := average(r.EarnedPremium); m < c.MinMeanPremium {
		return fmt.Sprintf("too small (mean net premium %.0f)", m)
	}
	return ""
}

// SelectReferences returns the companies c selects, in their input order.
func SelectReferences(refs []ReferenceSet, c ReferenceCriteria) []ReferenceSet {
	out := make([]ReferenceSet, 0, len(refs))
	for _, r := range refs {
		if c.Reason(r) == "" {
			out = append(out, r)
		}
	}
	return out
}

// coefficientOfVariation is the population standard deviation of xs over its
// mean, or +Inf when the mean is not positive.
func coefficientOfVariation(xs []float64) float64 {
	m := average(xs)
	if math.IsNaN(m) || m <= 0 {
		return math.Inf(1)
	}
	ss := 0.0
	for _, x := range xs {
		ss += (x - m) * (x - m)
	}
	return math.Sqrt(ss/float64(len(xs))) / m
}

func average(xs []float64) float64 {
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/domain/triangle/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/triangle/selection.go internal/domain/triangle/selection_test.go
git commit -m "Add selection rules for Schedule P reference companies"
```

---

### Task 3: score drift relative to the pool median

**Files:**
- Modify: `internal/domain/triangle/compare.go` (`CompareToReference` and its comment, plus a new `relativeToMedian`)
- Modify: `internal/domain/triangle/compare_test.go`
- Modify: `internal/infrastructure/web/static/app.js` (`renderRealism`)

**Interfaces:**
- Produces: `Report.LossRatioDrift.Band` is the reference drifts divided by their median, so its Lo, Hi, Min and Max are relative to the pool. The generated drift is compared with it unchanged.

- [ ] **Step 1: Write the failing test**

In `internal/domain/triangle/compare_test.go`, lift the `ref` closure out of `TestDriftBandComesFromReferenceDrift` into a file-level helper, and have that test call it:

```go
// driftRef is a reference company whose developed loss ratio is 0.5 over the
// first five accident years and secondHalf/100 over the last five.
func driftRef(name string, secondHalf float64) ReferenceSet {
	cells := make([][]float64, 10)
	for i := range cells {
		v := 50.0
		if i >= 5 {
			v = secondHalf
		}
		cells[i] = []float64{v}
	}
	ep := make([]float64, 10)
	for i := range ep {
		ep[i] = 100
	}
	return ReferenceSet{
		Name: name, Paid: Triangle{Cells: [][]float64{{1, 2}}},
		Incurred: Triangle{Cells: [][]float64{{1, 1}}}, DevelopedIncurred: Triangle{Cells: cells},
		EarnedPremium: ep,
	}
}
```

In `TestDriftBandComesFromReferenceDrift`, `refs` becomes `[]ReferenceSet{driftRef("shrinking", 40), driftRef("flat", 50), driftRef("growing", 75)}`. Its drifts are 0.8, 1.0 and 1.5, with median 1, so its expected band does not change. Then add:

```go
// MR-15: the drift band is the reference drifts over their median, so a level
// the reference companies share, such as a market cycle, does not set it.
func TestDriftBandIsRelativeToThePoolMedian(t *testing.T) {
	// Raw drifts 0.8, 0.9 and 1.2, around a median of 0.9.
	refs := []ReferenceSet{driftRef("improving", 40), driftRef("typical", 45), driftRef("worsening", 60)}
	flat, ep := flatTriangle(10, 60, 100)
	report := CompareToReference(Comparison{Incurred: flat, EarnedPremium: ep}, refs)
	b := report.LossRatioDrift.Band
	if math.Abs(b.Min-0.8/0.9) > 1e-9 || math.Abs(b.Max-1.2/0.9) > 1e-9 {
		t.Fatalf("drift band min/max [%v, %v], want the drifts over their median [%v, %v]", b.Min, b.Max, 0.8/0.9, 1.2/0.9)
	}
	if !report.LossRatioDrift.Within {
		t.Fatalf("flat generated drift outside the band %+v", b)
	}
	// A drift of 0.85 is inside the raw drifts' P5-P95, [0.81, 1.17], but
	// below the relative band, [0.90, 1.30].
	improving := make([][]float64, 10)
	for i := range improving {
		v := 60.0
		if i >= 5 {
			v = 51
		}
		improving[i] = []float64{v}
	}
	report = CompareToReference(Comparison{Incurred: Triangle{StartYear: 1998, Cells: improving}, EarnedPremium: ep}, refs)
	if report.LossRatioDrift.Within {
		t.Fatalf("generated drift %v inside %+v, want outside the relative band", report.LossRatioDrift.Value, report.LossRatioDrift.Band)
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test ./internal/domain/triangle/ -run Drift`
Expected: `TestDriftBandIsRelativeToThePoolMedian` FAILS with `drift band min/max [0.8, 1.2]`. `TestDriftBandComesFromReferenceDrift` still passes.

- [ ] **Step 3: Implement**

In `CompareToReference`, replace `driftBand := bandFromValues(drifts)` with `driftBand := bandFromValues(relativeToMedian(drifts))`, and add below `CompareToReference`:

```go
// relativeToMedian divides xs by their median, keeping their spread and
// dropping the level they share. It returns xs unchanged when the median is
// not positive.
func relativeToMedian(xs []float64) []float64 {
	med := Percentile(xs, 50)
	if math.IsNaN(med) || med <= 0 {
		return xs
	}
	out := make([]float64, len(xs))
	for i, x := range xs {
		out[i] = x / med
	}
	return out
}
```

In the `CompareToReference` doc comment, replace the paragraph that begins "The drift band is the reference companies' own drift" with:

```go
// The drift band is the reference companies' drift relative to the pool's
// median drift (MR-15). Accident years 1998-2007 span the 2001-2004 hard
// market, which improved the later accident years of most companies alike,
// so their raw drifts centre below 1; the generator models no market cycle.
// Dividing by the median keeps the spread between companies and drops the
// level they share. The check is a realism bound, not a guard against
// systematic drift in the model; that guard is a test that switches the
// model's noise off (TestPresetHasNoSystematicLossRatioDrift).
```

- [ ] **Step 4: Say so in the UI**

In `internal/infrastructure/web/static/app.js` `renderRealism`, change the drift card title to `"Loss-ratio drift 2nd half / 1st half vs reference, relative to its median (flat = 1)"`. Append this sentence to the `scope.textContent` template, after "loose sanity bound.": `The drift band is each company's drift over the reference median, which leaves out the market cycle the companies share.`

- [ ] **Step 5: Run everything**

Run: `go test ./... && go vet ./... && gofmt -l .`
Expected: PASS with no gofmt output. On the 121 companies the relative drift band is about [0.69, 1.32], and the gate seeds' drifts (0.96-1.07) sit inside it. The golden hashes hold.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/triangle/compare.go internal/domain/triangle/compare_test.go internal/infrastructure/web/static/app.js
git commit -m "Score loss-ratio drift relative to the reference median"
```

---

### Task 4: select the pool and recalibrate the preset

**Files:**
- Modify: `internal/application/realism.go` (new `PersonalMotorCriteria`)
- Modify: `internal/application/realism_test.go` (`personalMotorRefs`, a new `TestPersonalMotorPool`, and the `TestDefaultPresetIsRealistic` book size and comment)
- Modify: `cmd/claimsgen/main.go` (`runUI`), `internal/infrastructure/web/server_test.go` (the server helper)
- Modify: `internal/infrastructure/config/motor-personal.yaml` (header comment, injury `close_lag`)
- Modify: `internal/application/golden_test.go` (three hashes)
- Modify: `internal/infrastructure/web/static/app.js` (`renderRealism` scope text)

**Interfaces:**
- Consumes: `triangle.ReferenceCriteria` and `triangle.SelectReferences` (Task 2), and the relative drift band (Task 3).
- Produces: `func PersonalMotorCriteria() triangle.ReferenceCriteria` in package `application`.

- [ ] **Step 1: Write the failing pool test**

In `internal/application/realism_test.go`, add:

```go
// MR-15: the gate's pool is the complete companies with steady premium and
// reinsurance that write at least $5m a year, less reinsurers.
func TestPersonalMotorPool(t *testing.T) {
	all, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorFile)
	if err != nil {
		t.Fatal(err)
	}
	c := application.PersonalMotorCriteria()
	var got []string
	for _, r := range triangle.SelectReferences(all, c) {
		got = append(got, r.Name)
	}
	want := []string{
		"353", "460", "620", "1066", "1090", "1538", "1716", "1767", "2003", "2143",
		"2208", "3240", "4839", "5185", "6947", "7080", "8427", "8672", "10007", "10022",
		"13420", "13501", "13889", "14044", "14176", "14257", "14311", "14443", "15024", "15199",
		"15997", "18163", "19119", "23574", "25755", "27022", "27065", "29440", "31062", "31550",
		"34509", "34592", "35173", "37028", "41041",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pool = %v\nwant %v", got, want)
	}
	reasons := map[string]string{}
	for _, r := range all {
		reasons[r.Name] = c.Reason(r)
	}
	for name, want := range map[string]string{
		"29297": "net premium varies too much (CV 0.975)",         // fronts: keeps 5% of its direct premium
		"13641": "net-to-direct ratio varies too much (CV 0.392)", // kept $4k of $14.0m direct in 2007
		"10308": "net premium varies too much (CV 0.519)",         // about $70k a year, shrinking
		"20430": "too small (mean net premium 2986)",              // cedes a steady 75%, which net premium makes fair
		"33499": "reinsurer (Dorinco Rein Co)",
	} {
		if reasons[name] != want {
			t.Errorf("company %s: Reason = %q, want %q", name, reasons[name], want)
		}
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test ./internal/application/ -run TestPersonalMotorPool`
Expected: a build failure, `undefined: application.PersonalMotorCriteria`.

- [ ] **Step 3: Define the pool**

In `internal/application/realism.go`, add:

```go
// PersonalMotorCriteria selects the private passenger auto reference pool
// (MR-15). The two coefficient-of-variation limits are Meyers' for personal
// auto (CAS Monograph 1, 2015, table 11): books with steady premium and a
// steady reinsurance programme. The $5m-a-year floor (Schedule P is in
// thousands) keeps companies whose factors are mostly claim sampling noise
// from setting the band edges; the gate's generated book earns about
// $20-45m a year on its scored sections. Reinsurers write assumed business,
// not a personal auto book. Of the 121 complete companies, 45 are selected.
func PersonalMotorCriteria() triangle.ReferenceCriteria {
	return triangle.ReferenceCriteria{
		MaxPremiumCV:     0.45,
		MaxNetToDirectCV: 0.125,
		MinMeanPremium:   5000,
		Exclude: map[string]string{
			"10019": "reinsurer (Overseas Partners Us Reins Co)",
			"23876": "reinsurer (Mapfre Reins Corp)",
			"33499": "reinsurer (Dorinco Rein Co)",
			"35408": "reinsurer (Sirius Amer Ins Co)",
			"42439": "reinsurer (Toa-Re Ins Co Of Amer)",
		},
	}
}
```

Run: `go test ./internal/application/ -run TestPersonalMotorPool`
Expected: PASS. If the pool differs, do not edit `want`. Compare the companies that differ against the rules: the coefficients are population standard deviations over the mean, and the rules use strict bounds (`cv >= max` rejects).

- [ ] **Step 4: Score the gate on the pool and see it fail**

In `internal/application/realism_test.go`, change `personalMotorRefs` to select:

```go
// personalMotorRefs is the realism gate's reference pool: the embedded
// private passenger auto companies that PersonalMotorCriteria selects.
func personalMotorRefs(t *testing.T) []triangle.ReferenceSet {
	t.Helper()
	all, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorFile)
	if err != nil {
		t.Fatal(err)
	}
	return triangle.SelectReferences(all, application.PersonalMotorCriteria())
}
```

In `TestDefaultPresetIsRealistic`, set `req.InitialBookSize = 100000` and replace the comment above it with:

```go
	// 100k keeps claim sampling noise in the late single-origin factors and
	// in the drift below the spread of the reference pool, which is made of
	// books of $5m a year and up: at 40k the incurred factor at age 9-10 left
	// its band on 4 of 60 seeds by luck, at 100k on none of 30.
```

In `cmd/claimsgen/main.go` `runUI`, select after loading:

```go
	all, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorFile)
	if err != nil {
		fmt.Fprintf(stderr, "claimsgen: reference data: %v\n", err)
		return 1
	}
	refs := triangle.SelectReferences(all, application.PersonalMotorCriteria())
```

`main.go` already imports `application` and `triangle`. In `newTestServer` in `internal/infrastructure/web/server_test.go`, return `web.NewServer(triangle.SelectReferences(refs, application.PersonalMotorCriteria()))`; the file already imports both packages.

Run: `go test ./internal/application/ -run TestDefaultPresetIsRealistic -v`
Expected: FAIL. The paid factors at age 2-3 and age 3-4 fall below their bands (about 1.101 and 1.039): the preset pays its injury claims too fast for this pool.

- [ ] **Step 5: Recalibrate injury settlement**

In `motor-personal.yaml`, in the `third_party_injury` claims section, set `close_lag.shape: 1.5` (was 1.0) and `close_lag.mean_days: 500` (was 400). A higher gamma shape thins the very long settlements, so the age 9-10 factors stay inside their bands while ages 2-5 slow down. In calibration runs, the combination moved the paid factors at ages 2-3, 3-4 and 4-5 from about P4, P4 and P9 of the pool to P13, P20 and P20, and the share of 120-month paid within 12 months from 54% to 49% (pool median 44%). It passed every check on seeds 1-30 at a 100k book.

Rewrite the sentence in the injury comment that begins "Its lags carry the later-age development" to:

```yaml
    # Its lags carry the later-age development of the Schedule P reference:
    # close_lag shape 1.5 and mean_days 500 were set by calibration against
    # the 45-company pool (MR-15). They keep paid development at ages 2-5 off
    # the pool's fast edge, and the factors at age 9-10 inside their bands,
    # across seeds 1-30.
```

Rewrite the file's header comment, lines 2-4, to:

```yaml
# The realism reference is Schedule P Part 1B, private passenger auto
# liability (data/reference/schedule p/ppauto_pos98-07.csv, the 45
# companies application.PersonalMotorCriteria selects). Part 1B covers
```

Leave the rest of the header as it is. Do not touch the `pricing` block: settlement speed does not enter pricing.

- [ ] **Step 6: Run the gate**

Run: `go test ./internal/application/ -run 'TestDefaultPresetIsRealistic|TestPresetHasNoSystematicLossRatioDrift|TestPersonalMotorPool' -v`
Expected: PASS on seeds 1, 42 and 7, and the noise-free drift within 3.5% of 1.

Then check that the calibration is not tuned to those three seeds. Edit the seed list in `TestDefaultPresetIsRealistic` to `1..30` in place and run it once; expect all 30 to pass. Revert the edit. If any seed fails, report the failing check and seed in the PR body instead of tuning further. Keep the values a reserving actuary would find believable: injury mean close between 300 and 900 days, shape between 1 and 3. If the gate cannot pass inside those ranges, stop and report the closest report to the maintainer.

- [ ] **Step 7: Refresh the golden hashes**

Run: `go test ./internal/application/ -run Golden`
Expected: three mismatches whose actual values are the ones in the global constraints. Paste them into `wantHash`, `wantAggregateHash` and `wantAnnualHash`, then re-run and confirm PASS.

- [ ] **Step 8: Name the pool in the UI**

In `renderRealism` in `app.js`, the `scored` strings name "the Schedule P private passenger auto reference". After "liability reference." in the whole-book string, and after "to score against it." in the scored-sections string, add: ` The reference companies are those with steady premium and reinsurance that write at least $5m a year.`

- [ ] **Step 9: Run everything**

Run: `go test ./... && go vet ./... && gofmt -l .`
Expected: PASS with no gofmt output.

- [ ] **Step 10: Commit**

```bash
git add -A internal cmd
git commit -m "Score the gate on the selected reference pool and recalibrate injury settlement"
```

---

### Task 5: docs

**Files:**
- Modify: `README.md` (the realism diagram and the paragraph under it)
- Modify: `docs/architecture.md` (layout, `triangle`, `schedulep`, invariants)
- Modify: `docs/roadmap.md` (the per-line-of-business reference data bullet)
- Modify: `AGENTS.md` (the `data/reference/` and `tools/` lines, the realism gate convention)
- Modify: `docs/review.md` (close MR-15)
- Modify: `docs/screenshots/ui-realism-pass.png`, `ui-realism-fail.png` (regenerated)
- Delete: this plan

- [ ] **Step 1: README**

In the realism diagram, change the `references` node to `references[("45 Schedule P private passenger auto<br/>liability companies, accident years 1998-2007,<br/>steady premium and reinsurance, $5m a year and up")]` and the `bands` node to `bands["P5-P95 band for each metric<br/>across the companies,<br/>drift relative to their median"]`.

In the paragraph after the diagram:

- Replace "Generated data is checked against 96 hand-curated Schedule P private passenger auto reference companies (`data/reference/schedule p/ppauto_pos98-07/`, accident years 1998-2007)." with "Generated data is checked against Schedule P private passenger auto reference companies from the CAS loss reserving database (`data/reference/schedule p/ppauto_pos98-07.csv`, accident years 1998-2007; see `data/reference/README.md`)."
- Replace "Part 1B is net of reinsurance and includes defence costs; the generated losses are gross of reinsurance and exclude defence costs, which the calibration absorbs implicitly." with "Part 1B losses are net of reinsurance and include defence costs, and they are scored against net earned premium. The generated losses are gross of reinsurance and exclude defence costs, which the calibration absorbs implicitly."
- Replace "The companies were curated from the full Schedule P extract via `data/reference/gr-code-list.md` and `tools/prune-dec2025.ps1` to remove low-volume and degenerate companies." with "Of the 121 companies with every accident year known to age 10, the check keeps the 45 that `application.PersonalMotorCriteria` selects: net premium steady across the years (coefficient of variation under 0.45) and a steady reinsurance programme (coefficient of variation of the net-to-direct premium ratio under 0.125), the limits Meyers used to select Schedule P triangles (CAS Monograph 1, 2015); at least $5m of net premium a year, so claim sampling noise in small books does not set the band edges; and no reinsurers."
- Replace "Real books drift widely, so the drift band is loose; a separate test" with "The drift band is each company's drift over the pool's median: the accident years span the 2001-2004 hard market, which improved most companies' later years alike, and the generator models no market cycle, so the band keeps the spread between companies and leaves out the level they share. A separate test".

Check that the caption "Raising the third-party injury base frequency to 0.1 pushes the ultimate loss ratio outside its band" still holds in Step 6.

- [ ] **Step 2: Architecture**

In `docs/architecture.md`:

- In the layout block, `data/reference/` becomes "CAS Schedule P files, private passenger auto embedded via refdata", and `tools/` becomes "dev-only: README screenshots".
- Change the `CompareToReference` bullet to end "...and the loss-ratio drift, relative to the pool's median drift, against the P5-P95 bands across the reference companies, and returns a `Report`." Add after it: "- `ReferenceCriteria` picks the reference companies: steady net premium and net-to-direct ratio, a size floor and named exclusions. `SelectReferences` applies it, and `application.PersonalMotorCriteria` is the gate's pool."
- Replace the `schedulep` bullet with: "- **`schedulep`** reads a CAS Schedule P file, one row per company, accident year and lag, into one reference set per company with every cell present: the paid and incurred triangles at the 2007 valuation, the incurred developed to age 10, and net and direct earned premium. `refdata` embeds the private passenger auto file; the other five lines in `data/reference/schedule p/` are kept but neither embedded nor read."
- In the realism invariant, "inside the Schedule P P5-P95 bands across several seeds" becomes "inside the P5-P95 bands of the selected Schedule P pool across several seeds".

- [ ] **Step 3: Roadmap and AGENTS.md**

In `docs/roadmap.md`, replace the first three sentences of the "Per-line-of-business reference data and calibration" bullet, up to and including "kept against `data/reference/gr-code-list.md`.", with: "The realism gate is motor-only today: `claimsgen ui` loads the private passenger auto file (`refdata.PersonalMotorFile`) and scores against the companies `application.PersonalMotorCriteria` selects. Reference data and its selection criteria need keying per line of business, so each class calibrates against an appropriate Schedule P family. `data/reference/schedule p/` holds the unmodified CAS files for all six lines; MR-18 records how many companies each keeps under the same rules, and that the size floor has to be set per line." Keep the rest of the bullet.

In `AGENTS.md`:

- `data/reference/` becomes "the CAS Schedule P files (private passenger auto embedded) and their provenance".
- The `tools/` line becomes "dev-only helpers, not part of the binary: `screenshots/` (a Node script that regenerates the README screenshots). The Node dependency there does not contradict the no-build-step UI."
- In the realism gate convention, "scores the shipped preset against the embedded Schedule P bands across several seeds" becomes "scores the shipped preset against the bands of the embedded Schedule P companies `application.PersonalMotorCriteria` selects, across several seeds".

- [ ] **Step 4: Close MR-15**

In `docs/review.md`, delete the MR-15 section and renumber the positions so they run 1-5. Then:

- MR-16: change "On MR-15's 45-company pool" to "On the 45-company pool", "The preset's reported incurred" to "Before MR-15's recalibration, the preset's reported incurred", and "Action: after MR-15, read" to "Action: read".
- MR-17: change "In the current pool the preset's paid factors" to "Before MR-15's recalibration, the preset's paid factors in the then-current 96-company pool".

- [ ] **Step 5: Regenerate the screenshots**

The realism screenshots show the bands, which have moved. If Node and Chrome are available:

```bash
go build ./cmd/claimsgen && ./claimsgen ui --port 8093 &
(cd tools/screenshots && npm install && node screenshots.js)
kill %1
```

Expected: the six PNGs in `docs/screenshots/` are rewritten. Only the realism ones should change noticeably; view `ui-realism-pass.png` and `ui-realism-fail.png` to check that the pass shows every check inside and the fail shows the loss ratio outside. If Node or Chrome is not available, say so in the PR body.

- [ ] **Step 6: Run everything and check the docs**

Run: `go test ./... && go vet ./... && gofmt -l .`
Expected: PASS with no gofmt output.

Run: `git grep -n "hand-curated\|gr-code-list\|prune-dec2025\|PersonalMotorDir\|96 \(Schedule P\|private\|companies\)" -- ':!docs/superpowers' ':!docs/raw user inputs' ':!docs/background-context.md'`
Expected: no output.

- [ ] **Step 7: Delete this plan and commit**

```bash
git rm -q docs/superpowers/plans/2026-10-02-mr-15-rebuild-reference.md
git add -A README.md AGENTS.md docs
git commit -m "Document the rebuilt reference pool and close MR-15"
```

Then open the PR. Its title and body are the squash commit `main` keeps, so lead with what changed and why. Name MR-15 as closed, and say that `go test ./...`, `go vet ./...` and `gofmt -l .` were run.
