# cienergytool

> Measure the **energy consumption** and **carbon footprint** of every CI/CD pipeline run.
> Emit **SCI-compliant JSON** ([ISO/IEC 21031:2024](https://sci.greensoftware.foundation/)).
> Visualize it in either a **server-backed Grafana stack** or a **zero-dependency embedded HTML/JS/CSS dashboard**.
> Works for **single repos *and* monorepo / multi-repo builds** — one SCI report per repository, in one invocation.

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

### Repository scope

| Layout | How |
|---|---|
| **Single repository**          | default behaviour — `--repo myorg/myapp` (or auto-detected from `GITHUB_REPOSITORY` / `BUILD_REPOSITORY_NAME` / `CI_PROJECT_PATH` / `GIT_URL`) → one `energy-report.json`. |
| **Monorepo / multi-repo run**  | pass a comma-separated list: `--repo "myorg/app1,myorg/app2,myorg/lib"`. The aggregator emits **one report per repository** with the same energy/runner figures and a distinct `run.repository`, plus a `metadata.labels.repositories` field listing the full peer set for traceability. |

Use `{repo}` in `--out` to template the destination (e.g. `--out ./reports/cienergy-{repo}.json`); if `--out` has no placeholder, the slug is appended automatically before the extension (`energy-report.json` → `energy-report-myorg_app1.json`). The OTLP exporter pushes one set of metrics per repository so the `repository` dimension is preserved downstream.

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
| **A — Grafana** | `cd dashboard/grafana && podman compose up` | Postgres + Grafana | org-wide rollout, long-term trends |
| **B — Embedded** | open `dashboard/embedded/index.html` in a browser | none | per-PR review, demo, air-gapped |

Mode B reads `energy-report.json` files directly (drag-and-drop, file picker, or `?src=` URL), runs in any browser, and weighs < 200 KB.

### 4. Run locally without GitHub Actions

#### Quickest path — `./run.sh`

A one-shot entry point that **builds + aggregates 4 demo repos + ingests + opens the dashboard**:

```sh
./run.sh                        # uses defaults
INGESTER_TOKEN=xxx ./run.sh     # if the ingester requires bearer auth
REPOS="axa/claims,axa/policy" REGION=FR ./run.sh
```

What it does, in order:

1. Builds `./bin/cienergy-aggregator` if missing (`go build`).
2. Runs the aggregator in multi-repo mode → writes `./reports/cienergy-<repo>.json` per repo.
3. Probes `${INGESTER_URL}/readyz` (default `http://localhost:8085`). If it answers `200`, POSTs every report to `/v1/runs` then **GETs `/v1/runs?limit=200` to verify the rows are actually persisted in Postgres** (prints id / repository / startedAt / sci for each).
4. Stages the reports under `dashboard/embedded/local-reports/` with an auto-generated `index.json`, starts a tiny local HTTP server (`python3 -m http.server`) on `${DASHBOARD_PORT}` (default `8086`), and opens `http://127.0.0.1:8086/index.html?src=./local-reports/index.json` in your default browser — the dashboard **auto-loads the freshly generated reports**, no drag-drop required (set `OPEN_DASHBOARD=0` to skip). To stop the static server later: `kill $(cat /tmp/cienergy-dashboard-8086.pid)`.

All knobs are env-vars: `REGION`, `REPOS`, `STEPS_FILE`, `OUT_TEMPLATE` (supports `{repo}`), `INGESTER_URL`, `INGESTER_TOKEN`, `DASHBOARD_PORT`, `OPEN_DASHBOARD`.

#### Or call the binary directly

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

# Monorepo / multi-repo run — one SCI report per repository
# (same energy & runner figures, distinct run.repository).
./bin/cienergy-aggregator \
  --start "$(date -u +%FT%TZ)" \
  --region FR \
  --repo "axa/claims,axa/policy,axa/shared-lib" \
  --steps-file ./examples/samples/steps.jsonl \
  --out ./reports/cienergy-{repo}.json
# → ./reports/cienergy-axa_claims.json
#   ./reports/cienergy-axa_policy.json
#   ./reports/cienergy-axa_shared-lib.json
```

> **Notes**
> - `--region` is an [Electricity Maps zone code](https://www.electricitymaps.com/) (e.g. `US-VA`, `FR`, `WE`), **not** a cloud-region code. Unknown values fall back to the bundled `WORLD` average.
> - `--commit` is optional; without it, the aggregator records `0000000`.
> - `--repo` accepts **one or more comma-separated slugs**. With more than one, the aggregator writes one report per repository; use `{repo}` in `--out` to template the path, or let the slug be appended automatically before the extension. The full peer list is preserved in `metadata.labels.repositories` for traceability. See [Binaries → `cienergy-aggregator`](#cienergy-aggregator) for the full flag reference.
> - **Embodied carbon accuracy.** Even when you only pass `--start` (no `--end`), the amortisation share is computed from `max(endT − startT, Σ step durations)`, so the Scope-3.1 figure reflects the real work done in the steps file instead of the aggregator's own wall-clock.
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
| `--out`         | `energy-report.json` | use `-` for stdout · supports `{repo}` placeholder for multi-repo runs |
| `--repo`        | auto-detected | one or more comma-separated slugs (`org/app1,org/app2`) → one report per repo |
| `--region`      | `WORLD` | Electricity Maps zone (`FR`, `US-VA`, `WE`…) |
| `--rapl-kwh`    | `-1` | bypass model, inject a measured kWh value |
| `--embodied-gco2eq` | `-1` | override Boavizta result |
| `--otlp-endpoint`   | env `CIENERGY_OTLP_ENDPOINT` | POST metrics to `<url>/v1/metrics` |
| `--cache-hit`   | `false` | flag report as a fully-cached build |
| `--team`, `--cost-center` | env | added to `metadata` for CSRD rollups |

Env shortcuts: `CIENERGY_EMAPS_TOKEN`, `CIENERGY_TEAM`, `CIENERGY_COST_CENTER`,
`CIENERGY_OTLP_ENDPOINT`, `CIENERGY_OTLP_HEADER`.

> **Multi-repo / monorepo runs.** Pass several slugs to `--repo` to emit one
> SCI report per repository (same energy / runner figures, distinct
> `run.repository`). The full list is preserved in `metadata.labels.repositories`
> for traceability.
>
> ```sh
> ./bin/cienergy-aggregator \
>   --repo "axa/claims,axa/policy,axa/shared-lib" \
>   --out  ./reports/cienergy-{repo}.json \
>   --steps-file ./steps.jsonl
> ```
>
> If `--out` does not contain `{repo}`, the slug is appended before the
> extension automatically (e.g. `energy-report.json` → `energy-report-axa_claims.json`).

> **Embodied carbon accuracy.** The amortisation share is computed from the
> effective run duration: `max(endT − startT, Σ step durations)`. This means
> you can pass only `--start` (without `--end`) and still get a representative
> Scope-3.1 figure based on the work actually done in the steps file.

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
export PORT=8085

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

### `cienergy-cidetect`

Scans a repository for CI/build YAML files and turns each detected pipeline
into a measurable JSONL steps file. Used by `cienergy-aggregator` to produce
**distinct** energy/carbon numbers per repo (instead of the legacy "monorepo"
mode that copied the same numbers onto every repo).

Detected platforms:

| Platform        | Files inspected                                         |
|-----------------|---------------------------------------------------------|
| GitHub Actions  | `.github/workflows/*.yml`, `*.yaml` (one report per file) |
| GitLab CI       | `.gitlab-ci.yml`                                        |
| Azure Pipelines | `azure-pipelines.yml`, `azure-pipelines.yaml`, `.azure-pipelines.yml` |
| Jenkins         | `Jenkinsfile` (heuristic — opaque, modelled as 3 steps) |
| Tekton          | `tekton/*.yaml`, `.tekton/*.yaml`                       |

Steps are classified by intent (`checkout`, `setup`, `build`, `test`,
`docker`, `lint`, `security-scan`, `deploy`, `artifact`, `comment`, `shell`)
with conservative `(durationSeconds, cpuUtilPct)` heuristics. Every emitted
step records `source: "ci-detect-heuristic"` so dashboards can flag modelled
data.

```sh
# Inspect what would be detected:
./bin/cienergy-cidetect --repo ./path/to/repo

# Dump JSONL steps for a single pipeline (the first one):
./bin/cienergy-cidetect --repo ./path/to/repo --jsonl

# Wire into the aggregator: one *distinct* report per (repo, pipeline)
./bin/cienergy-aggregator \
  --repo "axa/claims,axa/policy,myorg/api" \
  --repo-path "axa/claims=./checkouts/claims" \
  --repo-path "axa/policy=./checkouts/policy" \
  --repo-path "myorg/api=./checkouts/api" \
  --steps-file ./examples/samples/steps.jsonl \
  --out "./reports/cienergy-{repo}-{workflow}.json"
```

When `--repo-path slug=path` is provided, the global `--steps-file` becomes a
fallback used only for repos *without* a path. Repos with multiple workflow
files generate one report per file, all carrying
`metadata.labels.pipeline_path` so the dashboard can roll up per pipeline.

Or via `run.sh`:

```sh
REPO_PATHS='axa/claims=./checkouts/claims,axa/policy=./checkouts/policy' ./run.sh
```

## Improvement suggestions

Every `energy-report.json` now ships with a `suggestions[]` array. The
aggregator runs ~10 deterministic heuristics over the report's own numbers
(dependency-cache miss, podman layer cache, long tests, dirty grid zone,
ARM runners, oversized runner, redundant builds, missing path filters,
artifact bloat, missing cache savings instrumentation). Each suggestion
carries:

| Field | Meaning |
|---|---|
| `id`                       | Stable identifier (e.g. `enable-dependency-cache`)        |
| `severity`                 | `critical` · `major` · `minor` · `info`                   |
| `title` / `detail`         | Human-readable summary + actionable detail                |
| `estimatedSavingKWh`       | Upper-bound kWh saved if the suggestion is applied        |
| `estimatedSavingGCO2eq`    | Same, expressed in gCO₂eq using the run's grid intensity  |
| `reference`                | Link to the upstream documentation                        |

**Local dashboard (Mode B)** — the embedded SPA renders a *💡 Improvement
suggestions* card under the runs table, grouped per report, with a severity
selector at the top. Each row shows a colour-coded severity pill, the
detail text, the upstream doc link, and the upper-bound savings.

**Grafana (Mode A)** — provisioned dashboard
[`cienergy — improvement suggestions`](dashboard/grafana/dashboards/cienergy-suggestions.json)
(uid `cienergy-suggestions`) renders, against the auto-migrated
`v_run_suggestions` view: total open suggestions / potential save / repo
count (stat row), top suggestions by gCO₂eq saved (bar gauge), severity
donut, per-repo leaderboard, and a filterable details table. Two template
variables — `severity` (min level) and `repository` (multi-select).

The aggregator never fails on suggestions and they are stored as JSONB in
Postgres (`runs.suggestions` column, auto-added on startup by the
ingester), so older runs without the field keep working.

## OpenTelemetry Collector (OTLP)

In addition to the Postgres-backed ingester, `cienergy-aggregator` can push its
report as **OpenTelemetry metrics** (OTLP/HTTP-JSON) to any collector — OTel
Collector, Grafana Alloy, Datadog, Honeycomb, New Relic, etc. This is the
recommended path when you already run an observability stack and want CI
energy to live next to your existing app metrics.

### What gets emitted

Seven `Gauge` metrics, one resource per run, attributes aligned with the
[OpenTelemetry sustainability semantic conventions (draft)](https://github.com/open-telemetry/semantic-conventions/issues/1129):

| Metric                                       | Unit          | Meaning                                          |
|----------------------------------------------|---------------|--------------------------------------------------|
| `cienergy.energy.kwh`                        | `kWh`         | Total energy of the run                          |
| `cienergy.carbon.operational.gco2eq`         | `gCO2eq`      | Scope 2 (location-based)                         |
| `cienergy.carbon.embodied.gco2eq`            | `gCO2eq`      | Scope 3 cat. 1 (amortised)                       |
| `cienergy.carbon.total.gco2eq`               | `gCO2eq`      | Operational + embodied                           |
| `cienergy.grid.intensity.gco2eq_per_kwh`     | `gCO2eq/kWh`  | Grid intensity at runner location at run time    |
| `cienergy.sci.value`                         | `gCO2eq`      | SCI per functional unit (ISO/IEC 21031:2024)     |
| `cienergy.run.duration.seconds`              | `s`           | Wall-clock duration                              |

Resource attributes: `service.name=cienergy`, `service.namespace=<repo>`,
`ci.platform`, `ci.workflow`, `ci.run.id`, `ci.commit.sha`, `host.arch`,
`host.cpu.model.name`, `cloud.region`, `sustainability.grid.{zone,source}`,
`sustainability.sci.functional_unit`, plus `team` / `cost_center` when set.

### Push from any CI job

```sh
./bin/cienergy-aggregator \
  --steps-file steps.jsonl \
  --region FR \
  --otlp-endpoint http://otel-collector.observability:4318 \
  --otlp-header  "Authorization: Bearer $OTEL_TOKEN"
```

Or via env: `CIENERGY_OTLP_ENDPOINT`, `CIENERGY_OTLP_HEADER`. Empty endpoint =
no-op (safe default).

### Local end-to-end demo

The Mode-A stack already bundles an OTel Collector (`otel/opentelemetry-collector-contrib:0.110.0`)
and a Prometheus, both pre-wired to the provisioned **`cienergy — OTLP live
metrics`** Grafana dashboard:

```sh
make stack-up        # Postgres + ingester + Grafana + otel-collector + Prometheus
make otlp-demo       # build, run the sample, push to http://localhost:4318
open http://localhost:3000/d/cienergy-otlp
```

Collector endpoints exposed: `4318` (OTLP/HTTP), `4317` (OTLP/gRPC),
`8889` (`/metrics` for Prometheus scrape).

## License

Apache-2.0. Vendored libraries keep their original MIT/Apache-2 licenses.

## References

Standards (SCI, GHG Protocol, ESRS E1), sectoral data (IEA, Shift Project, ADEME-Arcep), research (Patterson, Luccioni, Strubell), tooling (Kepler, Scaphandre, eco-ci, CCF, CodeCarbon, Boavizta) — full list in [PLAN.md §10](PLAN.md#10-references-all-validated-as-of-june-2026).

