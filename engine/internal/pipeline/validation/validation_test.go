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
						Name: "checkout", Uses: "actions/checkout@v7",
						Features: []model.FeatureRef{{ID: "github.checkout"}},
					},
				},
			},
		},
	}
	report := Validate(context.Background(), workflow)
	if report.Support != model.SupportPartial {
		t.Fatalf("support = %s, want partial", report.Support)
	}
	if workflow.Jobs[0].Steps[0].Support != model.SupportEmulated {
		t.Fatalf("step support = %s, want emulated", workflow.Jobs[0].Steps[0].Support)
	}
}

func TestValidationClassifiesEmptyJobsAndAmbiguousStepsAsRejected(t *testing.T) {
	workflow := &model.Workflow{
		WorkflowSummary: model.WorkflowSummary{Provider: model.ProviderGitHub, Path: ".github/workflows/test.yml"},
		Jobs: []model.Job{
			{ID: "empty", Name: "empty", Runner: "ubuntu-latest"},
			{
				ID: "ambiguous", Name: "ambiguous", Runner: "ubuntu-latest",
				Steps: []model.Step{{Name: "both", Run: "echo run", Uses: "owner/action@v1"}},
			},
		},
	}

	report := Validate(context.Background(), workflow)
	for _, id := range []string{"common.empty-job", "common.ambiguous-step"} {
		feature := findFeature(report.Features, id)
		if feature == nil {
			t.Fatalf("missing %s in %#v", id, report.Features)
		}
		if feature.RuntimeDisposition != model.RuntimeReject || feature.Support != model.SupportUnsupported {
			t.Fatalf("%s is not rejected: %#v", id, feature)
		}
	}
}

func findFeature(features []model.FeatureSupport, id string) *model.FeatureSupport {
	for index := range features {
		if features[index].FeatureID == id {
			return &features[index]
		}
	}
	return nil
}
