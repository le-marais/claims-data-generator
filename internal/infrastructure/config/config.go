// Package config maps YAML files onto the LineOfBusiness domain object.
// Decoding is strict: unknown keys fail, and the domain's own validation
// runs on every load.
package config

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/le-marais/claimsgen/internal/domain/lob"
)

//go:embed motor-personal.yaml
var motorPersonalYAML []byte

// LOBParams mirrors the domain structs so the domain stays free of yaml tags.
// These exported types also serve as the JSON shape of the web API.

type LOBParams struct {
	Name   string       `yaml:"name" json:"name"`
	Book   BookParams   `yaml:"book" json:"book"`
	Claims ClaimsParams `yaml:"claims" json:"claims"`
	Runoff RunoffParams `yaml:"runoff" json:"runoff"`
}

// BookParams mirrors lob.BookParams for YAML/JSON.
type BookParams struct {
	GrowthFactor        float64              `yaml:"growth_factor" json:"growth_factor"`
	SizeVolatility      float64              `yaml:"size_volatility" json:"size_volatility"`
	Spread              float64              `yaml:"spread" json:"spread"`
	SumInsuredMedian    float64              `yaml:"sum_insured_median" json:"sum_insured_median"`
	SumInsuredInflation float64              `yaml:"sum_insured_inflation" json:"sum_insured_inflation"`
	ExcessChoices       []ExcessChoiceParams `yaml:"excess_choices" json:"excess_choices"`
	TargetLossRatio     float64              `yaml:"target_loss_ratio" json:"target_loss_ratio"`
}

// ExcessChoiceParams mirrors lob.ExcessChoice for YAML/JSON.
type ExcessChoiceParams struct {
	Value  float64 `yaml:"value" json:"value"`
	Weight float64 `yaml:"weight" json:"weight"`
}

// ClaimsParams mirrors lob.ClaimParams for YAML/JSON.
type ClaimsParams struct {
	BaseFrequency   float64          `yaml:"base_frequency" json:"base_frequency"`
	ReportLagMedian float64          `yaml:"report_lag_median" json:"report_lag_median"`
	ReportLagSigma  float64          `yaml:"report_lag_sigma" json:"report_lag_sigma"`
	Severity        SeverityParams   `yaml:"severity" json:"severity"`
	CloseLag        CloseLagParams   `yaml:"close_lag" json:"close_lag"`
	Inflation       InflationParams  `yaml:"inflation" json:"inflation"`
	NilProbability  float64          `yaml:"nil_probability" json:"nil_probability"`
	Recoveries      RecoveriesParams `yaml:"recoveries" json:"recoveries"`
	Reopening       ReopeningParams  `yaml:"reopening" json:"reopening"`
}

// InflationParams mirrors lob.InflationParams for YAML/JSON.
type InflationParams struct {
	Mean       float64 `yaml:"mean" json:"mean"`
	Volatility float64 `yaml:"volatility" json:"volatility"`
}

// RecoveriesParams mirrors lob.RecoveryParams for YAML/JSON.
type RecoveriesParams struct {
	Salvage     RecoveryTypeParams `yaml:"salvage" json:"salvage"`
	Subrogation RecoveryTypeParams `yaml:"subrogation" json:"subrogation"`
}

// RecoveryTypeParams mirrors lob.RecoveryTypeParams for YAML/JSON.
type RecoveryTypeParams struct {
	Probability   float64 `yaml:"probability" json:"probability"`
	MeanShare     float64 `yaml:"mean_share" json:"mean_share"`
	Concentration float64 `yaml:"concentration" json:"concentration"`
	LagMedianDays float64 `yaml:"lag_median_days" json:"lag_median_days"`
	LagSigma      float64 `yaml:"lag_sigma" json:"lag_sigma"`
}

// ReopeningParams mirrors lob.ReopeningParams for YAML/JSON.
type ReopeningParams struct {
	Probability    float64 `yaml:"probability" json:"probability"`
	EstimateFactor float64 `yaml:"estimate_factor" json:"estimate_factor"`
	EstimateSigma  float64 `yaml:"estimate_sigma" json:"estimate_sigma"`
	LagMedianDays  float64 `yaml:"lag_median_days" json:"lag_median_days"`
	LagSigma       float64 `yaml:"lag_sigma" json:"lag_sigma"`
}

// SeverityParams mirrors lob.SeverityParams for YAML/JSON.
type SeverityParams struct {
	ThirdPartyWeight        float64 `yaml:"third_party_weight" json:"third_party_weight"`
	OwnDamageMedianFraction float64 `yaml:"own_damage_median_fraction" json:"own_damage_median_fraction"`
	OwnDamageSigma          float64 `yaml:"own_damage_sigma" json:"own_damage_sigma"`
	ThirdPartyScale         float64 `yaml:"third_party_scale" json:"third_party_scale"`
	ThirdPartyAlpha         float64 `yaml:"third_party_alpha" json:"third_party_alpha"`
}

// CloseLagParams mirrors lob.CloseLagParams for YAML/JSON.
type CloseLagParams struct {
	Shape              float64 `yaml:"shape" json:"shape"`
	MeanDays           float64 `yaml:"mean_days" json:"mean_days"`
	SizeThreshold      float64 `yaml:"size_threshold" json:"size_threshold"`
	SizeMultiplier     float64 `yaml:"size_multiplier" json:"size_multiplier"`
	RiskLoading        float64 `yaml:"risk_loading" json:"risk_loading"`
	ThirdPartyShape    float64 `yaml:"third_party_shape" json:"third_party_shape"`
	ThirdPartyMeanDays float64 `yaml:"third_party_mean_days" json:"third_party_mean_days"`
}

// RunoffParams mirrors lob.RunoffParams for YAML/JSON.
type RunoffParams struct {
	CaseAdequacyMean  float64 `yaml:"case_adequacy_mean" json:"case_adequacy_mean"`
	CaseAdequacySigma float64 `yaml:"case_adequacy_sigma" json:"case_adequacy_sigma"`
	PaymentsPerYear   float64 `yaml:"payments_per_year" json:"payments_per_year"`
	SettlementShare   float64 `yaml:"settlement_share" json:"settlement_share"`
	Concentration     float64 `yaml:"concentration" json:"concentration"`
	RevisionsPerYear  float64 `yaml:"revisions_per_year" json:"revisions_per_year"`
	RevisionSigma     float64 `yaml:"revision_sigma" json:"revision_sigma"`
}

func decode(r io.Reader) (LOBParams, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var dto LOBParams
	if err := dec.Decode(&dto); err != nil {
		return LOBParams{}, fmt.Errorf("parsing config: %w", err)
	}
	return dto, nil
}

// Load reads a line of business definition from YAML and validates it.
func Load(r io.Reader) (lob.LineOfBusiness, error) {
	dto, err := decode(r)
	if err != nil {
		return lob.LineOfBusiness{}, err
	}
	l := dto.ToDomain()
	if err := l.Validate(); err != nil {
		return lob.LineOfBusiness{}, fmt.Errorf("invalid config: %w", err)
	}
	return l, nil
}

// PresetInfo identifies one embedded line of business preset.
type PresetInfo struct {
	ID   string
	Name string
}

// New presets are registered here and in presetYAML; the UI picks them up
// with no further changes.
var presetInfos = []PresetInfo{
	{ID: "motor-personal", Name: "Motor personal"},
}

var presetYAML = map[string][]byte{
	"motor-personal": motorPersonalYAML,
}

// Presets lists the embedded presets in display order.
func Presets() []PresetInfo {
	return slices.Clone(presetInfos)
}

// PresetParams returns a preset's raw parameter set, e.g. to prefill an editor.
func PresetParams(id string) (LOBParams, error) {
	b, ok := presetYAML[id]
	if !ok {
		return LOBParams{}, fmt.Errorf("unknown preset %q", id)
	}
	return decode(bytes.NewReader(b))
}

// Preset returns an embedded preset as a validated domain object.
func Preset(id string) (lob.LineOfBusiness, error) {
	b, ok := presetYAML[id]
	if !ok {
		return lob.LineOfBusiness{}, fmt.Errorf("unknown preset %q", id)
	}
	return Load(bytes.NewReader(b))
}

// LoadFile loads a line of business definition from a YAML file.
func LoadFile(path string) (lob.LineOfBusiness, error) {
	f, err := os.Open(path)
	if err != nil {
		return lob.LineOfBusiness{}, fmt.Errorf("opening config: %w", err)
	}
	defer f.Close()
	return Load(f)
}

// MotorPersonal returns the embedded personal motor preset.
func MotorPersonal() (lob.LineOfBusiness, error) {
	return Load(bytes.NewReader(motorPersonalYAML))
}

func (d LOBParams) ToDomain() lob.LineOfBusiness {
	excesses := make([]lob.ExcessChoice, len(d.Book.ExcessChoices))
	for i, e := range d.Book.ExcessChoices {
		excesses[i] = lob.ExcessChoice{Value: e.Value, Weight: e.Weight}
	}
	return lob.LineOfBusiness{
		Name: d.Name,
		Book: lob.BookParams{
			GrowthFactor:        d.Book.GrowthFactor,
			SizeVolatility:      d.Book.SizeVolatility,
			Spread:              d.Book.Spread,
			SumInsuredMedian:    d.Book.SumInsuredMedian,
			SumInsuredInflation: d.Book.SumInsuredInflation,
			ExcessChoices:       excesses,
			TargetLossRatio:     d.Book.TargetLossRatio,
		},
		Claims: lob.ClaimParams{
			BaseFrequency:   d.Claims.BaseFrequency,
			ReportLagMedian: d.Claims.ReportLagMedian,
			ReportLagSigma:  d.Claims.ReportLagSigma,
			Severity: lob.SeverityParams{
				ThirdPartyWeight:        d.Claims.Severity.ThirdPartyWeight,
				OwnDamageMedianFraction: d.Claims.Severity.OwnDamageMedianFraction,
				OwnDamageSigma:          d.Claims.Severity.OwnDamageSigma,
				ThirdPartyScale:         d.Claims.Severity.ThirdPartyScale,
				ThirdPartyAlpha:         d.Claims.Severity.ThirdPartyAlpha,
			},
			CloseLag: lob.CloseLagParams{
				Shape:              d.Claims.CloseLag.Shape,
				MeanDays:           d.Claims.CloseLag.MeanDays,
				SizeThreshold:      d.Claims.CloseLag.SizeThreshold,
				SizeMultiplier:     d.Claims.CloseLag.SizeMultiplier,
				RiskLoading:        d.Claims.CloseLag.RiskLoading,
				ThirdPartyShape:    d.Claims.CloseLag.ThirdPartyShape,
				ThirdPartyMeanDays: d.Claims.CloseLag.ThirdPartyMeanDays,
			},
			Inflation: lob.InflationParams{
				Mean:       d.Claims.Inflation.Mean,
				Volatility: d.Claims.Inflation.Volatility,
			},
			NilProbability: d.Claims.NilProbability,
			Recoveries: lob.RecoveryParams{
				Salvage:     d.Claims.Recoveries.Salvage.toDomain(),
				Subrogation: d.Claims.Recoveries.Subrogation.toDomain(),
			},
			Reopening: lob.ReopeningParams{
				Probability:    d.Claims.Reopening.Probability,
				EstimateFactor: d.Claims.Reopening.EstimateFactor,
				EstimateSigma:  d.Claims.Reopening.EstimateSigma,
				LagMedianDays:  d.Claims.Reopening.LagMedianDays,
				LagSigma:       d.Claims.Reopening.LagSigma,
			},
		},
		Runoff: lob.RunoffParams{
			CaseAdequacyMean:  d.Runoff.CaseAdequacyMean,
			CaseAdequacySigma: d.Runoff.CaseAdequacySigma,
			PaymentsPerYear:   d.Runoff.PaymentsPerYear,
			SettlementShare:   d.Runoff.SettlementShare,
			Concentration:     d.Runoff.Concentration,
			RevisionsPerYear:  d.Runoff.RevisionsPerYear,
			RevisionSigma:     d.Runoff.RevisionSigma,
		},
	}
}

func (r RecoveryTypeParams) toDomain() lob.RecoveryTypeParams {
	return lob.RecoveryTypeParams{
		Probability:   r.Probability,
		MeanShare:     r.MeanShare,
		Concentration: r.Concentration,
		LagMedianDays: r.LagMedianDays,
		LagSigma:      r.LagSigma,
	}
}
