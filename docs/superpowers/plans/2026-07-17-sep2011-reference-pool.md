# Sep2011 Reference Pool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge the `schedule p/sep2011/auto_personal` Schedule P dataset into the app's personal motor reference pool so the realism test gate and the web UI score against both vintages (289 reference companies instead of 143).

**Architecture:** `data/reference/refdata.go` embeds both dataset directories and exposes `PersonalMotorDirs`, the single source of truth for which directories make up the pool. `schedulep.LoadFS` becomes variadic and loads all given directories in order, qualifying each `ReferenceSet.Name` with its vintage directory (e.g. `dec2025/10007`) so overlapping company IDs stay distinct. All call sites load the merged pool via the manifest.

**Tech Stack:** Go (standard library only: `embed`, `io/fs`, `testing/fstest`). Spec: `docs/superpowers/specs/2026-07-17-sep2011-reference-pool-design.md`.

## Global Constraints

- Repo root: `C:\Users\Stephan\repos\claims data generator` (path contains spaces - always quote it in shell commands).
- Module path: `github.com/le-marais/claimsgen`.
- Go standard library only; no new dependencies.
- Run tests from the repo root with `go test ./...`.
- Dataset facts: `schedule p/dec2025/ppauto_pos98-07` has 143 JSON files; `schedule p/sep2011/auto_personal` has 146; merged pool is 289. Both use the same JSON schema (sep2011 has extra `FuturePaid`/`FutureIncurred` keys the parser already ignores).
- `PersonalMotorDirs` order is dec2025 first, sep2011 second. Keep this order everywhere.
- Commit after each task. Never chain `git commit && git push`; do not push at all.
- Commit messages end with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

### Task 1: Variadic LoadFS with vintage-qualified names

**Files:**
- Modify: `internal/infrastructure/schedulep/reader.go`
- Test: `internal/infrastructure/schedulep/reader_test.go`

**Interfaces:**
- Consumes: existing `parse(name string, b []byte) (triangle.ReferenceSet, error)` and `errNoReferenceFiles` in `reader.go`.
- Produces: `LoadFS(fsys fs.FS, dirs ...string) ([]triangle.ReferenceSet, error)` - loads every `*.json` under each dir in the order given, files sorted by name within each dir; errors if any dir has zero files. `ReferenceSet.Name` becomes `<vintage>/<file-stem>` where vintage is the parent directory of the dataset dir (e.g. dir `schedule p/dec2025/ppauto_pos98-07` gives `dec2025/10007`). `LoadDir(dir string)` keeps its signature and produces the same qualified names (vintage from the parent of the disk path). Task 2 and 3 rely on these exact behaviors.

- [ ] **Step 1: Write the failing tests**

Add to `internal/infrastructure/schedulep/reader_test.go`. Add `"testing/fstest"` to the imports.

```go
const minimalRef = `{"ClassId":1,` +
	`"PaidTriangle":{"TriangleValues":[[1998,[100,150]],[1999,[120]]]},` +
	`"IncurredTriangle":{"TriangleValues":[[1998,[200,210]],[1999,[220]]]},` +
	`"EarnedPremium":[[1998,400],[1999,450]]}`

func TestLoadFSMergesDirsWithQualifiedNames(t *testing.T) {
	fsys := fstest.MapFS{
		"schedule p/dec2025/ppauto/10007.json": {Data: []byte(minimalRef)},
		"schedule p/sep2011/auto/10007.json":   {Data: []byte(minimalRef)},
	}
	refs, err := schedulep.LoadFS(fsys, "schedule p/dec2025/ppauto", "schedule p/sep2011/auto")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("loaded %d reference sets, want 2", len(refs))
	}
	if refs[0].Name != "dec2025/10007" || refs[1].Name != "sep2011/10007" {
		t.Errorf("names = %q, %q; want dec2025/10007, sep2011/10007", refs[0].Name, refs[1].Name)
	}
}

func TestLoadFSErrorsWhenAnyDirIsEmpty(t *testing.T) {
	fsys := fstest.MapFS{
		"schedule p/dec2025/ppauto/10007.json": {Data: []byte(minimalRef)},
	}
	_, err := schedulep.LoadFS(fsys, "schedule p/dec2025/ppauto", "schedule p/sep2011/auto")
	if err == nil {
		t.Fatal("LoadFS with an empty dir: want error, got nil")
	}
	if !strings.Contains(err.Error(), "schedule p/sep2011/auto") {
		t.Fatalf("error %q does not name the empty directory", err)
	}
}

func TestLoadDirQualifiesNamesByVintage(t *testing.T) {
	refs, err := schedulep.LoadDir(refDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(refs[0].Name, "dec2025/") {
		t.Errorf("Name = %q, want dec2025/ prefix", refs[0].Name)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd "C:/Users/Stephan/repos/claims data generator" && go test ./internal/infrastructure/schedulep/`
Expected: compile FAILURE - `LoadFS` does not accept multiple dirs yet (`too many arguments in call to schedulep.LoadFS`).

- [ ] **Step 3: Implement the variadic loader**

In `internal/infrastructure/schedulep/reader.go`, replace the existing `LoadFS` and `LoadDir` functions with:

```go
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
```

Notes:
- `LoadFile` is unchanged - single-file loads keep unqualified names (`TestLoadKnownCompany` expects `10007`).
- The existing `TestLoadFSEmbeddedMatchesDisk` still compiles (variadic accepts one dir) but its DeepEqual comparison also still passes because both sides now qualify the same way. Do not modify it in this task; Task 2 rewrites it.
- `TestLoadDirEmptyNamesDirectory` keeps passing via the `errNoReferenceFiles` rewrap in `LoadDir`.

- [ ] **Step 4: Run the package tests to verify they pass**

Run: `cd "C:/Users/Stephan/repos/claims data generator" && go test ./internal/infrastructure/schedulep/`
Expected: `ok github.com/le-marais/claimsgen/internal/infrastructure/schedulep` - all tests pass, including the three new ones.

- [ ] **Step 5: Run the full suite to check for fallout**

Run: `cd "C:/Users/Stephan/repos/claims data generator" && go build ./... && go test ./...`
Expected: all packages pass. (`cmd/claimsgen`, `internal/application`, and `internal/infrastructure/web` still load a single dir through the variadic signature.)

- [ ] **Step 6: Commit**

```bash
cd "C:/Users/Stephan/repos/claims data generator"
git add internal/infrastructure/schedulep/reader.go internal/infrastructure/schedulep/reader_test.go
git commit -m "Load multiple Schedule P dirs with vintage-qualified names

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Embed the sep2011 dataset and expose the PersonalMotorDirs manifest

**Files:**
- Modify: `data/reference/refdata.go`
- Test: `internal/infrastructure/schedulep/reader_test.go` (rewrite `TestLoadFSEmbeddedMatchesDisk`)

**Interfaces:**
- Consumes: `schedulep.LoadFS(fsys fs.FS, dirs ...string)` and `schedulep.LoadDir(dir string)` from Task 1 (vintage-qualified names on both).
- Produces: `refdata.Files embed.FS` now containing both datasets, and `refdata.PersonalMotorDirs []string` = `{"schedule p/dec2025/ppauto_pos98-07", "schedule p/sep2011/auto_personal"}`. Task 3's call sites use exactly `schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDirs...)`.

- [ ] **Step 1: Write the failing test**

In `internal/infrastructure/schedulep/reader_test.go`, replace the whole `TestLoadFSEmbeddedMatchesDisk` function with:

```go
func TestLoadFSEmbeddedMatchesDisk(t *testing.T) {
	embedded, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDirs...)
	if err != nil {
		t.Fatal(err)
	}
	if len(embedded) != 289 {
		t.Fatalf("embedded reference sets = %d, want 289", len(embedded))
	}
	var disk []triangle.ReferenceSet
	for _, dir := range refdata.PersonalMotorDirs {
		refs, err := schedulep.LoadDir(filepath.Join("../../../data/reference", dir))
		if err != nil {
			t.Fatal(err)
		}
		disk = append(disk, refs...)
	}
	if !reflect.DeepEqual(embedded, disk) {
		t.Fatal("embedded reference sets differ from disk")
	}
}
```

Add `"github.com/le-marais/claimsgen/internal/domain/triangle"` to the test file's imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd "C:/Users/Stephan/repos/claims data generator" && go test ./internal/infrastructure/schedulep/`
Expected: compile FAILURE - `refdata.PersonalMotorDirs` is undefined.

- [ ] **Step 3: Implement the manifest**

Replace the body of `data/reference/refdata.go` with:

```go
// Package refdata embeds the Schedule P reference datasets so the compiled
// binary can evaluate realism without access to the repository.
package refdata

import "embed"

//go:embed "schedule p/dec2025/ppauto_pos98-07/*.json" "schedule p/sep2011/auto_personal/*.json"
var Files embed.FS

// PersonalMotorDirs lists the embedded datasets that make up the personal
// motor reference pool, in load order. Both vintages cover the same line of
// business: dec2025 spans accident years 1998-2007, sep2011 spans 1988-1997.
var PersonalMotorDirs = []string{
	"schedule p/dec2025/ppauto_pos98-07",
	"schedule p/sep2011/auto_personal",
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd "C:/Users/Stephan/repos/claims data generator" && go test ./internal/infrastructure/schedulep/`
Expected: PASS, with `TestLoadFSEmbeddedMatchesDisk` seeing 289 sets.

- [ ] **Step 5: Commit**

```bash
cd "C:/Users/Stephan/repos/claims data generator"
git add data/reference/refdata.go internal/infrastructure/schedulep/reader_test.go
git commit -m "Embed the sep2011 personal motor data behind a pool manifest

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Load the merged pool at every call site and update docs

**Files:**
- Modify: `cmd/claimsgen/main.go:116`
- Modify: `internal/infrastructure/web/server_test.go:25`
- Modify: `internal/application/realism_test.go:15` and `:34`
- Modify: `README.md` (realism paragraph, currently line 73)
- Modify: `internal/infrastructure/config/motor-personal.yaml:4` (comment)

**Interfaces:**
- Consumes: `refdata.PersonalMotorDirs` and variadic `schedulep.LoadFS` from Tasks 1-2.
- Produces: nothing new - the app and tests score against the merged 289-company pool.

- [ ] **Step 1: Switch the UI entry point**

In `cmd/claimsgen/main.go`, replace

```go
	refs, err := schedulep.LoadFS(refdata.Files, "schedule p/dec2025/ppauto_pos98-07")
```

with

```go
	refs, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDirs...)
```

- [ ] **Step 2: Switch the web server test**

In `internal/infrastructure/web/server_test.go`, replace

```go
	refs, err := schedulep.LoadFS(refdata.Files, "schedule p/dec2025/ppauto_pos98-07")
```

with

```go
	refs, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDirs...)
```

- [ ] **Step 3: Switch the realism test gate**

In `internal/application/realism_test.go`, replace both occurrences of

```go
	refs, err := schedulep.LoadDir("../../data/reference/schedule p/dec2025/ppauto_pos98-07")
```

with

```go
	refs, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDirs...)
```

and add `refdata "github.com/le-marais/claimsgen/data/reference"` to that file's imports.

- [ ] **Step 4: Update the docs**

In `README.md`, replace the sentence fragment

```
Generated data is checked against the ~145 Schedule P private passenger auto reference datasets (1998-2007) in `data/reference/schedule p/dec2025/ppauto_pos98-07/`:
```

with

```
Generated data is checked against 289 Schedule P private passenger auto reference datasets from two vintages - `data/reference/schedule p/dec2025/ppauto_pos98-07/` (accident years 1998-2007) and `data/reference/schedule p/sep2011/auto_personal/` (accident years 1988-1997):
```

In `internal/infrastructure/config/motor-personal.yaml`, replace the comment fragment

```
# data (data/reference/schedule p/dec2025/ppauto_pos98-07).
```

with

```
# data (data/reference/schedule p, dec2025 and sep2011 personal motor).
```

- [ ] **Step 5: Run the full suite**

Run: `cd "C:/Users/Stephan/repos/claims data generator" && go build ./... && go test ./...`
Expected: all packages pass. `TestDefaultPresetIsRealistic` must still pass - the merged pool can only widen the bands. If it fails, stop and report; do not loosen the test.

- [ ] **Step 6: Commit**

```bash
cd "C:/Users/Stephan/repos/claims data generator"
git add cmd/claimsgen/main.go internal/infrastructure/web/server_test.go internal/application/realism_test.go README.md internal/infrastructure/config/motor-personal.yaml
git commit -m "Score realism against the merged two-vintage reference pool

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```
