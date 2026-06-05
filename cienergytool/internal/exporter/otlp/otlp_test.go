package otlp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/axa-oss/cienergytool/internal/model"
)

func sampleReport() *model.Report {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	return &model.Report{
		SpecVersion: model.SpecVersion,
		Run: model.Run{
			ID: "test-1", Platform: "local", Repository: "x/y", Workflow: "ci",
			CommitSha: "abc123", StartedAt: now, EndedAt: now.Add(time.Minute),
			DurationSeconds: 60,
		},
		Runner: model.Runner{Arch: "x86_64", CPUModel: "test", Region: "FR"},
		Energy: model.Energy{TotalKWh: 0.012345},
		Carbon: model.Carbon{
			OperationalGCO2eq: 4.83, EmbodiedGCO2eq: 0.92, TotalGCO2eq: 5.75,
			GridIntensity: model.GridIntensity{ValueGCO2eqPerKWh: 391, Source: "ember", Zone: "FR", Timestamp: now},
		},
		SCI: model.SCI{Value: 5.75, Unit: "gCO2eq", FunctionalUnit: "1 run", R: 1},
	}
}

func TestExporterEmptyEndpointIsNoop(t *testing.T) {
	if err := New("").Export(context.Background(), sampleReport()); err != nil {
		t.Fatalf("empty endpoint should be a noop, got %v", err)
	}
}

func TestExporterEmitsOTLPJSON(t *testing.T) {
	var got map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/metrics" {
			t.Errorf("expected /v1/metrics, got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("wrong content-type: %s", r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	if err := New(ts.URL).Export(context.Background(), sampleReport()); err != nil {
		t.Fatalf("export failed: %v", err)
	}
	rm, ok := got["resourceMetrics"].([]any)
	if !ok || len(rm) != 1 {
		t.Fatalf("expected resourceMetrics[1], got %#v", got)
	}
}

