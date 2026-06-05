# Methodology

cienergytool follows the **Software Carbon Intensity** specification — **ISO/IEC 21031:2024** ([sci.greensoftware.foundation](https://sci.greensoftware.foundation/)).

```
SCI = ( (E × I) + M ) / R
```

| Term | Meaning |
|---|---|
| **E** | Energy used by the build, in **kWh** |
| **I** | Marginal grid carbon intensity at the runner location/time, in **gCO₂eq/kWh** |
| **M** | Embodied carbon of the hardware, amortised over its lifetime, in **gCO₂eq** |
| **R** | Functional unit (1 pipeline run by default) |

---

## E — Energy

Sources are tried in this order; the first one available wins, and the choice is recorded in `energy.byStep[].source`:

| Priority | Source | When usable | Reference |
|---|---|---|---|
| 1 | **Intel/AMD RAPL** counters | bare-metal / privileged runner | [Intel RAPL](https://www.intel.com/content/www/us/en/developer/articles/technical/software-security-guidance/advisory-guidance/running-average-power-limit-energy-reporting.html); [Scaphandre](https://github.com/hubblo-org/scaphandre) |
| 2 | **Kepler** (eBPF) | self-hosted runners on Kubernetes | [sustainable-computing.io](https://sustainable-computing.io/) (CNCF Sandbox) |
| 3 | **NVML / DCGM** | GPU steps | [NVML](https://developer.nvidia.com/management-library-nvml) |
| 4 | **eco-ci model** | hosted runners (no privileged access) — `kWh = (TDP × utilPct/100 × duration) / 3600 / 1000` | [Green Coding Solutions — eco-ci](https://github.com/green-coding-solutions/eco-ci-energy-estimation) |
| 5 | **CCF model** | fallback for any cloud-hosted runner — vCPU-hours × cloud coefficients | [Cloud Carbon Footprint methodology](https://www.cloudcarbonfootprint.org/docs/methodology/) |
| 6 | **CodeCarbon / Carbontracker** | in-process Python ML steps | [CodeCarbon](https://codecarbon.io/); [Carbontracker](https://github.com/lfwa/carbontracker) |

For hosted GitHub runners (no `/sys/class/powercap` access), we use **source 4** by default. RAM and disk are not yet modelled in v1 (typically < 10 % of CPU on build workloads — to be added in v1.1 from CCF coefficients).

## I — Grid intensity

| Source | Update frequency | Notes |
|---|---|---|
| [Electricity Maps API](https://www.electricitymaps.com/) | hourly, real-time | preferred when token available |
| [WattTime API](https://www.watttime.org/) | 5-minute marginal | alternative |
| [Ember monthly dataset](https://ember-climate.org/data/) | monthly | offline / no-token fallback, bundled |
| CCF static coefficients | per cloud region | last-resort for cloud regions not covered |

Resolution order: `electricitymaps → watttime → ember-monthly → ccf-static`. The resolved value, its source, zone and timestamp are recorded in `carbon.gridIntensity`.

## M — Embodied carbon

Embodied carbon is fetched from the **Boavizta API** ([doc.api.boavizta.org](https://doc.api.boavizta.org/)) using the runner CPU model and amortised linearly over a **4-year hardware lifetime** (default; configurable).

`M_step = (M_total × duration_step) / lifetime_seconds`

When the CPU SKU is unknown, we fall back to a CCF static coefficient (`~250 kgCO₂eq` per server, amortised).

## R — Functional unit

Default: **1 pipeline run** (`R = 1`). Configurable per repo via `cienergy.yaml`:

```yaml
functionalUnit: "1 production deployment"
R: 0.1     # if this pipeline runs ~10× per deployment
```

## Counter-factual: cache savings

When a cache hit is detected, the tool estimates the energy that *would have been* consumed by the cold equivalent (recorded during the most recent cold run on the same workflow) and reports it in `cache.savedKWhEstimate` / `cache.savedGCO2eqEstimate`. This enables the dashboard's **"what would we have emitted without caching"** view.

## Confidence and audit

Every report records:

- `energy.byStep[].source` — which probe produced each number
- `carbon.gridIntensity.source` and `.timestamp` — when and where intensity was sampled
- `carbon.embodiedSource` — where the embodied value came from

This is the minimum trace required for **CSRD / ESRS E1** auditability ([EFRAG](https://www.efrag.org/lab6)) and aligns with **GHG Protocol Scope 2 (market-based)** reporting.

## Known limitations (v1.0)

1. **Model-based energy on hosted runners** — not a hardware measurement. Confidence interval ±20–30 % depending on workload. Validated against RAPL on self-hosted runners (see `test/calibration/`).
2. **RAM/disk/network** not modelled separately — folded into the CPU model.
3. **Embodied carbon of GPU** uses CCF static when Boavizta has no entry for the SKU.
4. **Renewable PPAs** of cloud providers are *not* netted out — we use location-based grid intensity, which is the conservative choice ([GHG Protocol Scope 2 Guidance, 2015](https://ghgprotocol.org/scope_2_guidance)).

## References

See [PLAN.md §10](../PLAN.md#10-references-all-validated-as-of-june-2026) for the complete bibliography.

