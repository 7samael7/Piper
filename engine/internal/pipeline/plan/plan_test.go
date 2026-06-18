package plan

import (
	"testing"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
)

func TestCompileExpandsMatrixAndDependencies(t *testing.T) {
	workflow := &model.Workflow{Jobs: []model.Job{
		{
			ID: "test", Name: "Test",
			Matrix: &model.MatrixSpec{Dimensions: map[string][]interface{}{
				"os":   {"linux", "windows"},
				"node": {20, 22},
			}},
		},
		{ID: "deploy", Name: "Deploy", Needs: []string{"test"}},
	}}
	compiled, err := Compile(workflow, 128, 4)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(compiled.Jobs) != 5 {
		t.Fatalf("jobs = %d, want 5", len(compiled.Jobs))
	}
	if len(compiled.Jobs[4].Needs) != 4 {
		t.Fatalf("deploy needs = %v", compiled.Jobs[4].Needs)
	}
}

func TestCompileEnforcesExpansionLimit(t *testing.T) {
	workflow := &model.Workflow{Jobs: []model.Job{{
		ID: "test",
		Matrix: &model.MatrixSpec{Dimensions: map[string][]interface{}{
			"a": {1, 2, 3},
			"b": {1, 2, 3},
		}},
	}}}
	if _, err := Compile(workflow, 8, 4); err == nil {
		t.Fatal("expected expansion limit error")
	}
}
