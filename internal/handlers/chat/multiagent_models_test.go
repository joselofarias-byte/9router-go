package chat

import (
	"reflect"
	"testing"
)

func TestModelPickerNormalizesOnlyGenuineAliases(t *testing.T) {
	models := []ModelInfoObject{
		{ID: "ag/shared-model", OwnedBy: "ag"},
		{ID: "antigravity/shared-model", OwnedBy: "antigravity"},
		{ID: "oc/shared-model", OwnedBy: "oc"},
		{ID: "custom-one/shared-model", OwnedBy: "custom-one"},
		{ID: "custom-two/shared-model", OwnedBy: "custom-two"},
	}
	want := []modelCandidate{
		{ID: "antigravity/shared-model", OwnedBy: "antigravity", Provider: "antigravity"},
		{ID: "custom-one/shared-model", OwnedBy: "custom-one", Provider: "custom-one"},
		{ID: "custom-two/shared-model", OwnedBy: "custom-two", Provider: "custom-two"},
		{ID: "oc/shared-model", OwnedBy: "oc", Provider: "opencode"},
	}
	if got := normalizeModelCandidates(models); !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates: %+v", got)
	}
	for i, j := 0, len(models)-1; i < j; i, j = i+1, j-1 {
		models[i], models[j] = models[j], models[i]
	}
	if got := normalizeModelCandidates(models); !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog order changed selection: %+v", got)
	}
}
