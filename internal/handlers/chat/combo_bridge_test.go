package chat

import (
	"context"
	"testing"
)

func TestGetActiveCandidates(t *testing.T) {
	// Simple stub test to ensure bridge logic returns without panic
	nodes := getActiveCandidates(context.Background(), "test-model")

	// Should be 0 since the registry state is empty/uninitialized here
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
}
