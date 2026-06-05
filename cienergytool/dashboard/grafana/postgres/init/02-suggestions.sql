-- Add suggestions storage. JSONB so dashboards can dispatch on the schema
-- without us managing a new table.
ALTER TABLE runs ADD COLUMN IF NOT EXISTS suggestions JSONB DEFAULT '[]'::jsonb;

-- Convenience view that flattens one row per (run, suggestion) for Grafana.
CREATE OR REPLACE VIEW v_run_suggestions AS
SELECT
  r.started_at,
  r.repository,
  r.workflow,
  r.team,
  r.grid_zone,
  s.value->>'id'                                        AS suggestion_id,
  s.value->>'severity'                                  AS severity,
  s.value->>'title'                                     AS title,
  s.value->>'detail'                                    AS detail,
  COALESCE((s.value->>'estimatedSavingKWh')::float, 0)  AS saving_kwh,
  COALESCE((s.value->>'estimatedSavingGCO2eq')::float,0) AS saving_gco2eq,
  s.value->>'reference'                                 AS reference
FROM runs r,
     LATERAL jsonb_array_elements(COALESCE(r.suggestions, '[]'::jsonb)) AS s(value);

