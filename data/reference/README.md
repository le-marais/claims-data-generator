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

The private passenger auto and commercial auto files are embedded
(`refdata.go`). The realism gate scores the personal motor preset against the
companies `application.PersonalMotorCriteria` selects from the first, and the
commercial motor preset against those `application.CommercialAutoCriteria`
selects from the second. The other four lines are kept for future lines of
business.
