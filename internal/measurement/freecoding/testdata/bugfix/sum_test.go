package task

import "testing"

func TestSum(t *testing.T) {
	if got := Sum(2, 3); got != 5 {
		t.Fatalf("Sum(2, 3) = %d, want 5", got)
	}
	if got := Sum(-1, 1); got != 0 {
		t.Fatalf("Sum(-1, 1) = %d, want 0", got)
	}
}
