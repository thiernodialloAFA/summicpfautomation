package ecoci

import (
	"math"
	"testing"
)

func TestEstimateKWh(t *testing.T) {
	// 100W full-load CPU for 1 hour = 0.1 kWh
	got := EstimateKWh(100, 100, 3600)
	if math.Abs(got-0.1) > 1e-9 {
		t.Fatalf("expected 0.1 kWh, got %v", got)
	}
}

func TestEstimateKWhClamps(t *testing.T) {
	if EstimateKWh(0, 50, 60) != 0 {
		t.Fatal("zero TDP must yield 0")
	}
	if EstimateKWh(100, -10, 60) != 0 {
		t.Fatal("negative util clamped to 0")
	}
	a := EstimateKWh(100, 150, 60)
	b := EstimateKWh(100, 100, 60)
	if a != b {
		t.Fatalf("util>100 must clamp; %v != %v", a, b)
	}
}

