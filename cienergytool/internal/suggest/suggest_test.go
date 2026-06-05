package suggest

import (
	"testing"

	"github.com/axa-oss/cienergytool/internal/model"
)

func base() *model.Report {
	return &model.Report{
		Runner: model.Runner{Arch: "x86_64", TDPWatts: 270},
		Energy: model.Energy{TotalKWh: 0.05},
		Carbon: model.Carbon{GridIntensity: model.GridIntensity{ValueGCO2eqPerKWh: 400, Zone: "US-VA"}},
	}
}

func hasID(ss []model.Suggestion, id string) bool {
	for _, s := range ss {
		if s.ID == id {
			return true
		}
	}
	return false
}

func TestDependencyCacheTriggers(t *testing.T) {
	r := base()
	r.Energy.ByStep = []model.Step{
		{Name: "setup-java", KWh: 0.002, DurationSeconds: 12, CPUUtilPct: 35},
		{Name: "build", KWh: 0.01, DurationSeconds: 200, CPUUtilPct: 70},
	}
	out := For(r)
	if !hasID(out, "enable-dependency-cache") {
		t.Errorf("expected enable-dependency-cache, got %+v", out)
	}
}

func TestDirtyGridCriticalAndClean(t *testing.T) {
	r := base()
	r.Energy.ByStep = []model.Step{{Name: "build", KWh: 0.05, CPUUtilPct: 70}}
	out := For(r)
	if !hasID(out, "move-to-cleaner-zone") {
		t.Errorf("expected move-to-cleaner-zone for 400 g/kWh zone")
	}
	r.Carbon.GridIntensity.ValueGCO2eqPerKWh = 56
	out = For(r)
	if hasID(out, "move-to-cleaner-zone") {
		t.Errorf("did not expect move-to-cleaner-zone for FR-clean zone")
	}
}

func TestDockerCacheRule(t *testing.T) {
	r := base()
	r.Energy.ByStep = []model.Step{
		{Name: "podman build claims", KWh: 0.02, DurationSeconds: 60, CPUUtilPct: 60},
	}
	out := For(r)
	if !hasID(out, "enable-docker-layer-cache") {
		t.Errorf("expected enable-docker-layer-cache")
	}
	// Adding a buildx hint must suppress the suggestion.
	r.Energy.ByStep = append(r.Energy.ByStep, model.Step{Name: "podman buildx cache-from gha", KWh: 0.001})
	if hasID(For(r), "enable-docker-layer-cache") {
		t.Errorf("did not expect enable-docker-layer-cache once buildx is present")
	}
}

func TestRedundantBuildsRule(t *testing.T) {
	r := base()
	r.Energy.ByStep = []model.Step{
		{Name: "build baseline", KWh: 0.01, CPUUtilPct: 70},
		{Name: "build optimized", KWh: 0.012, CPUUtilPct: 70},
	}
	if !hasID(For(r), "share-build-artifacts") {
		t.Errorf("expected share-build-artifacts")
	}
}

func TestSeveritySorting(t *testing.T) {
	r := base()
	r.Energy.ByStep = []model.Step{
		{Name: "setup-java", KWh: 0.002, CPUUtilPct: 35},
		{Name: "podman build", KWh: 0.02, CPUUtilPct: 60},
		{Name: "build", KWh: 0.01, CPUUtilPct: 70},
		{Name: "build-step-2", KWh: 0.01, CPUUtilPct: 70},
	}
	out := For(r)
	if len(out) < 2 {
		t.Fatalf("expected multiple suggestions, got %d", len(out))
	}
	rank := map[string]int{"critical": 0, "major": 1, "minor": 2, "info": 3}
	for i := 1; i < len(out); i++ {
		if rank[out[i-1].Severity] > rank[out[i].Severity] {
			t.Errorf("not sorted: %s before %s", out[i-1].Severity, out[i].Severity)
		}
	}
}

