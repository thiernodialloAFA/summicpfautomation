<h1 align="center">summicpfautomation</h1>

<p align="center">
  <em>Companion mono-repo for the AXA Data Science &amp; Software Engineering Summit 2026 talks on<br>
  <strong>green CI/CD pipelines, AI AGENTS workflows</strong> and <strong>API eco-design</strong>.</em>
</p>

<p align="center">
  <a href="https://github.com/thiernodialloAFA/summicpfautomation/actions/workflows/ci.yml"><img src="https://github.com/thiernodialloAFA/summicpfautomation/actions/workflows/ci.yml/badge.svg?branch=main" alt="ci"></a>
  <a href="https://github.com/thiernodialloAFA/summicpfautomation/actions/workflows/dashboard-pages.yml"><img src="https://github.com/thiernodialloAFA/summicpfautomation/actions/workflows/dashboard-pages.yml/badge.svg" alt="dashboard-pages"></a>
  <a href="https://github.com/thiernodialloAFA/summicpfautomation/actions/workflows/docs.yml"><img src="https://github.com/thiernodialloAFA/summicpfautomation/actions/workflows/docs.yml/badge.svg" alt="docs"></a>
  <a href="cienergytool/LICENSE"><img src="https://img.shields.io/badge/License-Apache_2.0-blue.svg" alt="License: Apache-2.0"></a>
  <a href="https://sci.greensoftware.foundation/"><img src="https://img.shields.io/badge/SCI-ISO%2FIEC%2021031%3A2024-brightgreen" alt="SCI 21031:2024"></a>
</p>

---

## 🎯 What's in this repo?

Every pipeline you ship, every model you retrain, every JSON payload you serve consumes
electricity. This repo is the **demo + reference implementation** behind the two AXA Summit 2026
talks that quantify that hidden cost — and ship the tooling to keep it down.

| Folder / file | What it is | Status |
|---|---|---|
| [`cienergytool/`](./cienergytool/) | Production-grade Go CLI + embedded HTML dashboard that measures the **energy & carbon footprint of CI/CD pipelines** and emits **[SCI](https://sci.greensoftware.foundation/) (ISO/IEC 21031:2024)** reports. Includes a Grafana stack, a GitHub Action, an Azure DevOps template, an OTLP exporter, a CSRD/ESRS E1 CSV exporter and a CodeCarbon bridge for ML jobs. | ✅ v1 |
| [`talk-proposal-summit2026-ci-energy.md`](./talk-proposal-summit2026-ci-energy.md) | Talk #1 proposal — *"Build-Time Energy: The Invisible Kilowatts in Your CI — From Code Pipelines to AI Model Deployments"*. | 📝 |
| [`talk-proposal-summit2026.md`](./talk-proposal-summit2026.md) | Talk #2 proposal — *"There Is No AI Without API — Efficiency Goes Beyond Inference and Prompt Engineering"* (Green API scoring, 123 criteria). | 📝 |
| [`.github/workflows/`](./.github/workflows/) | CI for the cienergytool Go code, an MkDocs site deploy, and a **GitHub Pages deploy of the embedded dashboard** auto-loaded with fresh reports on every push. | ✅ |

---

## 🚀 Live demos

| What | URL | Source |
|---|---|---|
| **Embedded dashboard** (auto-loaded with the latest CI reports) | <https://thiernodialloafa.github.io/summicpfautomation/> | [`.github/workflows/dashboard-pages.yml`](./.github/workflows/dashboard-pages.yml) |
| **MkDocs site** (methodology, schema, ADRs, CSRD mapping) | published by [`docs.yml`](./.github/workflows/docs.yml) | [`cienergytool/docs/`](./cienergytool/docs/) |
| Sample SCI reports (JSON v1, drag-droppable into the dashboard) | [`cienergytool/dashboard/embedded/sample-reports/`](./cienergytool/dashboard/embedded/sample-reports/) | — |
| Sample CSRD/ESRS E1 export (CSV) | [`cienergytool/reports/cienergy-csrd-2026-Q2.csv`](./cienergytool/reports/cienergy-csrd-2026-Q2.csv) | — |

> 💡 The dashboard is **zero-dependency** (vanilla HTML/JS/CSS, no build step). Open the URL,
> or drag-drop your own `energy-report.json` files into it locally — no backend needed.

---

## ⚡ Quick start (60 seconds)

You need: **Go 1.22+**, `bash`, `python3`, `git` (and optionally `nvidia-smi` for GPU sampling).

```bash
git clone https://github.com/thiernodialloAFA/summicpfautomation.git
cd summicpfautomation/cienergytool
./run.sh        # builds the binaries, analyses 3 demo repos (incl. 2 cloned on the fly),
                # produces reports/cienergy-*.json + cienergy-csrd-<period>.csv,
                # opens the dashboard at http://127.0.0.1:8086 pre-loaded.
```

The script is **self-contained and idempotent** — it builds the 4 helper binaries the first time,
shallow-clones any remote repos you list in `REPOS=` / `REPO_REMOTES=`, scans their real CI
pipelines (`.github/workflows/*.yml`, `.gitlab-ci.yml`, `Jenkinsfile`, Azure pipelines, Tekton),
and **deletes the clones on exit** (`trap … EXIT`).

```bash
# scan your own repos (URLs are cloned & cleaned up automatically)
REPOS='myorg/api,https://github.com/acme/worker.git' REGION=DE ./run.sh

# CI-only mode (no browser, no local server, no ingester probe)
OPEN_DASHBOARD=0 SKIP_GPU=1 SKIP_OTLP=1 ./run.sh
```

See [`cienergytool/README.md`](./cienergytool/README.md) for the full configuration matrix
(GitHub Actions wrapper, Azure DevOps template, OTLP push, Grafana stack, CSRD export,
schema, ADRs, etc.).

---

## 🧭 Repo map

```
summicpfautomation/
├── .github/
│   └── workflows/
│       ├── ci.yml                # builds + tests the Go code, dogfoods the action
│       ├── docs.yml              # publishes the MkDocs site to GitHub Pages
│       ├── dashboard-pages.yml   # runs ./run.sh & publishes the dashboard to Pages
│       └── release.yml           # SLSA-3 signed release of the binaries
├── cienergytool/                 # ← the actual tool, see its README for details
│   ├── cmd/                      # 6 Go binaries (aggregator, ingester, cidetect, …)
│   ├── internal/                 # SCI maths, embodied (Boavizta), grid intensity (Ember/EM)
│   ├── dashboard/
│   │   ├── embedded/             # zero-dep HTML dashboard (published on Pages)
│   │   └── grafana/              # Postgres + OTLP collector + Grafana stack (compose)
│   ├── action/                   # GitHub Action (composite)
│   ├── pipeline/                 # Azure DevOps / GitLab / Jenkins integrations
│   ├── bridges/codecarbon/       # ML bridge (turns CodeCarbon JSON → cienergy steps)
│   ├── docs/                     # MkDocs source (methodology, schema, ADRs, CSRD mapping)
│   └── run.sh                    # one-shot local pipeline
├── talk-proposal-summit2026-ci-energy.md
└── talk-proposal-summit2026.md
```

---

## 🛠️ Workflows at a glance

| Workflow | Trigger | What it does |
|---|---|---|
| **ci** | every PR / push that touches `cienergytool/**` | `go test ./...`, build, sample validation, OpenAPI lint, **dogfoods the cienergy action on itself** |
| **dashboard-pages** | push to `main`, weekly cron, manual | runs `run.sh` in CI → fresh reports + CSRD CSV → publishes the **dashboard to GitHub Pages**, auto-loading them |
| **docs** | doc/MkDocs config changes | mirrors external READMEs into `docs/`, builds MkDocs, publishes the site |
| **release** | tag `v*.*.*` | reproducible cross-platform binaries, cosign-keyless signature, **SLSA Level 3 provenance** |

All four jobs honour `defaults.run.working-directory: cienergytool` so they remain
mono-repo-friendly while the tool lives in its sub-folder.

---

## 🗺️ Roadmap

Tracking the 5 phases of [`cienergytool/PLAN.md`](./cienergytool/PLAN.md) at a glance.
Legend: ✅ shipped · 🚧 in progress · 📋 planned · 💡 idea / RFC.

### v1 — Shipped (Q1–Q2 2026)

| Phase | Capability | Status |
|---|---|---|
| **0 — Foundations** | Go module layout, SCI JSON schema v1.0.0, sample reports, CI matrix | ✅ |
| **1 — MVP collector** | `cienergy-aggregator` Go binary, 3-tier source priority (eco-ci → RAPL → CPU+TDP estimate), Boavizta embodied carbon, Electricity Maps + Ember 2024 grid intensity | ✅ |
| **2 — GitHub Action** | composite `action/action.yml` wrapping any job, auto-uploaded artefact, dogfooded in this repo's own CI | ✅ |
| **3 — Storage & dashboards** | `cienergy-ingester` (REST + Postgres) · Grafana / Prometheus / OTLP stack (compose) · zero-dep embedded HTML dashboard with scenario simulator, per-runner-arch panels, suggestions panel | ✅ |
| **4 — AI/ML extensions** | `cienergy-gpu-probe` (NVML / `nvidia-smi`) · `bridges/codecarbon/` Python adapter (CodeCarbon JSON → cienergy steps) | ✅ |
| **5a — Azure DevOps** | reusable step template (`pipeline/azure-devops/cienergy-step-template.yml`) | ✅ |
| **5b — CSRD / ESRS E1** | `cienergy-csrd-export` CSV roll-up (Scope 2 operational + Scope 3.1 embodied, aggregable by repo/team/cost-center) | ✅ |
| **5c — Supply chain** | reproducible cross-platform release, cosign keyless signature, **SLSA Level 3** provenance | ✅ |
| **5d — GitHub Pages** | dashboard & MkDocs site auto-published on push; default `?src=` redirect for auto-load; weekly cron refresh | ✅ |

### v1.x — In progress (Q3 2026)

| Capability | Notes | Status |
|---|---|---|
| **GitLab CI** include template | mirrors the Azure DevOps wrapper pattern, surfaces metrics in MR widgets | 🚧 |
| **Jenkins** shared library | `cienergyStep` Groovy step for declarative + scripted pipelines | 🚧 |
| **Tekton** Task / Pipeline definitions | for OpenShift Pipelines / CD-Foundation users | 🚧 |
| **OCI image** of the aggregator | `ghcr.io/thiernodialloAFA/cienergy-aggregator:v1.x` for `image:`-style integrations | 🚧 |
| **Per-step gate** (`cienergy-gate`) | fail / warn the build above a configurable kWh or gCO₂eq budget | 🚧 |
| **Suggestion auto-PRs** | open a PR with the fix when a deterministic lever is detected (e.g. `arm64` runner, Dockerfile layer reordering) | 🚧 |

### v2 — Planned (Q4 2026 → 2027)

| Theme | Capability | Status |
|---|---|---|
| **Live grid** | switch to streaming Electricity Maps WebSocket for sub-hour resolution; cache locally | 📋 |
| **PUE catalogue** | per-cloud / per-region PUE table (AWS, Azure, GCP, OVH, Scaleway) feeding the Scope-2 calc | 📋 |
| **Multi-tenant ingester** | OIDC auth, per-org quotas, signed report receipts (transparency log) | 📋 |
| **APIGreenScore bridge** | join the cienergy SCI score with the 123-criteria [APIGreenScore](https://github.com/cnumr/APIGreenScore) report from talk #2 — one unified eco-score per release | 📋 |
| **AppCAT plug-in** | surface cienergy suggestions inside Microsoft's [App Modernization CAT](https://learn.microsoft.com/azure/migrate/appcat/) for Java / .NET upgrades | 💡 |
| **VS Code extension** | inline kWh / gCO₂eq lens above each `.github/workflows/*.yml` job, with one-click fix suggestions | 💡 |
| **`/eco-cost` GitHub bot** | comments on every PR with the energy / carbon delta vs. `main`, gated by the budget set in `cienergy.yaml` | 💡 |

> 📬 Have a use-case we should cover next? Open an
> [issue with the `roadmap` label](https://github.com/thiernodialloAFA/summicpfautomation/issues/new?labels=roadmap)
> — items with ≥ 3 community 👍 jump to the v1.x lane.

> 🛠️ **Maintainers**: re-sync the roadmap into GitHub (labels, milestones, issues) with
> [`./scripts/setup-github-roadmap.sh`](./scripts/setup-github-roadmap.sh). The script is
> idempotent (`--dry-run` supported); pass `CLOSE_SHIPPED=1` to also create and immediately
> close issues for every v1 line item.

---

## 📚 Talk material

| Talk | One-liner | Proposal |
|---|---|---|
| **#1 — Build-Time Energy** | Every CI run burns electricity nobody sees. Concrete measurements + 6 universal levers + AI-pipeline specifics (incremental ingestion, checkpoint-aware training, agent skill caching, model-registry-as-cache). | [`talk-proposal-summit2026-ci-energy.md`](./talk-proposal-summit2026-ci-energy.md) |
| **#2 — There Is No AI Without API** | AI efficiency does not stop at inference: an unpaginated, uncompressed JSON response burns kWh long before a token is generated. Built around the **123-criteria APIGreenScore** framework. | [`talk-proposal-summit2026.md`](./talk-proposal-summit2026.md) |

Both talks are co-presented by **Olivier** and **Thierno** at the AXA Data Science &
Software Engineering Summit 2026.

---

## 🔬 What the numbers really mean

The cienergytool emits **SCI ([ISO/IEC 21031:2024](https://sci.greensoftware.foundation/))-compliant**
JSON, separating:

- **Operational carbon** (Scope 2) — `kWh × grid intensity (gCO₂eq/kWh)` from
  [Electricity Maps](https://app.electricitymaps.com/) (live) or [Ember 2024](https://ember-climate.org/) (fallback).
- **Embodied carbon** (Scope 3.1) — amortised manufacturing footprint of the runner hardware
  using [Boavizta](https://github.com/Boavizta/boaviztapi) data.
- **SCI** — `(O + M) / R` where `R` = functional unit (per run, per commit, per artefact…).

A built-in **CSRD/ESRS E1 exporter** rolls the reports into a CSV ready for the
disclosure (Scope 2 operational + Scope 3.1 embodied), aggregable by repository, team
or cost-center.

See [`cienergytool/docs/methodology.md`](./cienergytool/docs/methodology.md) and the
two ADRs under [`cienergytool/docs/adr/`](./cienergytool/docs/adr/) for the source-priority
chain and schema-versioning policy.

---

## 🤝 Contributing

The cienergytool is **Apache-2.0** licensed and accepts external contributions.
Read [`cienergytool/README.md`](./cienergytool/README.md) for the development workflow,
then open a PR against `main`. The `ci` workflow will run the test suite **and** measure
its own energy/carbon footprint — your PR comes with its own SCI score 🌱.

For bug reports / RFCs, use the [GitHub issue tracker](https://github.com/thiernodialloAFA/summicpfautomation/issues).

---

## 📄 License

The tool and all source code under [`cienergytool/`](./cienergytool/) are released under the
**[Apache License 2.0](./cienergytool/LICENSE)**. The talk proposal Markdown files are
shared under **[CC BY 4.0](https://creativecommons.org/licenses/by/4.0/)** — re-use the
slides, cite the authors.

---

<p align="center">
  <sub>
    Built with ❤️ at AXA · powered by Go, vanilla JS &amp; <a href="https://sci.greensoftware.foundation/">Green Software Foundation SCI</a> ·
    measured with itself.
  </sub>
</p>

