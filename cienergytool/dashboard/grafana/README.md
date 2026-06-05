# Mode A — Grafana stack

A three-container Docker Compose stack (Postgres 16 + cienergy-ingester + Grafana 11)
with a provisioned *cienergy overview* dashboard, ready to receive
`energy-report.json` files via HTTP.

## Start

```sh
cd dashboard/grafana
docker compose up -d --build
# Postgres   :5432
# Ingester   :8080   (POST /v1/runs)
# Grafana    :3000   (anonymous Viewer; admin/admin to edit)
```

## Ingest reports

The `cienergy-ingester` HTTP service is now part of the stack — no more `psql`
required. See [`cmd/ingester/README.md`](../../cmd/ingester/README.md) for the
full API.

```sh
# One-shot: POST one report
curl -fsS -X POST http://localhost:8080/v1/runs \
  -H 'Content-Type: application/json' \
  --data @../embedded/sample-reports/run-001-baseline.json

# Seed all 5 bundled samples (from repo root)
make seed-samples
```

To enable bearer-token auth, set `INGESTER_TOKEN` in your shell before
`docker compose up`:

```sh
INGESTER_TOKEN='s3cr3t' docker compose up -d --build
```

## From CI

Both the GitHub Action and the Azure DevOps template accept an `ingester-url`
input (and an optional bearer token) — see
[`cmd/ingester/README.md`](../../cmd/ingester/README.md#push-a-report-from-ci).

## Default dashboard

`cienergy/overview` — KPI cards (kWh, gCO₂eq, mean SCI, cache savings), SCI trend
time series, repo leaderboard, runner-arch mix pie, recent-runs table.



