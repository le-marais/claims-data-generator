package csv

import (
	"fmt"

	"github.com/le-marais/claimsgen/internal/application"
)

// WriteSectionDetail writes the opt-in section files into dir, creating it if
// needed. sections are the line of business's section names, in order.
//
// claim_sections.csv has one row per claim, in claims.csv order, naming the
// section of cover the claim fell in.
//
// exposure_sections.csv has one row per origin month and section, origin
// months in exposure.csv order and sections in line-of-business order. The
// premium is the month's premium for the section on the run's origin basis,
// aggregated with exposure.csv's premium, so the sections add up to it to
// within the cent each policy's section split is rounded to.
func WriteSectionDetail(dir string, sections []string, ds application.Dataset, ag application.Aggregates) error {
	open, err := dirOpener(dir)
	if err != nil {
		return err
	}
	return writeSectionDetail(open, sections, ds, ag)
}

func writeSectionDetail(open opener, sections []string, ds application.Dataset, ag application.Aggregates) error {
	for i, c := range ds.Claims {
		if c.Section < 0 || c.Section >= len(sections) {
			return fmt.Errorf("claim %d (row %d): section index %d outside the line of business's %d sections", c.ID, i+1, c.Section, len(sections))
		}
	}
	for _, e := range ag.Exposure {
		if len(e.SectionPremiums) != len(sections) {
			return fmt.Errorf("exposure for %s: %d section premiums, want the line of business's %d", e.Month, len(e.SectionPremiums), len(sections))
		}
	}
	if err := writeFile(open, "claim_sections.csv", "claim_id,section",
		len(ds.Claims), func(i int) string {
			c := ds.Claims[i]
			return fmt.Sprintf("%d,%s", c.ID, sections[c.Section])
		}); err != nil {
		return err
	}
	return writeFile(open, "exposure_sections.csv", "origin_month,section,premium",
		len(ag.Exposure)*len(sections), func(i int) string {
			e := ag.Exposure[i/len(sections)]
			s := i % len(sections)
			return fmt.Sprintf("%s,%s,%s", e.Month, sections[s], formatAmount(e.SectionPremiums[s]))
		})
}
