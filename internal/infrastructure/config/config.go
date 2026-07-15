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

	"gopkg.in/yaml.v3"

	"github.com/le-marais/claimsgen/internal/domain/lob"
)

//go:embed motor-personal.yaml
var motorPersonalYAML []byte

// DTOs mirror the domain structs so the domain stays free of yaml tags.

type lobDTO struct {
	Name   string    `yaml:"name"`
	Book   bookDTO   `yaml:"book"`
	Claims claimsDTO `yaml:"claims"`
	Runoff runoffDTO `yaml:"runoff"`
}

type bookDTO struct {
	GrowthFactor        float64     `yaml:"growth_factor"`
	SizeVolatility      float64     `yaml:"size_volatility"`
	Spread              float64     `yaml:"spread"`
	SumInsuredMedian    float64     `yaml:"sum_insured_median"`
	SumInsuredInflation float64     `yaml:"sum_insured_inflation"`
	ExcessChoices       []excessDTO `yaml:"excess_choices"`
	PremiumRateFactor   float64     `yaml:"premium_rate_factor"`
}

type excessDTO struct {
	Value  float64 `yaml:"value"`
	Weight float64 `yaml:"weight"`
}

type claimsDTO struct {
	BaseFrequency   float64     `yaml:"base_frequency"`
	ReportLagMedian float64     `yaml:"report_lag_median"`
	ReportLagSigma  float64     `yaml:"report_lag_sigma"`
	Severity        severityDTO `yaml:"severity"`
	CloseLag        closeLagDTO `yaml:"close_lag"`
}

type severityDTO struct {
	ThirdPartyWeight        float64 `yaml:"third_party_weight"`
	OwnDamageMedianFraction float64 `yaml:"own_damage_median_fraction"`
	OwnDamageSigma          float64 `yaml:"own_damage_sigma"`
	ThirdPartyScale         float64 `yaml:"third_party_scale"`
	ThirdPartyAlpha         float64 `yaml:"third_party_alpha"`
}

type closeLagDTO struct {
	Shape          float64 `yaml:"shape"`
	MeanDays       float64 `yaml:"mean_days"`
	SizeThreshold  float64 `yaml:"size_threshold"`
	SizeMultiplier float64 `yaml:"size_multiplier"`
	RiskLoading    float64 `yaml:"risk_loading"`
}

type runoffDTO struct {
	CaseAdequacyMean  float64 `yaml:"case_adequacy_mean"`
	CaseAdequacySigma float64 `yaml:"case_adequacy_sigma"`
	PaymentsPerYear   float64 `yaml:"payments_per_year"`
	SettlementShare   float64 `yaml:"settlement_share"`
	Concentration     float64 `yaml:"concentration"`
	RevisionsPerYear  float64 `yaml:"revisions_per_year"`
	RevisionSigma     float64 `yaml:"revision_sigma"`
}

// Load reads a line of business definition from YAML and validates it.
func Load(r io.Reader) (lob.LineOfBusiness, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var dto lobDTO
	if err := dec.Decode(&dto); err != nil {
		return lob.LineOfBusiness{}, fmt.Errorf("parsing config: %w", err)
	}
	l := dto.toDomain()
	if err := l.Validate(); err != nil {
		return lob.LineOfBusiness{}, fmt.Errorf("invalid config: %w", err)
	}
	return l, nil
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

func (d lobDTO) toDomain() lob.LineOfBusiness {
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
			PremiumRateFactor:   d.Book.PremiumRateFactor,
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
				Shape:          d.Claims.CloseLag.Shape,
				MeanDays:       d.Claims.CloseLag.MeanDays,
				SizeThreshold:  d.Claims.CloseLag.SizeThreshold,
				SizeMultiplier: d.Claims.CloseLag.SizeMultiplier,
				RiskLoading:    d.Claims.CloseLag.RiskLoading,
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
