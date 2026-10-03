package task

import "testing"

func TestTotal(t *testing.T) {
	if got := Total([]int{1, 2, 3}); got != 6 {
		t.Fatalf("Total = %d, want 6", got)
	}
	if got := Total(nil); got != 0 {
		t.Fatalf("Total(nil) = %d, want 0", got)
	}
}

func TestAverage(t *testing.T) {
	if got := Average([]int{2, 4}); got != 3 {
		t.Fatalf("Average = %d, want 3", got)
	}
	if got := Average(nil); got != 0 {
		t.Fatalf("Average(nil) = %d, want 0", got)
	}
}
