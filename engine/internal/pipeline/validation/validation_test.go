package validation

import (
	"context"
	"testing"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
)

func TestValidationRejectsUnknownRegistryFeatureID(t *testing.T) {
	workflow := &model.Workflow{
		WorkflowSummary: model.WorkflowSummary{Provider: model.ProviderGitHub, Path: ".github/workflows/test.yml"},
		Features:        []model.FeatureRef{{ID: "github.does-not-exist", Path: "mystery"}},
		Jobs: []model.Job{
			{
				ID: "test", Name: "test", Runner: "ubuntu-latest",
				Steps: []model.Step{
					{Name: "run", Run: "echo ok", Features: []model.FeatureRef{{ID: "common.shell"}}},
				},
			},
		},
	}
	report := Validate(context.Background(), workflow)
	if report.Valid {
		t.Fatal("unknown registry feature id must invalidate validation")
	}
	if len(report.Issues) == 0 || report.Issues[0].Code != "support.unknown_feature_id" {
		t.Fatalf("unexpected issues: %#v", report.Issues)
	}
}

func TestValidationAggregatesSixStateSupport(t *testing.T) {
	workflow := &model.Workflow{
		WorkflowSummary: model.WorkflowSummary{Provider: model.ProviderGitHub, Path: ".github/workflows/test.yml"},
		Jobs: []model.Job{
			{
				ID: "test", Name: "test", Runner: "ubuntu-latest",
				Features: []model.FeatureRef{{ID: "github.runner"}},
				Steps: []model.Step{
					{
						Name: "checkout", Uses: "actions/checkout@v4",
						Features: []model.FeatureRef{{ID: "github.checkout"}},
					},
				},
			},
		},
	}
	report := Validate(context.Background(), workflow)
	if report.Support != model.SupportValidationOnly {
		t.Fatalf("support = %s, want validation-only", report.Support)
	}
	if workflow.Jobs[0].Steps[0].Support != model.SupportEmulated {
		t.Fatalf("step support = %s, want emulated", workflow.Jobs[0].Steps[0].Support)
	}
}
