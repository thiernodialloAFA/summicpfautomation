# cienergy-ingester

Small HTTP service that accepts SCI-compliant energy reports and stores them in
Postgres for the Grafana dashboard (Mode A).

## Endpoints

| Method | Path           | Auth | Purpose |
|--------|----------------|------|---------|
| GET    | `/healthz`     | —    | Liveness |
| GET    | `/readyz`      | —    | Readiness (DB ping) |
| POST   | `/v1/runs`     | bearer (optional) | Ingest one report |
| GET    | `/v1/runs`     | bearer (optional) | List recent runs (`?limit=N`, default 50, max 500) |
| GET    | `/v1/runs/{id}`| bearer (optional) | Fetch one full report |

Full spec: [`docs/api/openapi.yaml`](../../docs/api/openapi.yaml).

## Configuration (env)

| Var | Default | Purpose |
|---|---|---|
| `PORT`           | `8085` | listen port |
| `POSTGRES_URL`   | `postgres://cienergy:cienergy@localhost:5432/cienergy?sslmode=disable` | DSN |
| `INGESTER_TOKEN` | *(empty)* | if set, `/v1/*` requires `Authorization: Bearer <token>` |
| `MAX_BODY_BYTES` | `1048576` (1 MiB) | request body limit |

## Run

### With the bundled podman Compose stack

```sh
cd dashboard/grafana
podman compose up -d --build
# postgres   :5432
# ingester   :8085
# grafana    :3000   (anonymous Viewer)
```

### Standalone

```sh
go build -o bin/cienergy-ingester ./cmd/ingester
POSTGRES_URL='postgres://cienergy:cienergy@localhost:5432/cienergy?sslmode=disable' \
  ./bin/cienergy-ingester
```

### podman image only

```sh
podman build -t cienergy/ingester:dev -f cmd/ingester/Dockerfile .
podman run --rm -p 8085:8085 \
  -e POSTGRES_URL='postgres://cienergy:cienergy@host.docker.internal:5432/cienergy?sslmode=disable' \
  cienergy/ingester:dev
```

## Push a report from CI

The provided GitHub Action and Azure DevOps template both accept an
`ingester-url` (and optional `ingester-token`) input. When set, the report is
POSTed after the artifact upload:

```yaml
# GitHub Actions
- uses: axa-oss/cienergy-action@v1
  with:
    ingester-url:   https://cienergy.example.com
    ingester-token: ${{ secrets.CIENERGY_TOKEN }}
```

```yaml
# Azure DevOps
- template: pipeline/azure-devops/cienergy-step-template.yml@cienergy
  parameters:
    ingesterUrl: https://cienergy.example.com
    ingesterTokenVar: CIENERGY_TOKEN
    steps: [ ... ]
```

Push is **best-effort**: 3 retries with exponential backoff, never fails the
build on telemetry error (CI sustainability ≠ blocker).

## Seed the dev stack with sample reports

```sh
make stack-up        # starts postgres + ingester + grafana
make seed-samples    # POSTs the 5 bundled samples to localhost:8085
open http://localhost:3000   # Grafana → cienergy/overview
```

## Storage model

Reports go into two tables — see [`dashboard/grafana/postgres/init/01-schema.sql`](../../dashboard/grafana/postgres/init/01-schema.sql):

- `runs`       — one row per run, includes the raw JSON in a `JSONB raw` column.
- `run_steps`  — one row per step (FK to `runs.id`, cascade delete).

`POST /v1/runs` is **idempotent**: re-posting the same `run.id` overwrites the
energy/carbon columns and replaces the child step rows. This lets the agent
retry safely on partial failures.

## Curl quick test

```sh
curl -fsS -X POST http://localhost:8085/v1/runs \
  -H 'Content-Type: application/json' \
  --data @dashboard/embedded/sample-reports/run-001-baseline.json
# → 201
# {"energyKWh":0.020966,"id":"gha-7001","sciValue":9.118,"totalCO2":9.118}

curl -fsS 'http://localhost:8085/v1/runs?limit=10' | jq '.items[] | {id, repository, sci}'

curl -fsS http://localhost:8085/v1/runs/gha-7001 | jq '.sci'
```

## Observability

The service logs one line per request:

```
2026/06/04 18:00:01 POST /v1/runs 201 4.2ms
```

For production: front with a reverse proxy (nginx/Caddy/Envoy) for TLS
termination, request-id injection, and rate limiting. Prometheus metrics are
planned for v1.1 (Phase 5).

