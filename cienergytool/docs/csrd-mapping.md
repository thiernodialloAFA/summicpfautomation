# CSRD / ESRS E1 mapping

This document explains how cienergy reports map to the **EU Corporate
Sustainability Reporting Directive** (CSRD, [Directive 2022/2464](https://eur-lex.europa.eu/eli/dir/2022/2464/oj))
and the underlying **European Sustainability Reporting Standards** —
specifically [**ESRS E1 — Climate Change**](https://www.efrag.org/lab6)
(disclosure E1-6: *Gross Scopes 1, 2, 3 and total GHG emissions*).

> CSRD is **mandatory for large EU companies, including AXA, from
> FY 2024**, and report metrics must be auditable.

## Scope mapping

cienergy measures CI/CD pipeline execution, which falls entirely outside
Scope 1 (no direct combustion on the runners). The two relevant buckets:

| GHG Protocol bucket | cienergy field | Why |
|---|---|---|
| **Scope 2 — Indirect emissions from purchased energy** (location-based) | `carbon.operationalGCO2eq` | Electricity drawn by the runner during the build, converted via the grid intensity of the runner location (Electricity Maps / Ember). Conforms to [GHG Protocol Scope 2 Guidance (2015)](https://ghgprotocol.org/scope_2_guidance). |
| **Scope 3, Category 1 — Purchased goods and services** (embodied carbon) | `carbon.embodiedGCO2eq` | Amortised share of the runner hardware's manufacturing carbon for the duration of the build. Resolved via the [Boavizta API](https://doc.api.boavizta.org/). |

The **`carbon.totalGCO2eq`** field is the sum of the two and is what feeds
the ESRS E1-6 "total GHG emissions" line item for the CI workload.

## Why not market-based Scope 2?

Cloud providers publish PPA-based, market-based Scope 2 numbers that often
net out renewables. cienergy v1.0 reports the **location-based** view by
default, because:

1. ESRS E1-6 requires **both** location- and market-based figures to be
   disclosed when both are available (`§ AR 39`).
2. Location-based is the conservative default — auditors prefer not to be
   surprised by a sudden decarbonisation tied to a procurement contract.
3. Market-based requires verified instrument tracking the agent does not
   have access to. It can be re-applied downstream during consolidation.

## CSV export

The `cienergy-csrd-export` binary aggregates one or many reports at the
chosen granularity and emits a CSV whose columns are aligned with the
ESRS E1-6 disclosure tables:

```sh
cienergy-csrd-export \
    --in    ./reports/2025-Q4/ \
    --by    team \
    --period 2025-Q4 \
    --entity 'AXA SA' \
    --method location-based \
    --out    csrd-2025-Q4-by-team.csv
```

### Columns produced

| Column | Source | Notes |
|---|---|---|
| `reporting_entity`               | `--entity`              | Legal entity reporting the figures |
| `reporting_period`               | `--period`              | Free-text, copy to every row (e.g. `2025-Q4`) |
| `scope2_method`                  | `--method`              | `location-based` (default) or `market-based` |
| `aggregation_key`                | per `--by`              | run id / day / month / repository / team / cost-center |
| `runs_count`                     | computed                | Number of runs in the bucket |
| `energy_kwh`                     | sum of `energy.totalKWh` | Underlying activity data |
| `scope2_gco2eq_location_based`   | sum of `carbon.operationalGCO2eq` | The Scope 2 line |
| `scope3_cat1_gco2eq_embodied`    | sum of `carbon.embodiedGCO2eq`    | The Scope 3 Cat 1 line |
| `total_gco2eq`                   | sum of `carbon.totalGCO2eq`       | Disclosed total |
| `sci_mean_gco2eq_per_run`        | mean of `sci.value`               | Software Carbon Intensity (ISO/IEC 21031:2024) |
| `esrs_e1_6_reference`            | constant                | `ESRS E1-6 (gross Scopes 1,2,3 + total GHG)` |

### Granularities

`--by run | day | month | repository | team | cost-center`

For consolidation in your central CSRD spreadsheet, use `--by month` and
roll up; for cost-attribution dashboards, use `--by cost-center`.

## Audit trail

Every cienergy report records the provenance of every number — this is the
minimum chain of evidence an auditor will ask for:

| Field | What it proves |
|---|---|
| `energy.byStep[].source` | Which probe produced the kWh number (`rapl`, `kepler`, `nvml`, `eco-ci-model`, `ccf-model`, `codecarbon`) |
| `carbon.gridIntensity.source` + `.timestamp` + `.zone` | When and where grid intensity was sampled |
| `carbon.embodiedSource` | `boavizta` / `ccf-static` / `user-override` |
| `sci.functionalUnit` + `sci.R` | The denominator used to compute SCI |

All raw reports are stored in Postgres (`runs.raw` JSONB column) so the
auditor can replay the aggregation at any time.

## What this is **not**

- ❌ A full corporate GHG inventory. cienergy covers **only** the CI/CD
  pipeline footprint. Production runtime, end-user devices, employee
  commuting etc. are out of scope.
- ❌ A certified assurance report. The numbers are sourced and traceable
  but ESRS E1 requires **limited assurance** by a third party (rising to
  reasonable assurance later).
- ❌ A market-based Scope 2 number. See [above](#why-not-market-based-scope-2).

## References

- EU Directive 2022/2464 (CSRD): https://eur-lex.europa.eu/eli/dir/2022/2464/oj
- EFRAG ESRS E1 — Climate Change: https://www.efrag.org/lab6
- GHG Protocol Corporate Standard: https://ghgprotocol.org/corporate-standard
- GHG Protocol Scope 2 Guidance (2015): https://ghgprotocol.org/scope_2_guidance
- ISO/IEC 21031:2024 — Software Carbon Intensity: https://sci.greensoftware.foundation/
- Boavizta API (embodied carbon): https://doc.api.boavizta.org/
- Electricity Maps (grid intensity): https://www.electricitymaps.com/
- Ember Global Electricity Review (offline dataset): https://ember-climate.org/data/

