# cienergytool

> Measure the **energy consumption** and **carbon footprint** of every CI/CD pipeline run.
> Emit **SCI-compliant JSON** ([ISO/IEC 21031:2024](https://sci.greensoftware.foundation/)).
> Visualize it in either a **server-backed Grafana stack** or a **zero-dependency embedded HTML/JS/CSS dashboard**.

[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Schema](https://img.shields.io/badge/schema-v1.0.0-informational)](docs/schema/v1.json)
[![SCI](https://img.shields.io/badge/SCI-ISO%2FIEC%2021031%3A2024-brightgreen)](https://sci.greensoftware.foundation/)

Companion project to the AXA Summit 2026 talk *"Build-Time Energy: The Invisible Kilowatts in Your CI"*.

---

## Quick start

### Supported CI platforms

| Platform | Integration | Status |
|---|---|---|
| **GitHub Actions**     | composite action — `action/action.yml` | ✅ v1 |
| **Azure DevOps**       | reusable steps template (wrapper pattern) — `pipeline/azure-devops/cienergy-step-template.yml` | ✅ v1 |
| **GitLab CI**          | include template — `pipeline/gitlab-ci/` *(roadmap, Phase 5)* | 🚧 |
| **Jenkins**            | shared library — `pipeline/jenkins/` *(roadmap, Phase 5)* | 🚧 |
| **Local / any CLI**    | run `cienergy-aggregator` directly | ✅ v1 |

The aggregator **auto-detects** the platform from env vars (`TF_BUILD`, `GITHUB_ACTIONS`, `GITLAB_CI`, `JENKINS_URL`) — the same binary works everywhere.

### 1. GitHub Actions

```yaml
# .github/workflows/ci.yml
jobs:
  build:
    runs-on: ubuntu-22.04
    steps:
      - uses: actions/checkout@v4
      - uses: axa-oss/cienergy-action@v1   # wraps the rest of the job
        with:
          electricity-maps-token: ${{ secrets.EMAPS_TOKEN }}   # optional
      - run: ./build.sh
      - run: ./test.sh
```

### 2. Azure DevOps

```yaml
# .azure-pipelines/ci.yml
resources:
  repositories:
    - repository: cienergy
      type: github
      endpoint: 'github.com'
      name: axa-oss/cienergytool
      ref: refs/tags/v1.0.0

jobs:
  - job: build
    pool: { vmImage: 'ubuntu-22.04' }
    steps:
      - template: pipeline/azure-devops/cienergy-step-template.yml@cienergy
        parameters:
          region: 'WE'
          team: 'claims-platform'
          electricityMapsTokenVar: 'EMAPS_TOKEN'
          steps:
            - checkout: self
            - script: ./build.sh
            - script: ./test.sh
```

Full ADO docs: [`pipeline/azure-devops/README.md`](pipeline/azure-devops/README.md).

Both integrations install the collector, instrument the wrapped steps, and upload:

- `energy-report.json` — SCI-compliant report (see [schema](docs/schema/v1.json))
- `dashboard.html` — the embedded dashboard pre-loaded with this run

### 3. Open the dashboard

Two ways:

| Mode | Command | Backend? | Use case |
|---|---|---|---|
| **A — Grafana** | `cd dashboard/grafana && docker compose up` | Postgres + Grafana | org-wide rollout, long-term trends |
| **B — Embedded** | open `dashboard/embedded/index.html` in a browser | none | per-PR review, demo, air-gapped |

Mode B reads `energy-report.json` files directly (drag-and-drop, file picker, or `?src=` URL), runs in any browser, and weighs < 200 KB.

### 4. Run locally without GitHub Actions

```sh
make build

# Generate a report from the bundled sample (5 steps, ~7 min synthetic build).
./bin/cienergy-aggregator \
  --start "$(date -u +%FT%TZ)" \
  --cpu-model "Intel Xeon Platinum 8370C" \
  --vcpu 4 \
  --region US-VA \
  --workflow ci.yml \
  --repo myorg/myapp \
  --commit "$(git rev-parse HEAD 2>/dev/null || echo 0000000)" \
  --steps-file ./examples/samples/steps.jsonl \
  --out energy-report.json
```

> **Notes**
> - `--region` is an [Electricity Maps zone code](https://www.electricitymaps.com/) (e.g. `US-VA`, `FR`, `WE`), **not** a cloud-region code. Unknown values fall back to the bundled `WORLD` average.
> - `--commit` is optional; without it, the aggregator records `0000000`.
> - The shortcut `make sample` runs the same command and writes the report next to the embedded dashboard so you can open it immediately.

## What we measure

```
SCI = ( (E × I) + M ) / R           [ISO/IEC 21031:2024]
```

| Term | What | How |
|---|---|---|
| **E** | Energy used by the build (kWh) | RAPL → Kepler → NVML → eco-ci model → CCF model (best source available, recorded in `measurement.source`) |
| **I** | Grid carbon intensity (gCO₂eq/kWh) | [Electricity Maps](https://www.electricitymaps.com/) live, with monthly [Ember](https://ember-climate.org/data/) fallback |
| **M** | Embodied carbon (gCO₂eq) | [Boavizta API](https://doc.api.boavizta.org/) amortised over hardware lifetime |
| **R** | Functional unit | 1 pipeline run (default), configurable |

Full methodology: [docs/methodology.md](docs/methodology.md).

## Repository layout

See [PLAN.md §7](PLAN.md#7-repo-layout-proposed).

## Binaries (`bin/`)

After `make build`, the following self-contained Go binaries are produced in `bin/`. They are designed to be composed in any CI pipeline (no daemon, no shared state) — each one reads files / env vars and writes files / stdout.

| Binary | Role | Typical caller |
|---|---|---|
| [`cienergy-aggregator`](#cienergy-aggregator)   | Build an SCI-compliant `energy-report.json` from per-step samples | every job, end of build |
| [`cienergy-gate`](#cienergy-gate)               | Compare current report vs baseline, fail PR on regression | PR check / post-build step |
| [`cienergy-csrd-export`](#cienergy-csrd-export) | Aggregate many reports into a CSRD / ESRS E1 CSV | nightly / quarterly reporting job |
| [`cienergy-gpu-probe`](#cienergy-gpu-probe)     | Background sampler for `nvidia-smi` (GPU kWh) | wrapped around ML training steps |
| [`cienergy-ingester`](#cienergy-ingester)       | Long-running HTTP service that stores reports in Postgres | central deployment, behind the Grafana stack |

All binaries support `-h` / `--help` for the full flag list. Env-var defaults are auto-detected from GitHub Actions, Azure DevOps, GitLab CI and Jenkins.

---

### `cienergy-aggregator`

Reads a JSONL file of per-step samples and emits one `energy-report.json`
(SCI v1 schema). Resolves grid intensity (Electricity Maps → Ember fallback)
and embodied carbon (Boavizta → CCF static fallback) automatically.

```sh
./bin/cienergy-aggregator \
  --start    "$(date -u +%FT%TZ)" \
  --cpu-model "Intel Xeon Platinum 8370C" \
  --vcpu 4 --ram 16 --tdp 270 \
  --region   US-VA \
  --repo     myorg/myapp \
  --workflow ci.yml \
  --commit   "$(git rev-parse HEAD)" \
  --steps-file ./examples/samples/steps.jsonl \
  --out energy-report.json
```

Key flags:

| Flag | Default | Notes |
|---|---|---|
| `--steps-file` | *(required)* | JSONL, one `{name,durationSeconds,cpuUtilPct,kWh?,gpuKWh?,source?}` per line |
| `--out`         | `energy-report.json` | use `-` for stdout |
| `--region`      | `WORLD` | Electricity Maps zone (`FR`, `US-VA`, `WE`…) |
| `--rapl-kwh`    | `-1` | bypass model, inject a measured kWh value |
| `--embodied-gco2eq` | `-1` | override Boavizta result |
| `--otlp-endpoint`   | env `CIENERGY_OTLP_ENDPOINT` | POST metrics to `<url>/v1/metrics` |
| `--cache-hit`   | `false` | flag report as a fully-cached build |
| `--team`, `--cost-center` | env | added to `metadata` for CSRD rollups |

Env shortcuts: `CIENERGY_EMAPS_TOKEN`, `CIENERGY_TEAM`, `CIENERGY_COST_CENTER`,
`CIENERGY_OTLP_ENDPOINT`, `CIENERGY_OTLP_HEADER`.

---

### `cienergy-gate`

PR-blocking quality gate. Compares a current report against a baseline
(usually the report from `main`) and exits non-zero on regression.

```sh
./bin/cienergy-gate \
  --current  ./energy-report.json \
  --baseline ./base/energy-report.json \
  --metric   sci \
  --warn-increase-pct 10 \
  --max-increase-pct  25 \
  --format   gh-summary >> "$GITHUB_STEP_SUMMARY"
```

| Flag | Default | Notes |
|---|---|---|
| `--current`, `--baseline` | *(required)* | paths to two `energy-report.json` files |
| `--metric` | `sci` | one of `sci`, `kwh`, `co2` |
| `--warn-increase-pct` | `10` | log a warning past this delta |
| `--max-increase-pct`  | `25` | **fail** past this delta |
| `--format` | `text` | `text` · `json` · `gh-summary` (GitHub-Markdown) |

Exit codes: `0` ok · `1` warn (build still passes) · `2` fail · `64` bad input.

---

### `cienergy-csrd-export`

Aggregates a directory of reports into a CSV ready for **CSRD / ESRS E1**
disclosure (EU Directive 2022/2464). Maps values to GHG Protocol scopes:
operational = Scope 2 (location-based), embodied = Scope 3 cat. 1.

```sh
./bin/cienergy-csrd-export \
  --in     ./reports/ \
  --period 2025-Q4 \
  --by     team \
  --entity "AXA SA" \
  --method location-based \
  --out    csrd-2025Q4.csv
```

| Flag | Default | Notes |
|---|---|---|
| `--in`     | *(required)* | directory of `.json` reports, or a single file |
| `--out`    | `csrd.csv`   | use `-` for stdout |
| `--by`     | `run`        | `run` · `day` · `month` · `repository` · `team` · `cost-center` |
| `--period` | empty        | free-text label copied into every row (e.g. `2025-Q4`) |
| `--entity` | `AXA SA`     | reporting entity name |
| `--method` | `location-based` | GHG Scope 2 method: `location-based` or `market-based` |

See [docs/csrd-mapping.md](docs/csrd-mapping.md) for the full column dictionary.

---

### `cienergy-gpu-probe`

Polls `nvidia-smi` in the background while a GPU-heavy step runs (training,
inference, transcoding). On `SIGTERM` / `SIGINT` it appends one JSONL line
to your steps file describing the GPU kWh consumed since startup. If
`nvidia-smi` isn't found, it exits 0 and writes nothing — the aggregator
will then fall back to the eco-ci CPU model with no extra config.

```sh
./bin/cienergy-gpu-probe \
  --name        train \
  --steps-file  "$WORK/steps.jsonl" \
  --interval-ms 2000 \
  --cpu-util    40 &
PROBE_PID=$!

./train.py             # your actual workload

kill -TERM $PROBE_PID
wait
```

| Flag | Default | Notes |
|---|---|---|
| `--steps-file`  | *(required)* | JSONL file the aggregator will later read |
| `--name`        | `gpu-job` | step name written into the JSONL line |
| `--interval-ms` | `2000` | sampling interval |
| `--cpu-util`    | `40` | estimated CPU % for the wrapping step |

---

### `cienergy-ingester`

Small HTTP service that accepts reports and stores them in Postgres for
the Grafana dashboard. Stateless apart from the DB; safe to run multiple
replicas behind a load balancer. Container image is built from
[`cmd/ingester/Dockerfile`](cmd/ingester/Dockerfile).

```sh
export POSTGRES_URL="postgres://cienergy:cienergy@localhost:5432/cienergy?sslmode=disable"
export INGESTER_TOKEN="$(openssl rand -hex 24)"   # optional
export PORT=8080

./bin/cienergy-ingester
```

Push a report from any CI job:

```sh
curl -fsS -X POST "http://ingester.example.com/v1/runs" \
  -H "Authorization: Bearer $INGESTER_TOKEN" \
  -H "Content-Type: application/json" \
  --data-binary @energy-report.json
```

Endpoints:

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/runs`       | ingest one SCI report (max 1 MiB by default) |
| `GET`  | `/v1/runs`       | list recent runs — `?limit=N` (default 50) |
| `GET`  | `/v1/runs/{id}`  | fetch a single run as JSON |
| `GET`  | `/healthz`       | liveness |
| `GET`  | `/readyz`        | readiness (DB ping) |

Env vars: `PORT`, `POSTGRES_URL`, `INGESTER_TOKEN` (optional Bearer-auth),
`MAX_BODY_BYTES` (default `1048576`).

## License

Apache-2.0. Vendored libraries keep their original MIT/Apache-2 licenses.

## References

Standards (SCI, GHG Protocol, ESRS E1), sectoral data (IEA, Shift Project, ADEME-Arcep), research (Patterson, Luccioni, Strubell), tooling (Kepler, Scaphandre, eco-ci, CCF, CodeCarbon, Boavizta) — full list in [PLAN.md §10](PLAN.md#10-references-all-validated-as-of-june-2026).

