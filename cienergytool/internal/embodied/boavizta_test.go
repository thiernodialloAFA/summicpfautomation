package embodied

import (
	"context"
	"math"
	"testing"
)

func TestResolveFallsBackToCCFStatic(t *testing.T) {
	r := &Resolver{BaseURL: "", LifetimeYears: DefaultLifetimeYears}
	res := r.Resolve(context.Background(), "Unknown CPU", 3600)
	if res.Source != "ccf-static" {
		t.Fatalf("expected ccf-static fallback, got %s", res.Source)
	}
	// 250 kgCO2eq over 4 years prorated to 1h:
	// share = 3600 / (4 * 365.25 * 24 * 3600) ≈ 2.852e-5
	// gco2eq = 250 * 1000 * share ≈ 7.13
	expected := 250.0 * 1000.0 * (3600.0 / (DefaultLifetimeYears * 365.25 * 24 * 3600))
	if math.Abs(res.GCO2eqForRun-expected) > 0.01 {
		t.Fatalf("prorated gCO2eq mismatch: got %v, want %v", res.GCO2eqForRun, expected)
	}
}

func TestResolveZeroDurationYieldsZero(t *testing.T) {
	r := &Resolver{BaseURL: "", LifetimeYears: DefaultLifetimeYears}
	if v := r.Resolve(context.Background(), "x", 0); v.GCO2eqForRun != 0 {
		t.Fatalf("zero duration must yield 0, got %v", v.GCO2eqForRun)
	}
}

