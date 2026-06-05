# Mode A — Grafana stack

A three-container podman Compose stack (Postgres 16 + cienergy-ingester + Grafana 11)
with a provisioned *cienergy overview* dashboard, ready to receive
`energy-report.json` files via HTTP.

## Start

```sh
cd dashboard/grafana
podman compose up -d --build
# Postgres   :5432
# Ingester   :8085   (POST /v1/runs)
# Grafana    :3000   (anonymous Viewer; admin/admin to edit)
```

## Ingest reports

The `cienergy-ingester` HTTP service is now part of the stack — no more `psql`
required. See [`cmd/ingester/README.md`](../../cmd/ingester/README.md) for the
full API.

```sh
# One-shot: POST one report
curl -fsS -X POST http://localhost:8085/v1/runs \
  -H 'Content-Type: application/json' \
  --data @../embedded/sample-reports/run-001-baseline.json

# Seed all 5 bundled samples (from repo root)
make seed-samples
```

To enable bearer-token auth, set `INGESTER_TOKEN` in your shell before
`podman compose up`:

```sh
INGESTER_TOKEN='s3cr3t' podman compose up -d --build
```

## From CI

Both the GitHub Action and the Azure DevOps template accept an `ingester-url`
input (and an optional bearer token) — see
[`cmd/ingester/README.md`](../../cmd/ingester/README.md#push-a-report-from-ci).

## Default dashboard

`cienergy/overview` — full parity with the embedded HTML dashboard:

- **KPI row** — total energy, total carbon, mean SCI, cache savings (auto-scaled to Wh / kWh / MWh / GWh / TWh and g / kg / t / kt / Mt).
- **SCI trend** + **Repository leaderboard**.
- **What-if scenario** row driven by 3 dashboard variables at the top:
  - `Hosting zone` — pick `Measured` (keep original Electricity-Maps intensity per run) or any of 21 preset zones (FR · 56, DE · 380, US-VA · 280, IN · 700, …, World · 475 gCO₂eq/kWh). Annual averages from Ember 2024.
  - `Runs per day (X)` — textbox, default `1`.
  - `Days per year (Y, max 365)` — textbox, default `365`.
  - 4 stat panels show: projected runs/yr, projected energy/yr, projected carbon/yr (auto-scaled), and the petrol-car-km equivalent (170 g/km).
- **Energy breakdown by step** — stacked bar of `run_steps.kwh × X × Y`.
- **Carbon footprint** — daily stacked bar of operational vs embodied, with operational re-projected against the selected hosting zone when not `Measured`.
- **Runner-arch mix** pie + **Recent runs** table with per-column units.



