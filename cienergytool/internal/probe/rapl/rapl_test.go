package rapl

import (
	"math"
	"testing"
)

func TestUJToKWh(t *testing.T) {
	// 1 kWh = 3.6e15 µJ
	if got := ujToKWh(3.6e15); math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("expected 1.0, got %v", got)
	}
	if got := ujToKWh(0); got != 0 {
		t.Fatalf("expected 0, got %v", got)
	}
}

func TestAvailableHandlesMissingSysfs(t *testing.T) {
	// On macOS / Windows CI, /sys/class/powercap doesn't exist.
	// The probe must report unavailable without error.
	p, ok, err := Available()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok && p != nil {
		t.Fatalf("probe must be nil when unavailable")
	}
}

