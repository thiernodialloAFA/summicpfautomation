package nvml

import (
	"math"
	"testing"
	"time"
)

func TestIntegrateKWhConstantPower(t *testing.T) {
	t0 := time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC)
	// 1000 W constant over 1 hour → 1 kWh
	samples := []Sample{
		{At: t0, GPUWatts: []float64{1000}},
		{At: t0.Add(time.Hour), GPUWatts: []float64{1000}},
	}
	got := IntegrateKWh(samples)
	if math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("expected 1 kWh, got %v", got)
	}
}

func TestIntegrateKWhMultiGPU(t *testing.T) {
	t0 := time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC)
	// 2 × 400 W constant over 30 min → 0.4 kWh
	samples := []Sample{
		{At: t0, GPUWatts: []float64{400, 400}},
		{At: t0.Add(30 * time.Minute), GPUWatts: []float64{400, 400}},
	}
	got := IntegrateKWh(samples)
	if math.Abs(got-0.4) > 1e-9 {
		t.Fatalf("expected 0.4 kWh, got %v", got)
	}
}

func TestIntegrateKWhInsufficientData(t *testing.T) {
	if IntegrateKWh(nil) != 0 {
		t.Fatal("nil should be 0")
	}
	if IntegrateKWh([]Sample{{At: time.Now(), GPUWatts: []float64{100}}}) != 0 {
		t.Fatal("single sample should be 0")
	}
}

