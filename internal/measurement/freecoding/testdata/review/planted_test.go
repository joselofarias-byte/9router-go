package task

import "testing"

func TestClampPositive(t *testing.T) {
	if got := ClampPositive(-3); got != 0 {
		t.Fatalf("ClampPositive(-3) = %d, want 0", got)
	}
	if got := ClampPositive(4); got != 4 {
		t.Fatalf("ClampPositive(4) = %d, want 4", got)
	}
}
