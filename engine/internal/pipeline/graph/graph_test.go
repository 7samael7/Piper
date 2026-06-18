package graph

import (
	"testing"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
)

func TestTopologicalSortOrdersNeedsBeforeDependents(t *testing.T) {
	workflow := &model.Workflow{
		Jobs: []model.Job{
			{ID: "test", Needs: []string{"lint"}},
			{ID: "lint"},
			{ID: "deploy", Needs: []string{"test"}},
		},
	}

	ordered, err := TopologicalSort(workflow)
	if err != nil {
		t.Fatalf("topological sort: %v", err)
	}
	got := []string{ordered[0].ID, ordered[1].ID, ordered[2].ID}
	want := []string{"lint", "test", "deploy"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestTopologicalSortRejectsMissingNeed(t *testing.T) {
	workflow := &model.Workflow{
		Jobs: []model.Job{{ID: "test", Needs: []string{"lint"}}},
	}
	if _, err := TopologicalSort(workflow); err == nil {
		t.Fatal("expected missing dependency error")
	}
}

func TestTopologicalSortRejectsCycle(t *testing.T) {
	workflow := &model.Workflow{
		Jobs: []model.Job{
			{ID: "a", Needs: []string{"b"}},
			{ID: "b", Needs: []string{"a"}},
		},
	}
	if _, err := TopologicalSort(workflow); err == nil {
		t.Fatal("expected cycle error")
	}
}
