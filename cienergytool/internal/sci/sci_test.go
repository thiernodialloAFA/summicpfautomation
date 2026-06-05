package sci

import (
	"math"
	"testing"
)

func TestCompute(t *testing.T) {
	op, total, s := Compute(0.0123, 391, 0.92, 1)
	if math.Abs(op-4.8093) > 0.001 {
		t.Fatalf("operational = %v", op)
	}
	if math.Abs(total-5.7293) > 0.001 {
		t.Fatalf("total = %v", total)
	}
	if math.Abs(s-total) > 1e-9 {
		t.Fatalf("sci should equal total when R=1, got %v", s)
	}
}

func TestComputeRZeroFallsBackToOne(t *testing.T) {
	_, total, s := Compute(1, 100, 0, 0)
	if total != s {
		t.Fatalf("expected R=0 -> R=1; got total=%v sci=%v", total, s)
	}
}

