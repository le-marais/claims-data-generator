> **OUT OF CONTEXT - do not read (2026-10-02):** implementation plan, deleted once its work ships. Agents must not load this file into context or treat it as a source of truth. For how the system works today see `README.md`, `AGENTS.md`, and `docs/architecture.md`.

# Commercial auto preset implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a `motor-commercial` preset with a fleet book and no-excess liability, scored against the Schedule P Part 1C commercial auto pool, with realism evaluation moved out of the YAML into per-preset realism profiles.

**Architecture:** Reference lines (criteria and label) live in `application`; each preset's realism profile (line and scored section names) lives in the config preset registry. The web server holds one pool per line and scores a run by its preset. The fleet book adds a level above policies in `policy.BookSimulator`; with no fleet block the book is drawn exactly as today.

**Tech Stack:** Go 1.26, gonum, yaml.v3, plain JS UI.

## Global constraints

- Reproducibility: same seed and config give byte-identical output. Motor's `wantAggregateHash` and `wantAnnualHash` must not change; only `wantHash` moves, for the `fleet_id` column.
- The YAML and `lob` hold simulation parameters only; no reference or scoring keys.
- A zero value means off or the old behaviour, and a switched-off block's other fields are not required.
- New parameters follow AGENTS.md's checklist: domain struct and validate, config mirror and `ToDomain` (same Go field names), preset YAML comment, `formFields` for numeric ones, docs.
- Docs and comments: sentence case headers, no em dashes (use ` - `), concise.
- Run `go test ./...`, `go vet ./...` and `gofmt -l .` before every commit.

---

### Task 1: Reference lines and the commercial auto pool

**Files:**
- Modify: `data/reference/refdata.go`
- Modify: `internal/application/realism.go`
- Create: `internal/infrastructure/schedulep/pools.go`
- Test: `internal/application/realism_test.go`, `internal/infrastructure/schedulep/reader_test.go`

**Interfaces:**
- Produces: `application.PrivatePassengerAuto`, `application.CommercialAuto` (string constants), `application.ReferenceLine{ID, Label string; Criteria triangle.ReferenceCriteria}`, `application.ReferenceLines() []ReferenceLine`, `application.CommercialAutoCriteria() triangle.ReferenceCriteria`, `application.ReferencePool{Line ReferenceLine; Refs []triangle.ReferenceSet}`, `refdata.LineFiles map[string]string`, `schedulep.LoadPools(fsys fs.FS, files map[string]string, lines []application.ReferenceLine) (map[string]application.ReferencePool, error)`.

- [ ] **Step 1: Failing test** - `TestCommercialAutoPool` in `realism_test.go`, pinning the 42 companies `CommercialAutoCriteria` selects from `comauto_pos_98-07.csv`, plus reasons for a few left out (one per rule), in the style of `TestPersonalMotorPool`. And `TestLoadPools` in `reader_test.go`: every `application.ReferenceLines()` entry has a file in `refdata.LineFiles`, and `LoadPools` returns 45 and 42 companies.
- [ ] **Step 2: Run** `go test ./internal/application ./internal/infrastructure/schedulep -run 'Pool'` - fails to compile.
- [ ] **Step 3: Implement.**

`refdata.go`:

```go
//go:embed "schedule p/ppauto_pos98-07.csv" "schedule p/comauto_pos_98-07.csv"
var Files embed.FS

// LineFiles names the embedded file of each Schedule P line the realism gate
// scores against, keyed by the application's reference line IDs.
var LineFiles = map[string]string{
	"private_passenger_auto": "schedule p/ppauto_pos98-07.csv",
	"commercial_auto":        "schedule p/comauto_pos_98-07.csv",
}
```

`PersonalMotorFile` is removed; its users read `LineFiles[application.PrivatePassengerAuto]`.

`realism.go`:

```go
// Reference line IDs.
const (
	PrivatePassengerAuto = "private_passenger_auto"
	CommercialAuto       = "commercial_auto"
)

// ReferenceLine is a Schedule P line the realism gate scores against: what
// to call it and the rules that select its reference companies.
type ReferenceLine struct {
	ID       string
	Label    string
	Criteria triangle.ReferenceCriteria
}

// ReferenceLines lists the lines the gate can score against.
func ReferenceLines() []ReferenceLine {
	return []ReferenceLine{
		{ID: PrivatePassengerAuto, Label: "private passenger auto liability", Criteria: PersonalMotorCriteria()},
		{ID: CommercialAuto, Label: "commercial auto liability", Criteria: CommercialAutoCriteria()},
	}
}

// ReferencePool is a line's selected reference companies.
type ReferencePool struct {
	Line ReferenceLine
	Refs []triangle.ReferenceSet
}

// CommercialAutoCriteria selects the commercial auto reference pool. The two
// coefficient-of-variation limits are Meyers' for commercial auto (CAS
// Monograph 1, 2015, table 11). Commercial auto companies are smaller than
// personal auto ones: a $5m floor keeps 25, too few for P5-P95 bands, so the
// floor is $1m. No reinsurer passes the limits. Of the 137 complete
// companies, 42 are selected.
func CommercialAutoCriteria() triangle.ReferenceCriteria {
	return triangle.ReferenceCriteria{MaxPremiumCV: 0.399, MaxNetToDirectCV: 0.125, MinMeanPremium: 1000}
}
```

`schedulep/pools.go`:

```go
// LoadPools reads each reference line's file from fsys and selects its pool,
// keyed by line ID.
func LoadPools(fsys fs.FS, files map[string]string, lines []application.ReferenceLine) (map[string]application.ReferencePool, error) {
	pools := make(map[string]application.ReferencePool, len(lines))
	for _, line := range lines {
		name, ok := files[line.ID]
		if !ok {
			return nil, fmt.Errorf("reference line %q: no file", line.ID)
		}
		all, err := LoadFS(fsys, name)
		if err != nil {
			return nil, err
		}
		pools[line.ID] = application.ReferencePool{Line: line, Refs: triangle.SelectReferences(all, line.Criteria)}
	}
	return pools, nil
}
```

- [ ] **Step 4: Run** the tests - pass. Update `cmd/claimsgen/main.go`, `server_test.go`, `realism_test.go`, `reader_test.go` from `PersonalMotorFile` to the new names; `go test ./...` passes.
- [ ] **Step 5: Commit** "Add reference lines and the commercial auto pool".

### Task 2: Realism profiles outside the YAML

**Files:**
- Modify: `internal/domain/lob/lob.go`, `lob_test.go` (drop `Scored`, `ScoredSections`)
- Modify: `internal/infrastructure/config/config.go`, `config_test.go`, `motor-personal.yaml` (drop `scored`, add `PresetInfo.Realism`, `PresetInfoFor`)
- Modify: `internal/application/realism.go` (`RealismProfile`), `realism_test.go`, `golden_test.go`
- Modify: `internal/infrastructure/web/server.go`, `viewmodel.go`, `server_test.go`, `static/app.js`
- Modify: `cmd/claimsgen/main.go`

**Interfaces:**
- Consumes: Task 1.
- Produces: `application.RealismProfile{Line string; Sections []string}`, `(RealismProfile).SectionIndices(l lob.LineOfBusiness) ([]int, error)`, `config.PresetInfo{ID, Name string; Realism application.RealismProfile}`, `config.PresetInfoFor(id string) (PresetInfo, bool)`, `web.NewServer(pools map[string]application.ReferencePool)`, request field `preset`, realism JSON fields `scored`, `note`, `reference{label, companies, min_mean_premium}`.

- [ ] **Step 1: Failing tests.**
  - `TestRealismProfileResolvesSections` (application): motor's profile resolves to `[1, 2]`; a missing name errors naming it; no sections resolves to nil.
  - `TestPresetsHaveRealismProfiles` (config): every preset's profile names a line in `application.ReferenceLines()` and resolves against its preset.
  - `TestGenerateScoresAgainstThePresetPool` (web): `realism.scored` true, `reference.companies` 45, `reference.label` "private passenger auto liability"; `TestGenerateWithoutAPresetIsNotScored`: no `preset` gives `scored` false and a note.
- [ ] **Step 2: Run** - fails to compile.
- [ ] **Step 3: Implement.**

```go
// RealismProfile says how the realism gate scores a line of business: the
// Schedule P reference line it is compared with, and the sections of cover
// scored together against it, by name. No sections scores the whole book.
// It is evaluation, not simulation, so it sits beside a preset rather than in
// its parameters.
type RealismProfile struct {
	Line     string
	Sections []string
}

// SectionIndices resolves the profile's sections to their indices in l, in
// the profile's order, or nil for the whole book.
func (p RealismProfile) SectionIndices(l lob.LineOfBusiness) ([]int, error) {
	var out []int
	for _, name := range p.Sections {
		i := slices.IndexFunc(l.Claims.Sections, func(s lob.SectionParams) bool { return s.Name == name })
		if i < 0 {
			return nil, fmt.Errorf("scored section %q: not a section of %s", name, l.Name)
		}
		out = append(out, i)
	}
	return out, nil
}
```

  `config.presetInfos` entries gain `Realism: application.RealismProfile{Line: application.PrivatePassengerAuto, Sections: []string{"third_party_property", "third_party_injury"}}`; `Presets()` deep-copies the sections. Web: `Server.pools`; `generateRequest.Preset string \`json:"preset"\``; `handleGenerate` calls a `score(run) (realismJSON, error)` that returns `realismJSON{Note: ...}` when the preset is unknown, the line has no pool, or `SectionIndices` errors. `app.js` sends `preset` (the selected `#lob-select` value) and `renderRealism` shows a neutral "Not scored" banner with the note, or builds the scope text from `r.reference` (label, company count, floor as `$${min_mean_premium / 1000}m`). The CLI `ui` loads `schedulep.LoadPools(refdata.Files, refdata.LineFiles, application.ReferenceLines())`. Tests that read `ScoredSections()` resolve motor's profile through a helper `scoredSections(t, presetID, l)`.

- [ ] **Step 4: Run** `go test ./...` - passes; `TestGoldenAnnualTriangles` and the other golden tests unchanged.
- [ ] **Step 5: Commit** "Move realism scoring out of the line of business into preset profiles".

### Task 3: Per-section excess switch

**Files:**
- Modify: `internal/domain/lob/lob.go` (`SectionParams.NoExcess`, `PricingSectionParams.NoExcess`), `expectedloss.go`
- Modify: `internal/domain/claim/claim.go`
- Modify: `internal/infrastructure/config/config.go`
- Test: `internal/domain/claim/claim_test.go`, `internal/domain/lob/expectedloss_test.go`

**Interfaces:**
- Produces: `SectionParams.NoExcess bool`, `PricingSectionParams.NoExcess bool`; YAML `no_excess`.

- [ ] **Step 1: Failing tests.**
  - `TestNoExcessSectionIgnoresThePolicyExcess` (claim): the same seed on a $1,000-excess policy with `NoExcess` and on a $0-excess policy without it gives identical claims, and the $1,000-excess policy without it gives fewer.
  - `TestExpectedSectionLossIgnoresTheExcessWhenSwitchedOff` (lob): `ExpectedSectionLoss` at excess 1000 with `NoExcess` equals the same at excess 0, for a lognormal, a Pareto and a sum-insured section.
- [ ] **Step 2: Run** - fails to compile.
- [ ] **Step 3: Implement.** In `simulateClaim`: `excess := pol.Excess; if sec.NoExcess { excess = 0 }`, then use `excess` for the cover limit and the cost. In `ExpectedSectionLoss`: `if sec.NoExcess { excess = 0 }` before the switch. Config mirrors `NoExcess bool \`yaml:"no_excess" json:"no_excess"\`` on both section structs and maps it in `ToDomain`. Doc comments say the zero value applies the excess.
- [ ] **Step 4: Run** `go test ./...` - passes, golden hashes unchanged.
- [ ] **Step 5: Commit** "Add a per-section switch that takes no excess off a claim (MR-8)".

### Task 4: Fleet book

**Files:**
- Modify: `internal/domain/lob/lob.go` (`FleetParams`, `FleetSizeParams`, `BookParams.Fleet`, validation)
- Modify: `internal/domain/policy/book.go` (`Policy.FleetID`, fleets, `ProjectedSize`)
- Modify: `internal/infrastructure/config/config.go`, `internal/infrastructure/web/fields.go`
- Modify: `internal/infrastructure/csv/writer.go`
- Test: `internal/domain/policy/book_test.go`, `internal/domain/lob/lob_test.go`, `internal/application/golden_test.go`

**Interfaces:**
- Produces: `lob.FleetParams{Size FleetSizeParams; SumInsuredSigma, RiskSpread float64}`, `lob.FleetSizeParams{Median, Sigma float64}`, `(FleetParams).Enabled() bool`, `(FleetParams).ExpectedSize() float64`, `policy.Policy.FleetID int`; YAML `book.fleet.{size.{median,sigma},sum_insured_sigma,risk_spread}`; `policies.csv` column `fleet_id` after `policy_id`.

- [ ] **Step 1: Failing tests** in `book_test.go`:
  - `TestWithoutFleetsEachPolicyIsItsOwnFleet`: `FleetID == ID` for every policy.
  - `TestFleetVehiclesShareCoverAndExcess`: fleets on, every fleet's vehicles share `CoverStart`, `CoverEnd` and `Excess`, and policy IDs stay sequential.
  - `TestBookSizeCountsFleets`: size volatility 0, fleets on: distinct fleets per underwriting year are 95, 100, 105, 110.
  - `TestFleetSizeCentresOnItsMedian`: median 3, sigma 0.8, 20k fleets: the median vehicle count is 3 and every fleet has at least 1.
  - `TestFleetRiskFactorIsSharedByItsVehicles`: spread 0.01, risk spread 0.5: within-fleet risk factor variance is under 1% of the between-fleet variance.
  - `TestProjectedSizeCountsVehicles`: with fleets, `ProjectedSize` is the fleet projection times `m * exp(s^2/2)`.
  - `lob_test.go`: validation names `book.fleet.size.median` (negative, and 0.5), `book.fleet.size.sigma`, `book.fleet.sum_insured_sigma`, `book.fleet.risk_spread` (negative); a switched-off fleet block accepts negative sigmas.
- [ ] **Step 2: Run** - fails to compile.
- [ ] **Step 3: Implement.**

`lob.go`:

```go
// FleetParams switches on a fleet book: fleets are written first, and each
// vehicle on a fleet is a policy, drawn around the fleet's values. A
// Size.Median of 0 switches fleets off: every policy is then a fleet of one
// vehicle, and the other fields are not required.
type FleetParams struct {
	// Size is the lognormal number of vehicles on a fleet, rounded to a whole
	// number and at least 1.
	Size FleetSizeParams
	// SumInsuredSigma is the lognormal sigma of a fleet's median vehicle sum
	// insured around the book's SumInsuredMedian: how far fleets' vehicle
	// values differ, a courier's vans against a haulier's tractors.
	SumInsuredSigma float64
	// RiskSpread is the standard deviation of the mean-one gamma fleet risk
	// factor, which scales the risk factor of every vehicle on the fleet; 0
	// gives every fleet a factor of 1.
	RiskSpread float64
}

// FleetSizeParams is the lognormal number of vehicles on a fleet.
type FleetSizeParams struct {
	Median float64
	Sigma  float64
}

// Enabled reports whether the book is written as fleets.
func (f FleetParams) Enabled() bool { return f.Size.Median > 0 }

// ExpectedSize is about the mean number of vehicles on a fleet: the
// lognormal mean, ignoring the rounding, and 1 with fleets off.
func (f FleetParams) ExpectedSize() float64 {
	if !f.Enabled() {
		return 1
	}
	return math.Max(1, f.Size.Median*math.Exp(f.Size.Sigma*f.Size.Sigma/2))
}
```

  `BookParams.Fleet FleetParams`; `Spread` doc: with fleets on it is the heterogeneity of vehicles within a fleet. `FleetParams.validate()` checks finiteness of all four, `median >= 0`, returns nil at 0, then `median >= 1` and the three spreads `>= 0`.

`book.go`: `Simulate` writes `size` fleets a year. With fleets off, fleet `id` is policy `id` and `simulatePolicy` draws from `policy-<id>` exactly as today (cover start, sum insured, risk factor, excess). With fleets on, `simulateFleet` draws from `fleet-<fleetID>` in order: cover start, excess, vehicle count `max(1, round(LogNormal(ln median, sigma)))`, fleet median sum insured `medianSI * LogNormal(0, SumInsuredSigma)`, fleet risk factor (mean-one gamma of `RiskSpread`, 1 with no draw at 0); then each vehicle draws from `policy-<id>` its sum insured `LogNormal(ln fleetMedian, Spread)` and risk factor `fleetRisk * gamma(1/Spread^2, Spread^2)`, and is priced by the shared pricing code. `ProjectedSize` multiplies the fleet projection by `book.Fleet.ExpectedSize()`.

  Config mirrors `FleetParams`/`FleetSizeParams` (`yaml:"fleet"`, `"size"`, `"median"`, `"sigma"`, `"sum_insured_sigma"`, `"risk_spread"`). `fields.go` Book group adds four fields with labels and tips. CSV header `policy_id,fleet_id,cover_start,...`.

- [ ] **Step 4: Run** `go test ./...`. `TestGoldenCSVBytes` fails: confirm `wantAggregateHash` and `wantAnnualHash` still pass, then paste the printed `wantHash`.
- [ ] **Step 5: Commit** "Write a book as fleets of vehicles".

### Task 5: CLI preset flag

**Files:** Modify `cmd/claimsgen/main.go`; test `cmd/claimsgen/main_test.go`.

- [ ] **Step 1: Failing tests:** `TestGeneratePresetMatchesTheDefault` (`--preset motor-personal` gives the default's bytes), `TestGenerateRejectsPresetWithConfig`, `TestGenerateRejectsAnUnknownPreset`.
- [ ] **Step 2: Run** - fails.
- [ ] **Step 3: Implement** `--preset ID` (default `motor-personal`); `--config` with an explicit `--preset` is an error; load with `config.Preset(id)`. Usage text lists the flag.
- [ ] **Step 4: Run** `go test ./cmd/...` - passes.
- [ ] **Step 5: Commit** "Add a --preset flag to generate".

### Task 6: The commercial auto preset and its calibration

**Files:**
- Create: `internal/infrastructure/config/motor-commercial.yaml`
- Modify: `internal/infrastructure/config/config.go` (embed and register), `config_test.go`
- Modify: `internal/application/realism_test.go`, `pricing_test.go`, `golden_test.go`
- Modify: `internal/infrastructure/web/server_test.go`, `cmd/claimsgen/main_test.go`

- [ ] **Step 1: Tests.**
  - `TestDefaultPresetIsRealistic` becomes `TestPresetsAreRealistic`: for every `config.Presets()` entry, generate on seeds 1, 42, 7 at the preset's gate book size (`gateBookSize` map; a preset with no entry fails) over 1998-2007 and score against its line's pool.
  - `TestPresetHasNoSystematicLossRatioDrift` and `TestPresetLossRatioLandsNearTarget` run per preset.
  - `TestGoldenCommercialAnnualTriangles` pins a hash of the commercial preset's `SectionComparison` triangles and earned premium, as `TestGoldenAnnualTriangles` does for motor.
  - Config: `TestPresets` lists both presets; the commercial preset loads and validates.
  - Web: `/api/lobs` lists both; a commercial run with `preset: motor-commercial` scores against 42 companies.
  - CLI: `--preset motor-commercial` writes policies with fleets (some `fleet_id` shared by two rows).
- [ ] **Step 2: Write the YAML** with a fleet block, a per-vehicle sum insured median of about $35,000, deductibles of $250-$5,000, `own_damage`, and `third_party_property` and `third_party_injury` with `no_excess: true`, injury limit 1,000,000, case cover opened deficient, target loss ratio about 0.60. Every parameter commented with what it does and why the preset uses its value. Register `motor-commercial` / "Motor commercial" with the commercial auto profile.
- [ ] **Step 3: Calibrate** with an uncommitted sweep test that runs seeds 1-30 at the gate book size and prints each failing metric with its value and band. Targets, Part 1C at $1m (P5 / median / P95): paid share at age 1 0.17 / 0.32 / 0.47, age 2 0.39 / 0.57 / 0.72, age 4 0.77 / 0.87 / 0.94, age 8 0.986 / 0.997 / 1.001; incurred factor 1-2 0.96 / 1.26 / 1.49, 2-3 0.99 / 1.08 / 1.26; loss ratio 0.33 / 0.55 / 0.68. Adjust injury and property damage close lags, report lags, case adequacy and frequencies until all 30 seeds pass with margin. Pick the gate book size so the gate runs in a few seconds.
- [ ] **Step 4: Run** `go test ./...` - passes; paste the commercial golden hash.
- [ ] **Step 5: Commit** "Add the commercial auto preset, calibrated against Schedule P Part 1C".

### Task 7: Docs, screenshots and hand-over

- [ ] README: CLI `--preset`, the fleet book in the policy model diagram and text, `no_excess` in the claim events diagram and section settings, the realism section for both lines (pools, floors, profiles), the presets list.
- [ ] `docs/architecture.md`: reference lines, pools, realism profiles, the fleet level in `policy`, the server's pools, refdata embedding both files.
- [ ] `data/reference/README.md`: both files embedded, both pools.
- [ ] `AGENTS.md`: realism gate test name and per-preset pools; adding a parameter (no evaluation keys in the YAML); adding a line of business (register its realism profile and reference line); `data/reference` embedded files.
- [ ] `docs/mission.md` realism line; `docs/roadmap.md` loses the second-line-of-business section and its near-term pointer; `docs/review.md` MR-8 rewritten (switch shipped; open: switch it on for personal motor's liability with a recalibration, and the body shape).
- [ ] Screenshots: `cd tools/screenshots && npm ci && node screenshots.js` if the UI changed.
- [ ] Delete this plan. Run `gofmt -l .`, `go vet ./...`, `go test ./...`, `golangci-lint run ./...`. Commit, push, open the PR, and stop.
