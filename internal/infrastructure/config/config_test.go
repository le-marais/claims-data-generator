package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
  premium_rate_factor: 0.03
claims:
  base_frequency: 0.15
  report_lag_median: 2
  report_lag_sigma: 1.0
  severity:
    third_party_weight: 0.15
    own_damage_median_fraction: 0.15
    own_damage_sigma: 1.0
    third_party_scale: 5000
    third_party_alpha: 2.0
  close_lag:
    shape: 1.5
    mean_days: 60
    size_threshold: 20000
    size_multiplier: 4
    risk_loading: 0.5
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
runoff:
  case_adequacy_mean: 1.0
  case_adequacy_sigma: 0.3
  payments_per_year: 3
  settlement_share: 0.4
  concentration: 1.0
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
	if l.Claims.Severity.ThirdPartyAlpha != 2.0 {
		t.Errorf("ThirdPartyAlpha = %v, want 2.0", l.Claims.Severity.ThirdPartyAlpha)
	}
	if l.Runoff.SettlementShare != 0.4 {
		t.Errorf("SettlementShare = %v, want 0.4", l.Runoff.SettlementShare)
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
}

func TestLoadRejectsMissingRecoveriesBlock(t *testing.T) {
	bad := strings.Replace(validYAML, "  recoveries:", "  recoveries_gone:", 1)
	if _, err := Load(strings.NewReader(bad)); err == nil {
		t.Fatal("config without a recoveries block: want error, got nil")
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
	got := Presets()
	want := []PresetInfo{{ID: "motor-personal", Name: "Motor personal"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Presets() = %+v, want %+v", got, want)
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
