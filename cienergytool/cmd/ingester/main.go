// cienergy-ingester is a small HTTP service that accepts SCI-compliant
// energy reports and writes them to Postgres for the Grafana dashboard.
//
// Endpoints:
//   POST /v1/runs       — ingest one report (application/json)
//   GET  /v1/runs       — list recent runs (?limit=N, default 50)
//   GET  /v1/runs/{id}  — fetch a single run as JSON
//   GET  /healthz       — liveness probe
//   GET  /readyz        — readiness probe (DB ping)
//
// Configuration (env):
//   PORT                — listen port (default 8085)
//   POSTGRES_URL        — pgx-style DSN (e.g. postgres://user:pass@host:5432/db?sslmode=disable)
//   INGESTER_TOKEN      — if set, requests must carry  Authorization: Bearer <token>
//   MAX_BODY_BYTES      — request body size limit (default 1 MiB)
//
// Build:  go build -o bin/cienergy-ingester ./cmd/ingester
// Deps:   github.com/lib/pq  (run `go mod tidy` once)
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"github.com/axa-oss/cienergytool/internal/model"
)

const defaultMaxBody = 1 << 20 // 1 MiB

type server struct {
	db    *sql.DB
	token string
	max   int64
}

func main() {
	port := envOr("PORT", "8085")
	dsn := envOr("POSTGRES_URL", "postgres://cienergy:cienergy@localhost:5432/cienergy?sslmode=disable")
	token := os.Getenv("INGESTER_TOKEN")
	maxBody, _ := strconv.ParseInt(envOr("MAX_BODY_BYTES", strconv.Itoa(defaultMaxBody)), 10, 64)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)

	// Best-effort startup migration so older Postgres instances picked up the
	// suggestions column without recreating the volume. Safe to run on every
	// boot — IF NOT EXISTS makes both statements idempotent.
	if _, err := db.Exec(`ALTER TABLE runs ADD COLUMN IF NOT EXISTS suggestions JSONB DEFAULT '[]'::jsonb`); err != nil {
		log.Printf("warning: suggestions column migration: %v", err)
	}
	if _, err := db.Exec(`
		CREATE OR REPLACE VIEW v_run_suggestions AS
		SELECT r.started_at, r.repository, r.workflow, r.team, r.grid_zone,
		       s.value->>'id'                                          AS suggestion_id,
		       s.value->>'severity'                                    AS severity,
		       s.value->>'title'                                       AS title,
		       s.value->>'detail'                                      AS detail,
		       COALESCE((s.value->>'estimatedSavingKWh')::float, 0)    AS saving_kwh,
		       COALESCE((s.value->>'estimatedSavingGCO2eq')::float, 0) AS saving_gco2eq,
		       s.value->>'reference'                                   AS reference
		FROM runs r,
		     LATERAL jsonb_array_elements(COALESCE(r.suggestions, '[]'::jsonb)) AS s(value)`); err != nil {
		log.Printf("warning: v_run_suggestions view migration: %v", err)
	}

	srv := &server{db: db, token: token, max: maxBody}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", srv.handleHealth)
	mux.HandleFunc("/readyz", srv.handleReady)
	mux.HandleFunc("/v1/runs", srv.handleRuns)
	mux.HandleFunc("/v1/runs/", srv.handleRunByID)

	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           withLogging(srv.withAuth(mux)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Graceful shutdown.
	idleClosed := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
		close(idleClosed)
	}()

	log.Printf("cienergy-ingester listening on :%s (auth=%t)", port, token != "")
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("listen: %v", err)
	}
	<-idleClosed
	log.Println("bye")
}

// ----------------------------------------------------------------------------
// Handlers
// ----------------------------------------------------------------------------

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) }

func (s *server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		writeJSON(w, 503, map[string]string{"status": "down", "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ready"})
}

func (s *server) handleRuns(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.ingest(w, r)
	case http.MethodGet:
		s.list(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) handleRunByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/runs/")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	var raw []byte
	err := s.db.QueryRowContext(r.Context(), `SELECT raw FROM runs WHERE id = $1`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	} else if err != nil {
		writeError(w, 500, "query failed", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

func (s *server) ingest(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.max))
	if err != nil {
		writeError(w, 413, "request body too large", err)
		return
	}
	var report model.Report
	if err := json.Unmarshal(body, &report); err != nil {
		writeError(w, 400, "invalid JSON", err)
		return
	}
	if err := validate(&report); err != nil {
		writeError(w, 422, "schema validation failed", err)
		return
	}
	if err := s.upsert(r.Context(), &report, body); err != nil {
		writeError(w, 500, "insert failed", err)
		return
	}
	w.Header().Set("Location", "/v1/runs/"+report.Run.ID)
	writeJSON(w, 201, map[string]any{
		"id":        report.Run.ID,
		"sciValue":  report.SCI.Value,
		"totalCO2":  report.Carbon.TotalGCO2eq,
		"energyKWh": report.Energy.TotalKWh,
	})
}

func (s *server) list(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, repository, workflow, started_at, energy_kwh, total_gco2eq, sci_value, grid_zone, arch
		  FROM runs ORDER BY started_at DESC LIMIT $1`, limit)
	if err != nil {
		writeError(w, 500, "query failed", err)
		return
	}
	defer rows.Close()

	type row struct {
		ID         string    `json:"id"`
		Repository string    `json:"repository"`
		Workflow   string    `json:"workflow"`
		StartedAt  time.Time `json:"startedAt"`
		EnergyKWh  float64   `json:"energyKWh"`
		CO2        float64   `json:"totalGCO2eq"`
		SCI        float64   `json:"sci"`
		Zone       string    `json:"zone"`
		Arch       string    `json:"arch"`
	}
	out := make([]row, 0, limit)
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.ID, &x.Repository, &x.Workflow, &x.StartedAt, &x.EnergyKWh, &x.CO2, &x.SCI, &x.Zone, &x.Arch); err != nil {
			writeError(w, 500, "scan failed", err)
			return
		}
		out = append(out, x)
	}
	writeJSON(w, 200, map[string]any{"count": len(out), "items": out})
}

// ----------------------------------------------------------------------------
// DB
// ----------------------------------------------------------------------------

func (s *server) upsert(ctx context.Context, r *model.Report, raw []byte) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO runs (
			id, platform, repository, workflow, git_ref, commit_sha,
			started_at, ended_at, duration_seconds,
			arch, cpu_model, tdp_watts, provider, region, is_spot,
			energy_kwh,
			grid_intensity, grid_source, grid_zone, grid_ts,
			operational_gco2eq, embodied_gco2eq, total_gco2eq,
			sci_value, sci_R, functional_unit,
			cache_hit, saved_kwh, saved_gco2eq,
			team, cost_center, labels, suggestions, raw
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34
		)
		ON CONFLICT (id) DO UPDATE SET
			energy_kwh = EXCLUDED.energy_kwh,
			operational_gco2eq = EXCLUDED.operational_gco2eq,
			embodied_gco2eq = EXCLUDED.embodied_gco2eq,
			total_gco2eq = EXCLUDED.total_gco2eq,
			sci_value = EXCLUDED.sci_value,
			cache_hit = EXCLUDED.cache_hit,
			saved_kwh = EXCLUDED.saved_kwh,
			saved_gco2eq = EXCLUDED.saved_gco2eq,
			suggestions = EXCLUDED.suggestions,
			raw = EXCLUDED.raw
	`,
		r.Run.ID, r.Run.Platform, r.Run.Repository, r.Run.Workflow, r.Run.Ref, r.Run.CommitSha,
		r.Run.StartedAt, r.Run.EndedAt, r.Run.DurationSeconds,
		r.Runner.Arch, r.Runner.CPUModel, r.Runner.TDPWatts, r.Runner.Provider, r.Runner.Region, r.Runner.IsSpot,
		r.Energy.TotalKWh,
		r.Carbon.GridIntensity.ValueGCO2eqPerKWh, r.Carbon.GridIntensity.Source, r.Carbon.GridIntensity.Zone, r.Carbon.GridIntensity.Timestamp,
		r.Carbon.OperationalGCO2eq, r.Carbon.EmbodiedGCO2eq, r.Carbon.TotalGCO2eq,
		r.SCI.Value, r.SCI.R, r.SCI.FunctionalUnit,
		cacheHit(r), savedKWh(r), savedGCO2eq(r),
		metaTeam(r), metaCC(r), metaLabels(r),
		suggestionsJSON(r),
		raw,
	)
	if err != nil {
		return fmt.Errorf("insert run: %w", err)
	}

	// Replace child step rows.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM run_steps WHERE run_id = $1`, r.Run.ID); err != nil {
		return fmt.Errorf("delete steps: %w", err)
	}
	for i, st := range r.Energy.ByStep {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO run_steps (run_id, step_order, name, duration_seconds, cpu_util_pct, kwh, gpu_kwh, source)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			r.Run.ID, i, st.Name, st.DurationSeconds, st.CPUUtilPct, st.KWh, st.GPUKWh, st.Source,
		); err != nil {
			return fmt.Errorf("insert step %d: %w", i, err)
		}
	}
	return nil
}

// ----------------------------------------------------------------------------
// Validation (lightweight, matches docs/schema/v1.json required fields).
// ----------------------------------------------------------------------------

func validate(r *model.Report) error {
	if r.SpecVersion == "" {
		return errors.New("missing specVersion")
	}
	if r.Run.ID == "" || r.Run.Repository == "" || r.Run.Workflow == "" || r.Run.CommitSha == "" {
		return errors.New("run.{id,repository,workflow,commitSha} are required")
	}
	if r.Run.StartedAt.IsZero() || r.Run.EndedAt.IsZero() {
		return errors.New("run.{startedAt,endedAt} are required")
	}
	if r.Runner.CPUModel == "" || r.Runner.Arch == "" {
		return errors.New("runner.{cpuModel,arch} are required")
	}
	if r.Energy.TotalKWh < 0 || r.Carbon.TotalGCO2eq < 0 || r.SCI.Value < 0 {
		return errors.New("energy/carbon/sci values must be >= 0")
	}
	if r.Carbon.GridIntensity.Source == "" || r.Carbon.GridIntensity.Zone == "" {
		return errors.New("carbon.gridIntensity.{source,zone} are required")
	}
	return nil
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func cacheHit(r *model.Report) bool {
	return r.Cache != nil && r.Cache.Hit
}
func savedKWh(r *model.Report) float64 {
	if r.Cache == nil { return 0 }; return r.Cache.SavedKWhEstimate
}
func savedGCO2eq(r *model.Report) float64 {
	if r.Cache == nil { return 0 }; return r.Cache.SavedGCO2eqEstimate
}
func metaTeam(r *model.Report) string {
	if r.Metadata == nil { return "" }; return r.Metadata.Team
}
func metaCC(r *model.Report) string {
	if r.Metadata == nil { return "" }; return r.Metadata.CostCenter
}
func metaLabels(r *model.Report) []byte {
	if r.Metadata == nil || len(r.Metadata.Labels) == 0 {
		return []byte("null")
	}
	b, _ := json.Marshal(r.Metadata.Labels)
	return b
}

// suggestionsJSON serialises the improvement suggestions to a JSONB-ready
// payload. Empty arrays are stored as '[]' (never NULL) so the v_run_suggestions
// view always behaves the same.
func suggestionsJSON(r *model.Report) []byte {
	if len(r.Suggestions) == 0 {
		return []byte("[]")
	}
	b, _ := json.Marshal(r.Suggestions)
	return b
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, code int, msg string, err error) {
	log.Printf("error: %s: %v", msg, err)
	writeJSON(w, code, map[string]string{"error": msg, "detail": err.Error()})
}
func envOr(k, d string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return d
}

// ----------------------------------------------------------------------------
// Middleware
// ----------------------------------------------------------------------------

func withLogging(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: 200}
		h.ServeHTTP(rw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.status, time.Since(start))
	})
}

func (s *server) withAuth(h http.Handler) http.Handler {
	if s.token == "" {
		return h
	}
	expected := "Bearer " + s.token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow health endpoints without auth.
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			h.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Authorization") != expected {
			w.Header().Set("WWW-Authenticate", `Bearer realm="cienergy"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(c int) {
	s.status = c
	s.ResponseWriter.WriteHeader(c)
}

