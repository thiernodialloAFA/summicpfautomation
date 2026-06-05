-- cienergy storage schema (Postgres 16).
-- One row per run, plus a child table for per-step samples.

CREATE TABLE IF NOT EXISTS runs (
  id                   TEXT        PRIMARY KEY,
  platform             TEXT        NOT NULL,
  repository           TEXT        NOT NULL,
  workflow             TEXT        NOT NULL,
  git_ref              TEXT,
  commit_sha           TEXT        NOT NULL,
  started_at           TIMESTAMPTZ NOT NULL,
  ended_at             TIMESTAMPTZ NOT NULL,
  duration_seconds     DOUBLE PRECISION NOT NULL,
  arch                 TEXT        NOT NULL,
  cpu_model            TEXT,
  tdp_watts            DOUBLE PRECISION,
  provider             TEXT,
  region               TEXT,
  is_spot              BOOLEAN     DEFAULT FALSE,
  energy_kwh           DOUBLE PRECISION NOT NULL,
  grid_intensity       DOUBLE PRECISION NOT NULL,
  grid_source          TEXT        NOT NULL,
  grid_zone            TEXT        NOT NULL,
  grid_ts              TIMESTAMPTZ NOT NULL,
  operational_gco2eq   DOUBLE PRECISION NOT NULL,
  embodied_gco2eq      DOUBLE PRECISION NOT NULL,
  total_gco2eq         DOUBLE PRECISION NOT NULL,
  sci_value            DOUBLE PRECISION NOT NULL,
  sci_R                DOUBLE PRECISION NOT NULL,
  functional_unit      TEXT,
  cache_hit            BOOLEAN     DEFAULT FALSE,
  saved_kwh            DOUBLE PRECISION DEFAULT 0,
  saved_gco2eq         DOUBLE PRECISION DEFAULT 0,
  team                 TEXT,
  cost_center          TEXT,
  labels               JSONB,
  raw                  JSONB
);

CREATE INDEX IF NOT EXISTS idx_runs_started_at ON runs (started_at DESC);
CREATE INDEX IF NOT EXISTS idx_runs_repository ON runs (repository);
CREATE INDEX IF NOT EXISTS idx_runs_team       ON runs (team);

CREATE TABLE IF NOT EXISTS run_steps (
  run_id            TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  step_order        INT  NOT NULL,
  name              TEXT NOT NULL,
  duration_seconds  DOUBLE PRECISION NOT NULL,
  cpu_util_pct      DOUBLE PRECISION,
  kwh               DOUBLE PRECISION NOT NULL,
  gpu_kwh           DOUBLE PRECISION DEFAULT 0,
  source            TEXT NOT NULL,
  PRIMARY KEY (run_id, step_order)
);

-- Convenience view for the dashboard.
CREATE OR REPLACE VIEW v_run_metrics AS
SELECT
  r.started_at,
  r.repository,
  r.workflow,
  r.team,
  r.arch,
  r.grid_zone,
  r.grid_intensity,
  r.energy_kwh,
  r.total_gco2eq,
  r.sci_value,
  r.cache_hit,
  r.saved_gco2eq
FROM runs r;

