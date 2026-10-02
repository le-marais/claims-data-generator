package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/lob"
)

const validYAML = `
name: test-lob
book:
  growth_factor: 1.05
  size_volatility: 0.05
  spread: 0.4
  sum_insured_median: 20000
  sum_insured_inflation: 1.03
  excess_choices:
    - {value: 0, weight: 0.1}
    - {value: 500, weight: 0.9}
pricing:
  target_loss_ratio: 0.72
  sections:
    - name: own_damage
      base_frequency: 0.1275
      severity: {kind: sum_insured_lognormal, median_fraction: 0.15, sigma: 1.0}
    - name: third_party
      base_frequency: 0.0225
      severity: {kind: pareto, scale: 5000, alpha: 2.0}
  nil_probability: 0.05
  reopen_probability: 0.04
  reopen_estimate_factor: 0.45
  inflation_mean: 1.04
claims:
  sections:
    - name: own_damage
      base_frequency: 0.1275
      severity: {kind: sum_insured_lognormal, median_fraction: 0.15, sigma: 1.0}
      report_lag: {median: 2, sigma: 1.0}
      close_lag: {shape: 1.5, mean_days: 60, size_reference: 3000, size_elasticity: 0.2, risk_loading: 0.5}
      settlement: {lump_sum_probability: 0.5, share: 0.25, concentration: 4}
      recoveries: true
    - name: third_party
      base_frequency: 0.0225
      severity: {kind: pareto, scale: 5000, alpha: 2.0}
      report_lag: {median: 20, sigma: 1.5}
      close_lag: {shape: 1.0, mean_days: 900, risk_loading: 0.5}
      settlement: {share: 0.6, concentration: 4}
  inflation:
    mean: 1.04
    volatility: 0.02
  nil_probability: 0.05
  recoveries:
    salvage:
      probability: 0.1
      mean_share: 0.15
      concentration: 10
      lag_median_days: 21
      lag_sigma: 0.5
    subrogation:
      probability: 0.2
      mean_share: 0.8
      concentration: 10
      lag_median_days: 180
      lag_sigma: 0.7
  reopening:
    probability: 0.04
    estimate_factor: 0.45
    estimate_sigma: 0.5
    lag_median_days: 90
    lag_sigma: 0.7
runoff:
  case_adequacy_mean: 1.0
  case_adequacy_sigma: 0.3
  payments_per_year: 3
  concentration: 4
  min_payment: 50
  revisions_per_year: 4
  revision_sigma: 0.3
`

func TestLoadValidYAML(t *testing.T) {
	l, err := Load(strings.NewReader(validYAML))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if l.Name != "test-lob" {
		t.Errorf("Name = %q, want test-lob", l.Name)
	}
	if l.Book.GrowthFactor != 1.05 {
		t.Errorf("GrowthFactor = %v, want 1.05", l.Book.GrowthFactor)
	}
	if len(l.Book.ExcessChoices) != 2 || l.Book.ExcessChoices[1].Value != 500 {
		t.Errorf("ExcessChoices = %+v, want two entries with second value 500", l.Book.ExcessChoices)
	}
	if len(l.Claims.Sections) != 2 {
		t.Fatalf("claims sections = %d, want 2", len(l.Claims.Sections))
	}
	if tp := l.Claims.Sections[1]; tp.Severity.Kind != lob.Pareto || tp.Severity.Alpha != 2.0 || tp.Recoveries {
		t.Errorf("third-party section = %+v, want a Pareto with alpha 2.0 and no recoveries", tp)
	}
	if od := l.Claims.Sections[0]; od.CloseLag.SizeElasticity != 0.2 || !od.Recoveries {
		t.Errorf("own-damage section = %+v, want size elasticity 0.2 and recoveries", od)
	}
	if st := l.Claims.Sections[0].Settlement; st != (lob.SettlementParams{LumpSumProbability: 0.5, Share: 0.25, Concentration: 4}) {
		t.Errorf("own-damage settlement = %+v, want lump sum 0.5, share 0.25, concentration 4", st)
	}
	if l.Runoff.MinPayment != 50 {
		t.Errorf("MinPayment = %v, want 50", l.Runoff.MinPayment)
	}
	if l.Claims.Inflation.Mean != 1.04 {
		t.Errorf("inflation mean = %v, want 1.04", l.Claims.Inflation.Mean)
	}
	if l.Claims.Inflation.Volatility != 0.02 {
		t.Errorf("inflation volatility = %v, want 0.02", l.Claims.Inflation.Volatility)
	}
	if l.Claims.NilProbability != 0.05 {
		t.Errorf("nil_probability = %v, want 0.05", l.Claims.NilProbability)
	}
	if l.Claims.Recoveries.Salvage.MeanShare != 0.15 {
		t.Errorf("salvage mean_share = %v, want 0.15", l.Claims.Recoveries.Salvage.MeanShare)
	}
	if l.Claims.Recoveries.Subrogation.LagMedianDays != 180 {
		t.Errorf("subrogation lag_median_days = %v, want 180", l.Claims.Recoveries.Subrogation.LagMedianDays)
	}
	if l.Claims.Reopening.Probability != 0.04 {
		t.Errorf("reopening probability = %v, want 0.04", l.Claims.Reopening.Probability)
	}
	if l.Claims.Reopening.LagMedianDays != 90 {
		t.Errorf("reopening lag_median_days = %v, want 90", l.Claims.Reopening.LagMedianDays)
	}
}

func TestLoadRejectsMissingRecoveriesBlock(t *testing.T) {
	bad := strings.Replace(validYAML, "  recoveries:", "  recoveries_gone:", 1)
	if _, err := Load(strings.NewReader(bad)); err == nil {
		t.Fatal("config without a recoveries block: want error, got nil")
	}
}

func TestLoadRejectsMissingReopeningBlock(t *testing.T) {
	bad := strings.Replace(validYAML, "  reopening:", "  reopening_gone:", 1)
	if _, err := Load(strings.NewReader(bad)); err == nil {
		t.Fatal("config without a reopening block: want error, got nil")
	}
}

// A switched-off recovery or reopen block needs only its probability (MF-3).
func TestLoadAcceptsSwitchedOffBlockWithOnlyProbability(t *testing.T) {
	off := strings.Replace(validYAML, `    salvage:
      probability: 0.1
      mean_share: 0.15
      concentration: 10
      lag_median_days: 21
      lag_sigma: 0.5
`, `    salvage:
      probability: 0
`, 1)
	off = strings.Replace(off, `  reopening:
    probability: 0.04
    estimate_factor: 0.45
    estimate_sigma: 0.5
    lag_median_days: 90
    lag_sigma: 0.7
`, `  reopening:
    probability: 0
`, 1)
	if off == validYAML {
		t.Fatal("fixture replacement did not apply")
	}
	if _, err := Load(strings.NewReader(off)); err != nil {
		t.Fatalf("switched-off blocks with only a probability: want nil, got %v", err)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	bad := strings.Replace(validYAML, "growth_factor:", "growht_factor:", 1)
	if _, err := Load(strings.NewReader(bad)); err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
}

func TestLoadRejectsInvalidValuesWithFieldName(t *testing.T) {
	bad := strings.Replace(validYAML, "spread: 0.4", "spread: 0", 1)
	_, err := Load(strings.NewReader(bad))
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "book.spread") {
		t.Errorf("error %q does not name book.spread", err.Error())
	}
}

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lob.yaml")
	if err := os.WriteFile(path, []byte(validYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if l.Name != "test-lob" {
		t.Errorf("Name = %q, want test-lob", l.Name)
	}
}

func TestLoadFileMissing(t *testing.T) {
	if _, err := LoadFile("/does/not/exist.yaml"); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestMotorPersonalPresetIsValid(t *testing.T) {
	l, err := MotorPersonal()
	if err != nil {
		t.Fatalf("embedded preset failed to load: %v", err)
	}
	if l.Name != "motor-personal" {
		t.Errorf("preset name = %q, want motor-personal", l.Name)
	}
}

func TestPresets(t *testing.T) {
	var got []string
	for _, p := range Presets() {
		got = append(got, p.ID+" "+p.Name)
	}
	want := []string{"motor-personal Motor personal", "motor-commercial Motor commercial"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Presets() = %v, want %v", got, want)
	}
}

// Every preset is scored against a known reference line, on sections it has.
func TestPresetsHaveRealismProfiles(t *testing.T) {
	lines := map[string]bool{}
	for _, l := range application.ReferenceLines() {
		lines[l.ID] = true
	}
	for _, p := range Presets() {
		if !lines[p.Realism.Line] {
			t.Errorf("%s: realism line %q is not a reference line", p.ID, p.Realism.Line)
		}
		l, err := Preset(p.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Realism.SectionIndices(l); err != nil {
			t.Errorf("%s: %v", p.ID, err)
		}
		info, ok := PresetInfoFor(p.ID)
		if !ok || !reflect.DeepEqual(info, p) {
			t.Errorf("PresetInfoFor(%s) = %+v, %v; want %+v", p.ID, info, ok, p)
		}
	}
	if _, ok := PresetInfoFor("marine-cargo"); ok {
		t.Error("PresetInfoFor(marine-cargo) found a preset")
	}
}

// Presets hands out copies, so a caller cannot change the registry.
func TestPresetsReturnsACopy(t *testing.T) {
	Presets()[0].Realism.Sections[0] = "changed"
	if got := Presets()[0].Realism.Sections[0]; got == "changed" {
		t.Fatal("changing a returned preset changed the registry")
	}
}

func TestPresetParamsRoundTrip(t *testing.T) {
	params, err := PresetParams("motor-personal")
	if err != nil {
		t.Fatal(err)
	}
	want, err := MotorPersonal()
	if err != nil {
		t.Fatal(err)
	}
	if got := params.ToDomain(); !reflect.DeepEqual(got, want) {
		t.Fatalf("PresetParams().ToDomain() = %+v, want %+v", got, want)
	}
}

func TestPresetKnown(t *testing.T) {
	l, err := Preset("motor-personal")
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "motor-personal" {
		t.Fatalf("Preset name = %q, want motor-personal", l.Name)
	}
}

func TestPresetUnknown(t *testing.T) {
	if _, err := Preset("marine-cargo"); err == nil {
		t.Fatal("Preset(marine-cargo): want error, got nil")
	}
	if _, err := PresetParams("marine-cargo"); err == nil {
		t.Fatal("PresetParams(marine-cargo): want error, got nil")
	}
}

// leaves records every float64 and string leaf of a struct tree by its Go
// field-name path, with slice elements indexed, so two parallel trees can be
// compared field by field.
func leaves(v reflect.Value, path string, out map[string]any) {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			leaves(v.Field(i), path+"."+v.Type().Field(i).Name, out)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			leaves(v.Index(i), fmt.Sprintf("%s[%d]", path, i), out)
		}
	case reflect.Float64:
		out[path] = v.Float()
	case reflect.String:
		out[path] = v.String()
	case reflect.Bool:
		out[path] = v.Bool()
	default:
		panic(fmt.Sprintf("leaves: unhandled kind %s at %s", v.Kind(), path))
	}
}

// fillDistinct sets every float64 leaf to a distinct value (1, 2, 3, ...),
// every string to its path, every bool by the parity of the same counter (so
// neighbouring switches differ), and gives every slice two elements.
func fillDistinct(v reflect.Value, path string, next *float64) {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			fillDistinct(v.Field(i), path+"."+v.Type().Field(i).Name, next)
		}
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 2, 2))
		for i := 0; i < v.Len(); i++ {
			fillDistinct(v.Index(i), fmt.Sprintf("%s[%d]", path, i), next)
		}
	case reflect.Float64:
		*next++
		v.SetFloat(*next)
	case reflect.String:
		v.SetString(path)
	case reflect.Bool:
		*next++
		v.SetBool(int(*next)%2 == 1)
	}
}

// RF-13: ToDomain must carry every config field to the domain field of the
// same name, and the domain must have no field the config cannot set. A
// forgotten line in ToDomain would otherwise zero the parameter silently -
// and since a zero often means "off" (MF-3), it could even pass validation.
// Distinct values also catch two fields swapped or one copied twice.
func TestToDomainMapsEveryField(t *testing.T) {
	var params LOBParams
	next := 0.0
	fillDistinct(reflect.ValueOf(&params).Elem(), "", &next)

	got, want := map[string]any{}, map[string]any{}
	leaves(reflect.ValueOf(params.ToDomain()), "", got)
	leaves(reflect.ValueOf(params), "", want)
	for path, v := range want {
		if got[path] != v {
			t.Errorf("domain %s = %v, want %v from the config", path, got[path], v)
		}
	}
	for path := range got {
		if _, ok := want[path]; !ok {
			t.Errorf("domain field %s has no config field of the same name", path)
		}
	}
}
