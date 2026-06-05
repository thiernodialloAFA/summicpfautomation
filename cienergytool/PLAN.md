# CI Energy Tool — Implementation Plan

> **Mission.** Build an open, vendor-neutral tool that measures the **energy consumption** and **carbon footprint** of every CI/CD pipeline run (classic backend, data, ML, agent workflows), emits **SCI-compliant JSON**, and surfaces the data in a **dashboard** that becomes a regression test for build-time sustainability.
>
> Companion to the AXA Summit 2026 talk *"Build-Time Energy: The Invisible Kilowatts in Your CI"*.

---

## 1. Why this tool

| Driver | Source |
|---|---|
| CI runners are the **largest unmeasured energy line** in cloud-native estates. | [CNCF Cloud Native Sustainability Whitepaper, 2023](https://tag-env-sustainability.cncf.io/publications/cloud-native-sustainability-whitepaper/) |
| ICT = **3–4 % of global GHG emissions**, growing ~6 %/yr. | [Shift Project, *Lean ICT*, 2019](https://theshiftproject.org/en/article/lean-ict-our-new-report/); [ADEME × Arcep, mars 2023](https://www.arcep.fr/la-regulation/grands-dossiers-thematiques-transverses/lempreinte-environnementale-du-numerique.html) |
| Data centres = **~1.5 % of global electricity in 2024**, projected to **~3 % by 2030**. | [IEA, *Electricity 2024* report](https://www.iea.org/reports/electricity-2024) |
| Training one large model can emit **hundreds of t CO₂eq**; ~10 % is pure pipeline overhead (restarts, idle). | [Patterson et al., 2021](https://arxiv.org/abs/2104.10350); [Luccioni et al., 2022](https://arxiv.org/abs/2211.02001) |
| **CSRD / ESRS E1** mandates Scope 1/2/3 reporting for large EU companies (incl. AXA) from FY 2024. | [EFRAG ESRS E1](https://www.efrag.org/lab6) |
| **SCI (ISO/IEC 21031:2024)** standardises the unit `gCO₂eq / functional unit`. | [GSF SCI spec](https://sci.greensoftware.foundation/) |

---

## 2. Scope & non-goals

### In scope (v1)

1. **GitHub Actions** runners (Linux x86_64 + ARM64; hosted + self-hosted) — composite action.
2. **Azure DevOps Pipelines** (Microsoft-hosted + self-hosted agents) — reusable step-list wrapper template (`pipeline/azure-devops/cienergy-step-template.yml`).
3. Per-step / per-job / per-workflow energy + CO₂ estimation.
4. **SCI-compliant JSON** output written to artifacts + optional push to S3 / OTLP / Prometheus.
5. **Reference dashboards** — Mode A (Grafana stack) and Mode B (zero-dependency embedded HTML/JS/CSS).
6. CLI for local replay (`cienergy run`, `cienergy report`).

### Stretch (v1.x)

- GitLab CI, Jenkins collectors.
- GPU energy attribution (NVML / DCGM).
- Kubernetes-native mode via **Kepler** for self-hosted runner pools.
- **Embodied carbon** of the runner hardware via Boavizta.

### Out of scope

- Production-runtime energy of the deployed app (different concern, different tool).
- Replacing financial FinOps tools.

---

## 3. Methodology

The estimation pipeline follows the **SCI formula** (ISO/IEC 21031:2024):

```
SCI = ( (E × I) + M ) / R
```

| Term | Meaning | Source we use |
|---|---|---|
| **E** | Energy used by the build (kWh) | `eco-ci` model + RAPL (`scaphandre`) when available; NVML for GPU |
| **I** | Marginal grid carbon intensity (gCO₂eq/kWh) at the runner location/time | [Electricity Maps API](https://www.electricitymaps.com/) (free tier) or [WattTime](https://www.watttime.org/) |
| **M** | Embodied carbon of the hardware, amortised over its lifetime | [Boavizta API](https://doc.api.boavizta.org/) (Datavizta) |
| **R** | Functional unit (1 build, 1 PR, 1 test, 1 GB processed…) | configurable per repo |

### Energy estimation paths (in priority order)

| Source | When usable | Reference |
|---|---|---|
| **Intel/AMD RAPL** counters via `scaphandre` or `powerstat` | bare-metal / privileged runners | [Scaphandre (hubblo-org)](https://github.com/hubblo-org/scaphandre); [Intel RAPL](https://www.intel.com/content/www/us/en/developer/articles/technical/software-security-guidance/advisory-guidance/running-average-power-limit-energy-reporting.html) |
| **Kepler** (eBPF) for k8s self-hosted runners | self-hosted on Kubernetes | [Kepler / sustainable-computing.io](https://sustainable-computing.io/) (CNCF Sandbox) |
| **NVML / DCGM** for GPU jobs | ML training/fine-tune | [NVIDIA Management Library](https://developer.nvidia.com/management-library-nvml) |
| **eco-ci** estimation model (CPU TDP × utilisation × wall-time) | hosted runners (no privileged access) | [Green Coding Solutions — eco-ci](https://github.com/green-coding-solutions/eco-ci-energy-estimation) |
| **Cloud Carbon Footprint** formulas (vCPU-hours × coefficients) | fallback for any cloud-hosted runner | [CCF methodology](https://www.cloudcarbonfootprint.org/docs/methodology/) |
| **CodeCarbon / Carbontracker** | inside Python ML steps | [CodeCarbon](https://codecarbon.io/); [Carbontracker](https://github.com/lfwa/carbontracker) |

### Why three layers?

Hosted GitHub runners don't expose RAPL → we need a **model-based** fallback. Self-hosted on k8s can use **Kepler** for ground-truth attribution. ML jobs need **GPU-aware** probes. The tool picks the best available source per step and records the method in the JSON (`measurement.source = "rapl" | "kepler" | "nvml" | "eco-ci-model" | "ccf-model"`).

---

## 4. Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  CI runner (GitHub Actions / GitLab / self-hosted k8s)          │
│                                                                  │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────────────┐│
│  │ pre-job hook │──▶│  collector   │──▶│ post-job aggregator  ││
│  │ (start probe)│   │ (RAPL/NVML/  │   │ (per-step SCI JSON)  ││
│  └──────────────┘   │  eco-ci/CCF) │   └──────────┬───────────┘│
│                     └──────────────┘              │            │
└────────────────────────────────────────────────────┼────────────┘
                                                    │
                          ┌─────────────────────────┼─────────────┐
                          ▼                         ▼             ▼
                  artifact:                  OTLP exporter   S3 / GCS
                  energy-report.json         (Prometheus)    bucket
                          │                         │             │
                          └──────────┬──────────────┴─────────────┘
                                     ▼
                          ┌──────────────────────┐
                          │  Storage layer        │
                          │  (Postgres / TSDB)    │
                          └──────────┬───────────┘
                                     ▼
                          ┌──────────────────────┐
                          │  Dashboard            │
                          │  (Grafana + custom)   │
                          └──────────────────────┘
```

### Components

1. **`cienergy-collector`** (Go binary, single static executable, < 10 MB).
   - Probes available energy sources at start/end of each step.
   - Writes raw samples to `/tmp/cienergy/*.jsonl`.

2. **`cienergy-aggregator`** (Go).
   - Reads raw samples + GitHub Actions context (`$GITHUB_*` env vars).
   - Resolves grid intensity (Electricity Maps API, cached).
   - Resolves embodied carbon (Boavizta API, cached per SKU).
   - Computes SCI and emits the canonical JSON (see §5).

3. **`cienergy-action`** — thin GitHub Action wrapper (`uses: axa-oss/cienergy-action@v1`).
   - Pre-step: install collector, start probe.
   - Post-step: run aggregator, upload artifact, optional OTLP push.

4. **`cienergy-cli`** — local replay & diff (`cienergy diff main..feature`).

5. **`cienergy-dashboard`** — two delivery modes, same data model:
   - **Mode A — Grafana stack.** podman Compose: Postgres + Grafana + prebuilt dashboards (`workflow energy`, `repo trend`, `team leaderboard`, `SCI vs. baseline`). Best for orgs that already run Grafana.
   - **Mode B — Embedded static dashboard (zero-dependency).** A self-contained **HTML + vanilla JS + CSS** single-page app that reads one or many `energy-report.json` files directly (file upload, drag-and-drop, or `?src=` URL). No backend, no build step, no framework. Bundled assets:
     - `index.html` — single file, < 30 KB gzipped.
     - `app.js` — vanilla ES2022, no npm runtime deps; uses [Chart.js](https://www.chartjs.org/) (UMD, vendored, MIT) for charts and [Ajv](https://ajv.js.org/) (UMD, vendored) for JSON Schema validation.
     - `app.css` — system fonts, light/dark via `prefers-color-scheme`, fully responsive, WCAG 2.2 AA contrast.
     - Loads either: (a) a local `reports/*.json` directory via the `<input type="file" webkitdirectory>` picker, (b) a remote JSON index served by GitHub Pages / S3 static hosting, or (c) the GitHub Actions artifact via a small fetch helper.
     - Publishable as a **GitHub Pages** site or attached as a CI artifact so any PR comment links to a live, shareable dashboard URL — no infra to deploy.

---

## 5. Canonical JSON schema (SCI-compliant)

File name: `energy-report.json`. Versioned via `$schema`.

```jsonc
{
  "$schema": "https://axa-oss.github.io/cienergy/schema/v1.json",
  "specVersion": "1.0.0",
  "sciSpecVersion": "ISO/IEC 21031:2024",
  "run": {
    "id": "gha-1234567890",
    "platform": "github-actions",
    "repository": "axa/myapp",
    "workflow": "ci.yml",
    "ref": "refs/pull/42/merge",
    "commitSha": "a1b2c3d…",
    "startedAt": "2026-06-04T08:12:33Z",
    "endedAt":   "2026-06-04T08:19:07Z",
    "durationSeconds": 394
  },
  "runner": {
    "os": "ubuntu-22.04",
    "arch": "x86_64",
    "vcpu": 4,
    "ramGiB": 16,
    "cpuModel": "Intel Xeon Platinum 8370C",
    "tdpWatts": 270,
    "provider": "github-hosted",
    "region": "eastus2",
    "isSpot": false
  },
  "energy": {
    "totalKWh": 0.0123,
    "byStep": [
      { "name": "checkout",   "kWh": 0.0001, "durationSeconds": 3,   "source": "eco-ci-model" },
      { "name": "build",      "kWh": 0.0078, "durationSeconds": 210, "source": "eco-ci-model" },
      { "name": "test",       "kWh": 0.0039, "durationSeconds": 165, "source": "eco-ci-model" },
      { "name": "docker-build","kWh": 0.0005,"durationSeconds": 16,  "source": "eco-ci-model" }
    ]
  },
  "carbon": {
    "operationalGCO2eq": 4.81,
    "embodiedGCO2eq": 0.92,
    "totalGCO2eq": 5.73,
    "gridIntensity": {
      "valueGCO2eqPerKWh": 391,
      "source": "electricitymaps",
      "zone": "US-VA",
      "timestamp": "2026-06-04T08:15:00Z"
    },
    "embodiedSource": "boavizta"
  },
  "sci": {
    "value": 5.73,
    "unit": "gCO2eq",
    "functionalUnit": "1 pipeline run",
    "R": 1
  },
  "cache": {
    "hit": true,
    "savedKWhEstimate": 0.041,
    "savedGCO2eqEstimate": 16.03
  },
  "metadata": {
    "team": "claims-platform",
    "costCenter": "AXA-FR-IT-042",
    "labels": { "language": "java", "framework": "spring-boot" }
  }
}
```

### Schema decisions

- All energy in **kWh**, all carbon in **gCO₂eq** (SCI convention).
- `measurement.source` is **always recorded** — required to defend numbers in audit.
- Optional `cache.saved*` fields enable **counter-factual** dashboards ("what would we have emitted without caching").
- Aligns with [OpenTelemetry semantic conventions for sustainability](https://github.com/open-telemetry/semantic-conventions/issues/1129) (draft, tracked for v1.1).

---

## 6. Implementation plan

### Phase 0 — Foundations (week 1)

- [ ] Create repo `cienergytool/` with this plan, ADRs, CONTRIBUTING.
- [ ] Choose stack: **Go 1.22+** (collector/aggregator/CLI), **Python 3.12** (optional ML hooks), **Grafana 11 + Postgres 16** (dashboard).
- [ ] Set up `pre-commit`, conventional commits, `release-please`, **SHA-pinned actions** (cf. [CVE-2025-30066](https://www.cve.org/CVERecord?id=CVE-2025-30066)).
- [ ] Define ADR-0001: estimation source priority order.
- [ ] Define ADR-0002: JSON schema versioning policy.

### Phase 1 — MVP collector (weeks 2–3)

- [ ] `cienergy probe rapl` — read `/sys/class/powercap/intel-rapl:*/energy_uj` deltas.
- [ ] `cienergy probe eco-ci` — port the eco-ci CPU model (TDP × util × time).
- [ ] `cienergy probe ccf` — Cloud Carbon Footprint vCPU coefficients lookup.
- [ ] Grid intensity resolver: Electricity Maps API client + 1 h LRU cache.
- [ ] Output v1 JSON (see §5), validated against JSON Schema.
- [ ] Unit tests with golden files for each probe.

### Phase 2 — GitHub Action wrapper (week 4)

- [ ] `cienergy-action` composite action.
- [ ] `pre`, `main`, `post` hooks.
- [ ] Upload `energy-report.json` as artifact.
- [ ] PR comment bot: SCI delta vs. base branch.
- [ ] Example workflow in `examples/github-actions/`.

### Phase 3 — Storage & dashboards (weeks 5–6)

- [x] Postgres schema (`runs`, `steps`, `metadata` tables, partitioned by month).
- [x] Ingestion endpoint (`POST /v1/runs`) — Go service, [OpenAPI documented](docs/api/openapi.yaml). Idempotent upsert; bearer-token auth; distroless podman image. Code in [`cmd/ingester/`](cmd/ingester/).
- [x] OTLP exporter alternative (push to existing Prometheus/Tempo) — OTLP/HTTP-JSON exporter in [`internal/exporter/otlp/`](internal/exporter/otlp/), wired into the aggregator via `--otlp-endpoint` / `CIENERGY_OTLP_ENDPOINT`. A ready-to-run OpenTelemetry Collector + Prometheus pair ships in `dashboard/grafana/docker-compose.yml` (ports `4317`/`4318` for OTLP, `8889` for the scrape target), and the provisioned `cienergy — OTLP live metrics` Grafana dashboard renders the seven gauges (`cienergy.energy.kwh`, `cienergy.carbon.{operational,embodied,total}.gco2eq`, `cienergy.grid.intensity.gco2eq_per_kwh`, `cienergy.sci.value`, `cienergy.run.duration.seconds`).
- [x] **Mode A — Grafana dashboards** (provisioned as code):
  - *Workflow energy trend* (gCO₂eq/run over time, with rolling avg).
  - *Repo leaderboard* (top 10 most-emitting repos).
  - *Cache effectiveness* (saved kWh, hit rate).
  - *Runner mix* (x86 vs ARM, gCO₂eq/build).
  - *SCI regression* (% delta vs. main).
- [x] **Mode B — Embedded HTML/JS/CSS dashboard** (`dashboard/embedded/`):
  - Single-page app, no build tool required (`python -m http.server` works).
  - Panels: **summary cards** (total kWh, total gCO₂eq, SCI, Δ vs. baseline), **time-series** (per workflow), **breakdown** (per step, stacked bar), **runner mix** (x86 vs ARM, doughnut), **cache savings** (counter-factual area chart), **leaderboard** (sortable table).
  - Inputs accepted: drag-and-drop folder, `<input type="file" multiple>`, `?src=<url-to-json-index>`, `?artifact=<gh-actions-artifact-url>`.
  - JSON Schema validation on load (Ajv) — invalid files highlighted with line/path.
  - Export: PNG snapshot of any panel, CSV of the underlying data, shareable permalink (state encoded in URL hash).
  - Accessibility: keyboard-navigable, ARIA labels, prefers-reduced-motion respected.
  - Shipped both as standalone files **and** packaged inside the GitHub Action so each run can upload a self-contained `dashboard.html` artifact next to `energy-report.json`.
- [x] Docker-Compose one-liner for Mode A (`podman compose up`); zero-install for Mode B (open `index.html`).

### Phase 4 — AI/ML extensions (weeks 7–8)

- [x] NVML probe for GPU jobs — standalone binary `cienergy-gpu-probe` (no CGO; shells out to `nvidia-smi`). Code in [`internal/probe/nvml/`](internal/probe/nvml/) + [`cmd/gpu-probe/`](cmd/gpu-probe/).
- [x] [CodeCarbon](https://codecarbon.io/) bridge for in-process Python ML steps — [`bridges/codecarbon/`](bridges/codecarbon/).
- [x] Boavizta embodied-carbon API integration (CPU SKU lookup, 24h LRU cache, CCF static fallback) — [`internal/embodied/`](internal/embodied/). Wired into the aggregator.
- [ ] Model-registry-as-cache pattern: if MLflow model promoted instead of rebuilt → record `savings`.

### Phase 5 — Hardening & adoption (weeks 9–10)

- [ ] Kepler integration mode (self-hosted k8s runners).
- [ ] GitLab CI + Jenkins collectors. *(Azure DevOps shipped in v1 — see `pipeline/azure-devops/`.)*
- [ ] CSRD/ESRS E1-ready export (CSV mapped to GHG Protocol Scope 2 + 3 cat. 1).
- [ ] Threshold gates: fail PR if `gCO₂eq` regresses by > X % (configurable).
- [ ] Documentation site (`mkdocs-material`) with methodology, FAQ, audit trail.
- [ ] Pilot on 3 AXA repos, publish public OSS release.

---

## 7. Repo layout (proposed)

```
cienergytool/
├── PLAN.md                      ← this file
├── README.md
├── LICENSE                      ← Apache-2.0
├── docs/
│   ├── methodology.md
│   ├── adr/
│   │   ├── 0001-source-priority.md
│   │   └── 0002-schema-versioning.md
│   └── schema/v1.json
├── cmd/
│   ├── aggregator/
│   ├── ingester/
│   ├── gpu-probe/             ← NVML wrapper (Phase 4)
│   └── cli/
├── bridges/
│   └── codecarbon/            ← Python in-process ML meter (Phase 4)
├── internal/
│   ├── probe/
│   │   ├── rapl/
│   │   ├── ecoci/
│   │   ├── ccf/
│   │   ├── nvml/              ← nvidia-smi polling (Phase 4)
│   │   └── kepler/
│   ├── grid/                    ← Electricity Maps / WattTime clients
│   ├── embodied/                ← Boavizta client (Phase 4)
│   └── sci/                     ← SCI formula + validators
├── action/                      ← GitHub Action (action.yml + dist/)
├── pipeline/
│   ├── azure-devops/            ← reusable step-list wrapper template
│   ├── gitlab-ci/               ← roadmap (Phase 5)
│   └── jenkins/                 ← roadmap (Phase 5)
├── dashboard/
│   ├── grafana/                 ← Mode A: server-backed
│   │   ├── docker-compose.yml
│   │   ├── dashboards/*.json
│   │   └── postgres/migrations/
│   └── embedded/                ← Mode B: zero-dependency static SPA
│       ├── index.html
│       ├── app.js               ← vanilla ES2022
│       ├── app.css
│       ├── vendor/
│       │   ├── chart.umd.min.js ← Chart.js, MIT, vendored
│       │   └── ajv.min.js       ← Ajv, MIT, vendored
│       ├── sample-reports/      ← used by `npm run preview`-less demo
│       └── README.md
├── examples/
│   ├── github-actions/
│   ├── gitlab-ci/
│   └── kubernetes-self-hosted/
└── test/
    ├── golden/
    └── e2e/
```

---

## 8. Risk register

| Risk | Mitigation |
|---|---|
| Hosted runners hide hardware → energy is a **model estimate**, not a measurement. | Always record `measurement.source`; show confidence interval in dashboard; cross-validate self-hosted runs with RAPL. |
| Grid-intensity APIs have rate limits / cost. | LRU cache + nightly fallback to monthly averages (Ember, IEA). |
| `gCO₂eq` per build is small → risk of "feature fatigue". | Show **aggregated** numbers (org / month / cost-centre); express also in **€** (cloud bill) and **CI-minutes** to make impact visible. |
| Methodology drift between sources (CCF vs eco-ci vs RAPL). | Pin coefficient versions; publish reproducible benchmark suite; document deltas. |
| Supply-chain attack on the action itself. | SHA-pin all transitive actions; sign releases with **Sigstore / cosign**; SLSA level 3 target. |

---

## 9. Acceptance criteria for v1.0

1. A `hello-world` GitHub Actions workflow produces a valid `energy-report.json` matching the published schema.
2. **Mode A** (Grafana stack), started via `podman compose up`, ingests that JSON and renders 5 reference panels.
3. **Mode B** (embedded HTML/JS/CSS dashboard) opens directly from the filesystem (`file://` or any static host), loads one or many `energy-report.json` files with no backend, and renders the same 5 panels. Page weight < 200 KB total (incl. vendored Chart.js).
4. Methodology page documents every coefficient and links to the primary source.
5. CI of the tool itself **dogfoods** the tool (its own SCI is reported in the README badge **and** the embedded dashboard is published to GitHub Pages on every release).
6. Independent reproduction: a third party can re-run a sample workflow and obtain the same JSON ± 5 %.

---

## 10. References (all validated as of June 2026)

### Standards & frameworks

- **ISO/IEC 21031:2024 — Software Carbon Intensity (SCI)** — https://sci.greensoftware.foundation/
- **GHG Protocol — Corporate Standard** — https://ghgprotocol.org/corporate-standard
- **EFRAG — ESRS E1 Climate Change (CSRD)** — https://www.efrag.org/lab6
- **ISO 14064-1:2018** — GHG quantification — https://www.iso.org/standard/66453.html
- **Green Software Foundation — Patterns Catalog** — https://patterns.greensoftware.foundation/
- **OpenTelemetry — Sustainability semantic conventions (draft)** — https://github.com/open-telemetry/semantic-conventions/issues/1129

### Sectoral data

- **IEA — *Electricity 2024*** (data-centre share of demand) — https://www.iea.org/reports/electricity-2024
- **IEA — *Data Centres and Data Transmission Networks*** — https://www.iea.org/energy-system/buildings/data-centres-and-data-transmission-networks
- **The Shift Project — *Lean ICT* (2019)** — https://theshiftproject.org/en/article/lean-ict-our-new-report/
- **ADEME × Arcep — Empreinte environnementale du numérique en France, volet 2 (mars 2023)** — https://www.arcep.fr/la-regulation/grands-dossiers-thematiques-transverses/lempreinte-environnementale-du-numerique.html
- **Ember — Global electricity grid carbon intensity dataset** — https://ember-climate.org/data/

### Research (ML / AI footprint)

- **Patterson et al. — *Carbon Emissions and Large Neural Network Training* (Google, 2021)** — https://arxiv.org/abs/2104.10350
- **Luccioni et al. — *Estimating the Carbon Footprint of BLOOM* (2022)** — https://arxiv.org/abs/2211.02001
- **Luccioni et al. — *Power Hungry Processing: ⚡ Watts ⚡ Driving the Cost of AI Deployment?* (2024)** — https://arxiv.org/abs/2311.16863
- **Strubell et al. — *Energy and Policy Considerations for Deep Learning in NLP* (2019)** — https://arxiv.org/abs/1906.02243
- **Schwartz et al. — *Green AI* (2020)** — https://arxiv.org/abs/1907.10597

### Energy measurement tooling

- **Cloud Carbon Footprint — methodology** — https://www.cloudcarbonfootprint.org/docs/methodology/
- **Green Coding Solutions — eco-ci** — https://github.com/green-coding-solutions/eco-ci-energy-estimation
- **Green Coding Solutions — Green Metrics Tool** — https://github.com/green-coding-solutions/green-metrics-tool
- **Kepler — sustainable-computing.io (CNCF Sandbox)** — https://sustainable-computing.io/
- **Scaphandre (Hubblo)** — https://github.com/hubblo-org/scaphandre
- **PowerAPI** — https://powerapi.org/
- **CodeCarbon** — https://codecarbon.io/
- **Carbontracker** — https://github.com/lfwa/carbontracker
- **ML CO2 Impact calculator** — https://mlco2.github.io/impact/
- **Intel RAPL — energy reporting** — https://www.intel.com/content/www/us/en/developer/articles/technical/software-security-guidance/advisory-guidance/running-average-power-limit-energy-reporting.html
- **NVIDIA NVML / DCGM** — https://developer.nvidia.com/management-library-nvml

### Carbon-intensity APIs

- **Electricity Maps API** — https://www.electricitymaps.com/
- **WattTime API** — https://www.watttime.org/
- **Boavizta API (embodied carbon)** — https://doc.api.boavizta.org/

### Cloud-native sustainability

- **CNCF Cloud Native Sustainability Whitepaper (2023)** — https://tag-env-sustainability.cncf.io/publications/cloud-native-sustainability-whitepaper/
- **CNCF TAG Environmental Sustainability** — https://tag-env-sustainability.cncf.io/

### Supply-chain & CI security (energy ↔ security link)

- **CVE-2025-30066 — tj-actions/changed-files supply-chain compromise** — https://www.cve.org/CVERecord?id=CVE-2025-30066
- **StepSecurity — incident analysis** — https://www.stepsecurity.io/blog/harden-runner-detection-tj-actions-changed-files-action-is-compromised
- **SLSA Framework** — https://slsa.dev/
- **Sigstore / cosign** — https://www.sigstore.dev/

### Vendor footprint methodologies (cross-validation)

- **AWS Customer Carbon Footprint Tool** — https://aws.amazon.com/aws-cost-management/aws-customer-carbon-footprint-tool/
- **Microsoft Emissions Impact Dashboard** — https://www.microsoft.com/en-us/sustainability/emissions-impact-dashboard
- **Google Cloud Carbon Footprint** — https://cloud.google.com/carbon-footprint

---

## 11. Per-repo realism (June 2026 fix)

> **Bug fixed.** Up to v1.0.x the multi-repo mode of `cienergy-aggregator`
> emitted the *same* `--steps-file` numbers on every report (`run.repository`
> was the only differentiator). That made the leaderboard dashboards
> misleading on multi-repo orgs.

**What changed**

- New package [`internal/cidetect/`](internal/cidetect/) scans a checkout for
  GitHub Actions, GitLab CI, Azure Pipelines, Jenkins and Tekton files. Each
  step is classified (checkout / setup / build / test / podman / lint /
  security-scan / deploy / artifact / comment / shell) and assigned conservative
  `(durationSeconds, cpuUtilPct)` heuristics; `source` is always set to
  `ci-detect-heuristic` so dashboards can flag modelled data.
- New CLI [`cmd/cidetect/`](cmd/cidetect/) emits per-pipeline JSONL.
- `cienergy-aggregator` now accepts repeatable `--repo-path slug=path`. For
  every mapped repo it emits **one report per detected pipeline** with a
  *distinct* energy/carbon footprint; repos without a mapping fall back to the
  shared `--steps-file`.
- `run.sh` exposes `REPO_PATHS='slug=path,slug2=path2'` to drive this
  end-to-end.

**Validation** (4 repos, FR grid 56 gCO₂eq/kWh):

| Repo                                 | Detected pipeline                | Energy (kWh) | Carbon (g) |
|--------------------------------------|----------------------------------|--------------|------------|
| axa/claims (Java + podman + Trivy)   | claims-ci · 6 steps              | 0.0232       | 1.70       |
| axa/policy (Node + tests)            | policy-ci · 4 steps              | 0.0176       | 1.27       |
| axa/shared-lib (lint only)           | shared-lib-ci · 2 steps          | 0.0007       | 0.06       |
| myorg/green-api-workshop-final       | 🌿 Green API Score CI · 44 steps | 0.0352       | 2.79       |

Spread = ×50 between the lightest and heaviest repo.

## 12. Improvement suggestions (June 2026)

> **Goal.** Turn each report into a *trigger* for the developer: actionable
> recommendations with upper-bound savings, not just numbers to look at.

**What changed**

- New `Suggestion` type in [`internal/model/`](internal/model/report.go) and
  new package [`internal/suggest/`](internal/suggest/) with ~10 deterministic
  rules (cache miss, podman layer cache, long tests, dirty grid zone, ARM
  runners, oversized runner, redundant builds, missing path filters, artifact
  bloat, missing cache savings instrumentation). Every suggestion carries
  `severity`, `title`, `detail`, `estimatedSaving{KWh,GCO2eq}`, `reference`.
- The aggregator attaches `suggest.For(report)` to every emitted report
  (multi-pipeline aware — each report gets its own recommendations).
- Postgres: new JSONB column `runs.suggestions` (+ flattening view
  `v_run_suggestions`). The ingester auto-migrates the column on startup so
  existing dev volumes keep working.
- Embedded dashboard: new *💡 Improvement suggestions* card with a severity
  filter, grouped per report, colour-coded pills, savings displayed per row,
  upstream doc link.
- Grafana: provisioned dashboard `cienergy — improvement suggestions` with
  stat row (open count / kWh & gCO₂eq saveable / repo count), top-suggestions
  bar gauge, severity donut, per-repo leaderboard, filterable details table,
  and two template variables (`severity`, `repository`).

**Validation** (same 4 repos as §11):

| Repo                                 | Suggestions | Top recommendation                              |
|--------------------------------------|-------------|-------------------------------------------------|
| axa/claims                           | 3           | share-build-artifacts (major)                   |
| axa/policy                           | 2           | share-build-artifacts (major)                   |
| axa/shared-lib                       | 1           | right-size-runner (minor)                       |
| myorg/green-api-workshop-final       | 5           | share-build-artifacts (major)                   |







