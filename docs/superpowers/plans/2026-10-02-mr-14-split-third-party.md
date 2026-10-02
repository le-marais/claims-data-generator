> **OUT OF CONTEXT - do not read (2026-10-02) unless you are executing this plan:** implementation plan for MR-14, deleted once the work ships. It is not a source of truth for how the system works; for that see `README.md`, `AGENTS.md` and `docs/architecture.md`.

# Split third party into property and injury - implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close MR-14. The scored liability part of the motor preset should look like Schedule P Part 1B at claim level: many small, fast third-party property damage claims and fewer, larger, slow injury claims, scored together against the private passenger auto reference.

**Architecture:** Four steps, each shippable on the branch:

1. The realism gate scores the union of every `scored: true` section instead of at most one. This is byte-identical for the current preset.
2. A `lognormal` severity kind, a lognormal loss in start-year dollars with no cap, for property damage. Byte-identical: no preset uses it yet.
3. The preset swaps `third_party` for `third_party_property` and `third_party_injury`, both scored, and is recalibrated to the bands. The golden hashes move here and only here.
4. Docs.

**Tech stack:** Go 1.26.4+, gonum, yaml.v3, plain JS UI with no build step. Tests use `go test`.

## Global constraints

- Reproducibility is a hard invariant: the same seed and config give byte-identical output. Tasks 1 and 2 must leave all three golden hashes in `internal/application/golden_test.go` unchanged. Only Task 3 may refresh them, and only after the realism gate passes.
- Dependency direction: `domain` imports nothing outside itself, `application` orchestrates, `infrastructure` adapts.
- A zero value means "off" or the old behaviour, so existing YAMLs keep working. A YAML with one scored section, or none, must score exactly as before.
- No new dependencies.
- Docs and comments: sentence case headers, no em dashes (use ` - `), concise and factual.
- Branch `feature/mr-14-split-third-party`. Commit as you go. One PR, squash merged by the maintainer. Run `go test ./...`, `go vet ./...` and `gofmt -l .` before opening it, and say so in the body. Open the PR and stop: do not merge, approve or enable auto-merge.
- Out of scope: MR-8's per-section limit and excess switch. The preset excess keeps applying to every section, as it does today.

---

### Task 1: score the union of several scored sections

**Files:**
- Modify: `internal/domain/lob/lob.go` (the `SectionParams.Scored` comment, `ClaimParams.ScoredSection`, and `ClaimParams.validate`, which drops the at-most-one check)
- Modify: `internal/domain/lob/lob_test.go` (`TestValidationNamesTheOffendingField`, `TestValidateSkipsSwitchedOffBlocks`, `TestScoredSection`)
- Modify: `internal/application/realism.go` (`EvaluateRealism`, `SectionComparison`, `sectionOf`)
- Modify: `internal/application/realism_test.go`, `internal/application/golden_test.go` (call sites)
- Modify: `internal/infrastructure/web/server.go:216`, `internal/infrastructure/web/viewmodel.go` (`realismJSON`, `scoredSectionName`, `realismView`)
- Modify: `internal/infrastructure/web/static/app.js` (`renderRealism`)
- Modify: `internal/infrastructure/web/server_test.go` (realism response struct)

**Interfaces:**
- Produces: `func (c ClaimParams) ScoredSections() []int`, which returns the indices of the scored sections in order, or nil when none is scored.
- Produces: `func SectionComparison(ds Dataset, startYear, years int, sections []int) (triangle.Comparison, error)` and `func EvaluateRealism(ds Dataset, startYear, years int, sections []int, refs []triangle.ReferenceSet) (triangle.Report, error)`. An empty `sections` scores the whole book.
- Produces: the JSON field `realism.sections`, a list of section names that is empty for the whole book. It replaces `realism.section`.

- [ ] **Step 1: Write the failing domain tests**

In `internal/domain/lob/lob_test.go`, replace `TestScoredSection` with:

```go
func TestScoredSections(t *testing.T) {
	l := validMotor()
	if got := l.Claims.ScoredSections(); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("ScoredSections() = %v, want [1]", got)
	}
	l.Claims.Sections[0].Scored = true
	if got := l.Claims.ScoredSections(); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Errorf("ScoredSections() with both scored = %v, want [0 1]", got)
	}
	l.Claims.Sections[0].Scored, l.Claims.Sections[1].Scored = false, false
	if got := l.Claims.ScoredSections(); got != nil {
		t.Errorf("ScoredSections() with none scored = %v, want nil", got)
	}
}
```

Add `"reflect"` to the imports if it is missing. In `TestValidationNamesTheOffendingField`, delete this case, because two scored sections are now valid:

```go
		{"claims.sections", func(l *LineOfBusiness) { l.Claims.Sections[0].Scored = true }},
```

In `TestValidateSkipsSwitchedOffBlocks`, add it to the valid cases after `"no section scored"`:

```go
		{"two sections scored", func(l *LineOfBusiness) {
			l.Claims.Sections[0].Scored = true
		}},
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./internal/domain/lob/`
Expected: a build failure, `l.Claims.ScoredSections undefined`.

- [ ] **Step 3: Implement in the domain**

In `internal/domain/lob/lob.go`, change the `Scored` comment to:

```go
	// Scored marks a section the realism gate scores against the Schedule P
	// reference. The gate scores the scored sections together, their claims
	// against their combined premium; with none marked it scores the whole
	// book.
	Scored bool
```

Replace `ScoredSection` with:

```go
// ScoredSections are the indices of the sections the realism gate scores, in
// order, or nil when none is marked and the gate scores the whole book.
func (c ClaimParams) ScoredSections() []int {
	var scored []int
	for i, sec := range c.Sections {
		if sec.Scored {
			scored = append(scored, i)
		}
	}
	return scored
}
```

In `ClaimParams.validate`, remove the `scored` counter and the `if scored > 1` block. Change `scored, active := 0, 0` to `active := 0` and delete the `if sec.Scored { scored++ }` block.

- [ ] **Step 4: Run the domain tests**

Run: `go test ./internal/domain/lob/`
Expected: PASS. Other packages do not build yet, which is expected.

- [ ] **Step 5: Write the failing application test**

In `internal/application/realism_test.go`, replace every `req.LOB.Claims.ScoredSection()` with `req.LOB.Claims.ScoredSections()` (five call sites, plus one in `golden_test.go`). Then add:

```go
// Scoring several sections scores their union: their claims against their
// combined premium. Scoring every section is the whole book.
func TestRealismScoresTheUnionOfScoredSections(t *testing.T) {
	req := request(t)
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(8), req)
	if err != nil {
		t.Fatal(err)
	}
	all, err := application.SectionComparison(ds, req.StartYear, req.Years, []int{ownDamage, thirdParty})
	if err != nil {
		t.Fatal(err)
	}
	book, err := application.SectionComparison(ds, req.StartYear, req.Years, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(all.Paid, book.Paid) || !reflect.DeepEqual(all.Incurred, book.Incurred) {
		t.Fatal("scoring every section should give the whole book's triangles")
	}
	// A policy's premium is rounded to the cent apart from its section
	// premiums, so the sum of sections can differ from it by a cent or two.
	for i, ep := range book.EarnedPremium {
		if math.Abs(all.EarnedPremium[i]-ep) > 1e-6*ep {
			t.Fatalf("year %d: sections' earned premium %v, whole book %v", i, all.EarnedPremium[i], ep)
		}
	}
}
```

- [ ] **Step 6: Run it and confirm it fails**

Run: `go test ./internal/application/ -run 'TestRealismScoresTheUnionOfScoredSections'`
Expected: a build failure, because `SectionComparison` still takes an `int`.

- [ ] **Step 7: Implement in the application layer**

In `internal/application/realism.go`:

```go
// EvaluateRealism scores the scored sections of a dataset against the bands
// observed across the reference companies (see SectionComparison). sections
// are the indices of the scored sections; empty scores the whole book. Used
// as a test gate; it also backs the UI's realism view.
func EvaluateRealism(ds Dataset, startYear, years int, sections []int, refs []triangle.ReferenceSet) (triangle.Report, error) {
	c, err := SectionComparison(ds, startYear, years, sections)
	if err != nil {
		return triangle.Report{}, err
	}
	return triangle.CompareToReference(c, refs), nil
}
```

Change the first paragraph of the `SectionComparison` doc comment to:

```go
// SectionComparison builds what the realism gate scores: the accident-year
// triangles and earned premium of the given sections of cover together, their
// claims against each policy's premium for those sections, or of the whole
// book when sections is empty. The motor preset scores its third-party
// sections, because the Schedule P private passenger auto reference is a
// liability line with no physical damage in it, so own-damage claims are left
// out rather than bent to liability development speed. Paid is net of salvage
// and subrogation to match Schedule P, which reports paid losses net of
// recoveries.
```

Change its signature to `sections []int` and its body guard to:

```go
	policies, claims := ds.Policies, ds.Claims
	if len(sections) > 0 {
		policies, claims = sectionsOf(ds, sections)
	}
```

Replace `sectionOf` with:

```go
// sectionsOf narrows a dataset to some sections of cover: every policy
// carrying only those sections' premium, and their claims. The grid builder
// skips transactions of claims it is not given, so the full ledger can be
// passed alongside.
func sectionsOf(ds Dataset, sections []int) ([]policy.Policy, []claim.Claim) {
	in := make(map[int]bool, len(sections))
	for _, s := range sections {
		in[s] = true
	}
	policies := make([]policy.Policy, len(ds.Policies))
	for i, p := range ds.Policies {
		p.Premium = 0
		for _, s := range sections {
			p.Premium += p.SectionPremiums[s]
		}
		policies[i] = p
	}
	var claims []claim.Claim
	for _, c := range ds.Claims {
		if in[c.Section] {
			claims = append(claims, c)
		}
	}
	return policies, claims
}
```

`shared.Money` is integer cents, so for one section the sum equals the old single-section premium exactly, and the golden hashes stay unchanged. Summing a policy's section premiums can differ from its `Premium` by a cent, because each is rounded separately (`internal/domain/policy/book.go`). That is why the union test compares premium with a tolerance. The map is only used for lookups, never iterated into output, so it adds no nondeterminism.

- [ ] **Step 8: Update the web adapter**

In `internal/infrastructure/web/server.go:216`, pass `res.line.Claims.ScoredSections()`.

In `internal/infrastructure/web/viewmodel.go`, change the `realismJSON` field:

```go
	// Sections names the sections the report scores together; empty means
	// the whole book.
	Sections       []string       `json:"sections"`
```

Replace `scoredSectionName` with:

```go
// scoredSectionNames are the names of the sections the realism gate scores,
// or empty when it scores the whole book.
func scoredSectionNames(p config.LOBParams) []string {
	names := []string{}
	for _, sec := range p.Claims.Sections {
		if sec.Scored {
			names = append(names, sec.Name)
		}
	}
	return names
}
```

Update `realismView(r triangle.Report, sections []string)` to set `Sections: sections`, and its caller to `realismView(realism, scoredSectionNames(req.Params))`. Start with `[]string{}` rather than nil so the JSON is `[]`, not `null`.

In `internal/infrastructure/web/static/app.js` `renderRealism`, replace the `scored` constant with:

```js
  const names = (r.sections || []).map((s) => s.replaceAll("_", " "));
  const scored = names.length
    ? `Scored on the ${names.join(" and ")} ${names.length > 1 ? "sections" : "section"} alone, their claims against their share of premium: the Schedule P private passenger auto reference is a liability line, and the line of business marks these sections to score against it.`
    : "Scored on the whole book against the Schedule P private passenger auto liability reference.";
```

- [ ] **Step 9: Pin the JSON in the server test**

In `internal/infrastructure/web/server_test.go`, add a field to the `Realism` struct in the generate response:

```go
			Sections []string `json:"sections"`
```

and after the existing realism checks:

```go
	if !reflect.DeepEqual(resp.Realism.Sections, []string{"third_party"}) {
		t.Fatalf("realism.sections = %v, want [third_party]", resp.Realism.Sections)
	}
```

(Task 3 changes the expected value to `[third_party_property third_party_injury]`.) Add `"reflect"` to the imports if it is missing.

- [ ] **Step 10: Run everything and confirm the output did not move**

Run: `go test ./... && go vet ./... && gofmt -l .`
Expected: PASS with no gofmt output. The three golden tests pass with their current hashes. If any golden hash moved, stop and find out why before going further.

- [ ] **Step 11: Check the UI by hand**

Run `go build ./cmd/claimsgen && ./claimsgen ui`, generate with the preset, and open the Realism tab. The scope note should read "Scored on the third party section alone, their claims against their share of premium: ...".

- [ ] **Step 12: Commit**

```bash
git add -A internal
git commit -m "Score the union of every scored section in the realism gate"
```

---

### Task 2: add a lognormal severity kind in dollars

This adds a line-of-business parameter, so follow the order in `AGENTS.md`. `severity.median` is new; `severity.sigma` is shared with `sum_insured_lognormal`.

**Files:**
- Modify: `internal/domain/lob/lob.go` (`SeverityKind` constants, `SeverityParams`, `SeverityParams.validate`)
- Modify: `internal/domain/lob/expectedloss.go` (`ExpectedSectionLoss`)
- Modify: `internal/domain/claim/claim.go` (`simulateClaim` comment, `drawGroundUpLoss`)
- Modify: `internal/infrastructure/config/config.go` (`SeverityParams`, `toDomain`)
- Modify: `internal/infrastructure/web/fields.go` (`severityFields`)
- Modify: `internal/infrastructure/web/fields_internal_test.go` (known-kind check)
- Tests: `internal/domain/lob/lob_test.go`, `internal/domain/lob/expectedloss_test.go`, `internal/domain/claim/claim_test.go`

**Interfaces:**
- Produces: `lob.Lognormal SeverityKind = "lognormal"` and `SeverityParams.Median float64` (start-year dollars), with config YAML/JSON key `median`.

- [ ] **Step 1: Write the failing tests**

In `internal/domain/lob/lob_test.go` `TestValidationNamesTheOffendingField`, add:

```go
		{"claims.sections[1].severity.median", func(l *LineOfBusiness) {
			l.Claims.Sections[1].Severity = SeverityParams{Kind: Lognormal, Sigma: 0.8}
		}},
		{"claims.sections[1].severity.sigma", func(l *LineOfBusiness) {
			l.Claims.Sections[1].Severity = SeverityParams{Kind: Lognormal, Median: 2000}
		}},
```

In `TestValidateSkipsSwitchedOffBlocks`, add a valid case:

```go
		{"lognormal third party", func(l *LineOfBusiness) {
			l.Claims.Sections[1].Severity = SeverityParams{Kind: Lognormal, Median: 2000, Sigma: 0.8}
			l.Pricing.Sections[1].Severity = SeverityParams{Kind: Lognormal, Median: 2000, Sigma: 0.8}
		}},
```

In `internal/domain/lob/expectedloss_test.go`, add:

```go
// A lognormal section prices the uncapped stop-loss of a dollar lognormal,
// trended by the assumed claims index, ignoring the sum insured.
func TestExpectedSectionLossPricesADollarLognormal(t *testing.T) {
	p := motorPricing()
	p.Sections[1].Severity = SeverityParams{Kind: Lognormal, Median: 2000, Sigma: 0.8}
	payout := 1 - p.NilProbability + p.ReopenProbability*p.ReopenEstimateFactor
	want := p.Sections[1].BaseFrequency * 1.3 * payout * stopLossLognormal(1.1*2000, 0.8, 300)
	got := p.ExpectedSectionLoss(1, 20000, 300, 1.3, 1.1, 1.05)
	if math.Abs(got-want) > 1e-9*want {
		t.Fatalf("lognormal section loss = %v, want %v", got, want)
	}
	if other := p.ExpectedSectionLoss(1, 5000, 300, 1.3, 1.1, 1.05); other != got {
		t.Fatalf("lognormal section moved with the sum insured: %v vs %v", other, got)
	}
}
```

In `internal/domain/claim/claim_test.go`, add:

```go
// A lognormal section draws a dollar loss around its median, uncapped by the
// sum insured and with no cover limit.
func TestLognormalClaimsCentreOnTheirMedian(t *testing.T) {
	p := only(params(), thirdParty, 0.15)
	p.Sections[thirdParty].Severity = lob.SeverityParams{Kind: lob.Lognormal, Median: 3000, Sigma: 0.8}
	claims := claim.NewClaimSimulator(p).Simulate(random.NewSource(11), fixedBook(40000, 2000, 0, 1.0))
	if len(claims) < 1000 {
		t.Fatalf("got %d claims, want plenty", len(claims))
	}
	costs := make([]float64, len(claims))
	exceeded := false
	for i, c := range claims {
		costs[i] = c.Episodes[0].Ultimate.Dollars()
		exceeded = exceeded || costs[i] > 2000
		if c.CoverLimit != 0 {
			t.Fatalf("claim %d has cover limit %v, want none", c.ID, c.CoverLimit)
		}
	}
	sort.Float64s(costs)
	if median := costs[len(costs)/2]; math.Abs(median/3000-1) > 0.05 {
		t.Errorf("median cost %v, want about 3000", median)
	}
	if !exceeded {
		t.Error("no claim exceeded the sum insured; a lognormal severity should be uncapped")
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./internal/domain/...`
Expected: a build failure, `undefined: Lognormal` and `unknown field Median`.

- [ ] **Step 3: Implement in the domain**

In `internal/domain/lob/lob.go`, add to the `SeverityKind` constants after `Pareto`:

```go
	// Lognormal is a lognormal loss in start-year dollars with no cap, for
	// claims sized independently of the insured's own cover, such as
	// third-party property damage.
	Lognormal SeverityKind = "lognormal"
```

and to `SeverityParams`, after `MedianFraction` and `Sigma`:

```go
	// Median and Sigma parameterize Lognormal: the median loss in start-year
	// dollars, and the lognormal sigma.
	Median float64
```

Update the `MedianFraction` and `Sigma` comment so it reads "Sigma is shared with Lognormal". In `SeverityParams.validate`, add `namedFloat{prefix + ".median", s.Median}` to `checkFinite`, and add a case before `default`:

```go
	case Lognormal:
		if s.Median <= 0 {
			return fmt.Errorf("%s.median: must be positive, got %v", prefix, s.Median)
		}
		if s.Sigma <= 0 {
			return fmt.Errorf("%s.sigma: must be positive, got %v", prefix, s.Sigma)
		}
```

Change the `default` message to list all three kinds:

```go
		return fmt.Errorf("%s.kind: must be %q, %q or %q, got %q", prefix, SumInsuredLognormal, Pareto, Lognormal, s.Kind)
```

In `internal/domain/lob/expectedloss.go` `ExpectedSectionLoss`, add a case and update the doc comment's "A Pareto severity keeps the claims index and is uncapped" to "A Pareto or Lognormal severity keeps the claims index and is uncapped":

```go
	case Lognormal:
		return perClaim * stopLossLognormal(inflationFactor*sev.Median, sev.Sigma, excess)
```

In `internal/domain/claim/claim.go`, change `drawGroundUpLoss`:

```go
// drawGroundUpLoss draws a loss in start-year dollars from a section's
// severity: a lognormal fraction of the policy's base-year sum insured, a
// Pareto amount, or a lognormal amount. Every kind takes one draw.
func (s *ClaimSimulator) drawGroundUpLoss(src shared.RandomSource, pol policy.Policy, sev lob.SeverityParams) float64 {
	switch sev.Kind {
	case lob.Pareto:
		return src.Pareto(sev.Scale, sev.Alpha)
	case lob.Lognormal:
		return src.LogNormal(math.Log(sev.Median), sev.Sigma)
	}
	return s.baseSumInsured(pol) * src.LogNormal(math.Log(sev.MedianFraction), sev.Sigma)
}
```

and in `simulateClaim` change "a Pareto loss is uncapped" to "a Pareto or lognormal loss is uncapped". The cap and `CoverLimit` already apply only to `SumInsuredLognormal`.

- [ ] **Step 4: Run the domain tests**

Run: `go test ./internal/domain/...`
Expected: PASS.

- [ ] **Step 5: Mirror it in config and the form**

In `internal/infrastructure/config/config.go`, add to `SeverityParams` after `Sigma`:

```go
	Median         float64 `yaml:"median" json:"median"`
```

and `Median: s.Median,` in `toDomain`.

In `internal/infrastructure/web/fields.go`, append to `severityFields`:

```go
	{Path: []string{"severity", "median"}, Kind: "lognormal", Label: "Severity median", Tip: "Median ground-up loss in start-year dollars; uncapped by the sum insured."},
	{Path: []string{"severity", "sigma"}, Kind: "lognormal", Label: "Severity sigma", Tip: "Sigma of the lognormal loss."},
```

In `internal/infrastructure/web/fields_internal_test.go`, extend the known-kind check:

```go
			if k := lob.SeverityKind(f.Kind); f.Kind != "" && k != lob.SumInsuredLognormal && k != lob.Pareto && k != lob.Lognormal {
```

- [ ] **Step 6: Run everything and confirm the output did not move**

Run: `go test ./... && go vet ./... && gofmt -l .`
Expected: PASS. `TestToDomainMapsEveryField` and `TestFormFieldsCoverEveryParameter` pass, and the golden hashes are unchanged because no preset uses the new kind.

- [ ] **Step 7: Commit**

```bash
git add -A internal
git commit -m "Add a lognormal severity kind in dollars"
```

---

### Task 3: split the preset's third party and recalibrate

**Files:**
- Modify: `internal/infrastructure/config/motor-personal.yaml`
- Modify: `internal/application/generate_test.go` (section index constants) and the tests that use them: `pricing_test.go`, `realism_test.go`
- Modify: `internal/application/golden_test.go` (three hashes)
- Modify: `internal/infrastructure/web/server_test.go` (expected `realism.sections`)
- Modify: `tools/screenshots/screenshots.js` (the failing-run field index)

**Interfaces:**
- Consumes: `ScoredSections`, the multi-section `SectionComparison` (Task 1), and `lob.Lognormal` (Task 2).
- Produces: preset section order `own_damage` (0), `third_party_property` (1), `third_party_injury` (2).

- [ ] **Step 1: Replace the preset's third-party section**

In `motor-personal.yaml`, rewrite the header comment so it says the reference is Schedule P Part 1B (bodily injury and property damage liability, plus personal injury protection, medical payments and uninsured motorist, which this product does not carry), and that both third-party sections are scored together against it.

In `pricing.sections`, replace the `third_party` entry with:

```yaml
    - name: third_party_property
      base_frequency: 0.030
      severity: {kind: lognormal, median: 1800, sigma: 0.9}
    - name: third_party_injury
      base_frequency: 0.009
      severity: {kind: pareto, scale: 6000, alpha: 2.0}
```

In `claims.sections`, replace the `third_party` entry with the block below. Keep the comment style of the existing sections. The starting values keep the liability expected cost per policy-year close to today's, about 0.024 x (7,333 - 360) = $167 against 0.030 x 2,370 + 0.009 x 11,640 = $176. After the excess, the claim count runs about three property damage claims to each injury claim.

```yaml
    # Third-party property damage: the other party's vehicle and property.
    # The most common Schedule P Part 1B claim: reported within days, small,
    # and settled in weeks to months. Scored together with injury.
    - name: third_party_property
      base_frequency: 0.030
      severity: {kind: lognormal, median: 1800, sigma: 0.9}
      report_lag: {median: 3, sigma: 1.2}
      close_lag:
        shape: 1.2
        mean_days: 60
        size_reference: 2000
        size_elasticity: 0.2
        risk_loading: 0.3
      scored: true
    # Third-party bodily injury: rarer, heavy-tailed, reported later (median
    # 20 days, about 3.5% more than a year after the accident, which gives the
    # reported counts and the incurred some pure IBNR) and settled slowly.
    # Its lags carry the later-age development of the Schedule P reference.
    # Re-run the realism gate after any change to either third-party section.
    - name: third_party_injury
      base_frequency: 0.009
      severity: {kind: pareto, scale: 6000, alpha: 2.0}
      report_lag: {median: 20, sigma: 1.6}
      close_lag:
        shape: 1.0
        mean_days: 500
        size_reference: 10000
        size_elasticity: 0.25
        risk_loading: 0.3
      scored: true
```

Pricing must list the same names in the same order as claims. Keep the pricing values equal to the claims values, as the preset does today.

- [ ] **Step 2: Update the test section indices**

In `internal/application/generate_test.go`:

```go
// Section indices in the motor preset.
const (
	ownDamage          = 0
	thirdPartyProperty = 1
	thirdPartyInjury   = 2
)
```

Then fix every use of `thirdParty` in `internal/application`:

- `pricing_test.go:72`, the underpricing case, halves the injury scale. Also halve the property median: `under.Pricing.Sections[thirdPartyProperty].Severity.Median *= 0.5`.
- `pricing_test.go:107-108`, the own-damage-only case, zeroes both third-party sections' claims and pricing frequencies.
- `realism_test.go`:
  - In `TestScoredSectionPremiumAndClaims`, sum payments whose section is either third-party section.
  - In `TestRealismScoresTheUnionOfScoredSections`, pass `[]int{ownDamage, thirdPartyProperty, thirdPartyInjury}`.
  - Change the comment above `TestRealismScoresOnlyTheScoredSection` to "the scored third-party sections".

Then run `grep -rn "thirdParty\b" internal/application` to confirm nothing is left. The domain packages define their own `thirdParty` constants against their own params. Leave those alone.

In `internal/infrastructure/web/server_test.go`, expect `[]string{"third_party_property", "third_party_injury"}`. In `internal/infrastructure/config/config_test.go`, the test YAML has its own sections and needs no change. Run it to confirm.

- [ ] **Step 3: Run the realism gate**

Run: `go test ./internal/application/ -run 'TestDefaultPresetIsRealistic|TestPresetHasNoSystematicLossRatioDrift' -v`
Expected: the report for each seed. If a metric falls outside its band, recalibrate in this order and re-run after each change, moving one lever at a time:

1. **Paid ATA at ages 1-2 out of band.** This is the cost share of fast to slow claims. Move property `base_frequency` and injury `base_frequency` in opposite directions, keeping their combined expected cost near $170 per policy-year. Change the pricing values to match.
2. **Paid ATA at ages 3-10 out of band.** This is injury settlement speed. Change injury `close_lag.mean_days` (slower raises later factors) and then `size_elasticity`.
3. **Incurred ATA out of band.** This is case adequacy and the injury report lag. Change injury `report_lag.median` first. Change `runoff.case_adequacy_mean` only as a last resort, because it also moves own damage.
4. **Loss ratio or drift out of band.** This should not happen, because premium follows pricing (`target_loss_ratio` 0.72) and pricing equals claims. If it does, check that the pricing and claims sections match. Do not tune `target_loss_ratio` to pass. The loss ratio is emergent (see `TestPresetHasNoSystematicLossRatioDrift`).

Keep the values a reserving actuary would find believable: property median between $1,000 and $4,000, property mean close between 30 and 120 days, injury mean close between 300 and 900 days. If the gate cannot pass inside those ranges, stop and report the closest report to the maintainer rather than leaving the ranges.

Also run the gate on seeds 2, 3 and 11 once. Edit the seed list in place to do this, then revert the edit. The point is to check that the calibration is not tuned to seeds 1, 42 and 7.

- [ ] **Step 4: Refresh the golden hashes**

Run: `go test ./internal/application/ -run Golden`
Expected: three hash mismatches, each printing the actual value. The output moved on purpose because the preset changed. Paste the three values into `wantHash`, `wantAggregateHash` and `wantAnnualHash`, then re-run and confirm PASS.

- [ ] **Step 5: Update the screenshot script**

In `tools/screenshots/screenshots.js` around line 72, the failing run should raise the injury frequency. Change the path index from `1` to `2`. Update the comment, and the README caption in Task 4, to "a third-party injury base frequency of 0.1". Check by running the UI on port 8093 and the script, if Node is available. If not, say so in the PR body.

- [ ] **Step 6: Run everything**

Run: `go test ./... && go vet ./... && gofmt -l .`
Expected: PASS with no gofmt output.

- [ ] **Step 7: Commit**

```bash
git add -A internal tools
git commit -m "Split the preset's third party into property damage and injury"
```

---

### Task 4: docs

**Files:**
- Modify: `README.md`, `docs/architecture.md`, `docs/mission.md`, `docs/roadmap.md`, `docs/review.md`, `docs/todo.md` (only if it references the scored section)
- Delete: this plan file

- [ ] **Step 1: Update the README**
  - In "Claim events", the preset has three sections. Add a `lognormal` branch to the severity mermaid diagram ("lognormal: median median, sigma sigma, in start-year dollars"). Change the cap node to stay "sum_insured_lognormal only". Rewrite the paragraph after the diagram for property damage and injury.
  - In "Realism", describe the reference as Schedule P Part 1B and list what it includes (bodily injury and property damage liability, personal injury protection, medical payments, uninsured motorist) and excludes (physical damage, which is Part 1J with no 10-year history). The two third-party sections are scored together. State that the preset carries no first-party injury cover, and that the generated losses are gross of reinsurance and exclude defence costs, which the calibration absorbs implicitly. Update the mermaid "scored section only" node to "scored sections together".
  - Update the screenshot caption to the injury frequency.
  - Mention the lognormal kind in "Assumptions and known simplifications" only if something there names the severity kinds.
- [ ] **Step 2: Update the other living docs**
  - In `docs/architecture.md`, describe the realism gate as scoring the scored sections together.
  - In `docs/mission.md:43`, say "the third-party (liability) sections of the shipped preset".
  - In `docs/roadmap.md`, replace "Schedule P carries liability lines only, so a commercial *property* class has no direct reference family; commercial auto is the closest short-tail fit" with a correct statement. The embedded CAS extract holds six liability lines only. Schedule P itself has homeowners (Part 1A) and commercial multiple peril (Part 1E) on ten years, and special property (Part 1I) on a short history, none of which is embedded. So a commercial property class has no reference in the repo today.
  - In `docs/review.md`, delete MR-14 and renumber MR-12 to 1 and MR-8 to 2. The lognormal kind partly answers MR-8's "lognormal body" note. Edit MR-8 to drop the claim that a lognormal kind is missing, and keep the lognormal-body-with-Pareto-tail idea, the limit and the excess switch.
- [ ] **Step 3: Delete this plan and check the docs**

`git rm docs/superpowers/plans/2026-10-02-mr-14-split-third-party.md`. Then run `grep -rn "ScoredSection\b\|third_party\b\|realism.section\b" README.md docs AGENTS.md internal tools --include='*.md' --include='*.go' --include='*.js' --include='*.yaml'` and fix anything stale, but ignore `docs/superpowers/specs` and `docs/raw user inputs`.
- [ ] **Step 4: Commit, push and open the PR**

```bash
git add -A
git commit -m "Document the Part 1B split and close MR-14"
git push -u origin feature/mr-14-split-third-party
```

Open the PR with `gh pr create`. Title: "Split third-party cover into property damage and injury, scored together against Schedule P Part 1B". The body says what changed and why, names MR-14 as closed, states that `go test ./...` and `go vet ./...` pass, and gives the realism report's loss ratio and the paid ATA at age 1 for seed 1. Then stop: the maintainer reviews and merges.
