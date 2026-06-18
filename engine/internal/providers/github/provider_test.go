package github

import (
	"testing"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
)

func TestParseWorkflowDispatchInputsAndSupport(t *testing.T) {
	workflow, err := parseWorkflow(".github/workflows/build.yml", []byte(`
name: Build
on:
  workflow_dispatch:
    inputs:
      target:
        description: Deploy target
        required: true
        default: staging
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Lint
        run: echo lint
  deploy:
    needs: lint
    runs-on: ubuntu-latest
    steps:
      - uses: acme/deploy@v1
`))
	if err != nil {
		t.Fatalf("parse workflow: %v", err)
	}

	if workflow.Name != "Build" {
		t.Fatalf("workflow name = %q, want Build", workflow.Name)
	}
	if len(workflow.Triggers) != 1 || workflow.Triggers[0].Name != "workflow_dispatch" {
		t.Fatalf("unexpected triggers: %#v", workflow.Triggers)
	}
	if got := workflow.Triggers[0].Inputs[0]; got.Name != "target" || !got.Required || got.Default != "staging" {
		t.Fatalf("unexpected workflow_dispatch input: %#v", got)
	}
	if workflow.Jobs[0].Steps[0].Support != model.SupportPartial {
		t.Fatalf("checkout support = %s, want partial", workflow.Jobs[0].Steps[0].Support)
	}
	if workflow.Jobs[1].Support != model.SupportPartial {
		t.Fatalf("deploy support = %s, want partial", workflow.Jobs[1].Support)
	}
	if !githubHasFeature(workflow.Jobs[1].Steps[0].Features, "github.remote-action") {
		t.Fatal("expected stable remote action feature id")
	}
}

func githubHasFeature(features []model.FeatureRef, id string) bool {
	for _, feature := range features {
		if feature.ID == id {
			return true
		}
	}
	return false
}

func TestParseReusableWorkflowJob(t *testing.T) {
	workflow, err := parseWorkflow(".github/workflows/reuse.yml", []byte(`
on: push
jobs:
  call:
    uses: org/repo/.github/workflows/reusable.yml@main
`))
	if err != nil {
		t.Fatalf("parse workflow: %v", err)
	}
	if workflow.Jobs[0].ReusableWorkflow == "" {
		t.Fatal("expected reusable workflow job to be captured")
	}
	if workflow.Jobs[0].Support != model.SupportUnsupported {
		t.Fatalf("support = %s, want unsupported", workflow.Jobs[0].Support)
	}
}

func TestParseSetupActionsAndJobRunDefaults(t *testing.T) {
	workflow, err := parseWorkflow(".github/workflows/ci.yml", []byte(`
name: CI
on: push
jobs:
  backend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-dotnet@v4
        with:
          dotnet-version: 8.0.x
      - run: dotnet build
  frontend:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: frontend
    steps:
      - uses: actions/setup-node@v4
        with:
          node-version: 20
      - run: npm ci
`))
	if err != nil {
		t.Fatalf("parse workflow: %v", err)
	}

	if workflow.Jobs[0].Steps[0].Support != model.SupportPartial {
		t.Fatalf("setup-dotnet support = %s, want partial", workflow.Jobs[0].Steps[0].Support)
	}
	if workflow.Jobs[1].Steps[0].Support != model.SupportPartial {
		t.Fatalf("setup-node support = %s, want partial", workflow.Jobs[1].Steps[0].Support)
	}
	if got := workflow.Jobs[1].Steps[1].WorkingDirectory; got != "frontend" {
		t.Fatalf("working directory = %q, want frontend", got)
	}
}

func TestEmptyAndUnknownStepsHaveStructuredFeatureReferences(t *testing.T) {
	workflow, err := parseWorkflow(".github/workflows/invalid.yml", []byte(`
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Empty
      - name: Unknown
        timeout-minutes: 2
`))
	if err != nil {
		t.Fatal(err)
	}
	if !githubHasFeature(workflow.Jobs[0].Steps[0].Features, "common.empty-step") {
		t.Fatal("empty step must be explicitly unsupported")
	}
	if !githubHasFeature(workflow.Jobs[0].Steps[1].Features, "github.unknown") {
		t.Fatal("unknown step key must be explicit")
	}
	if origin := workflow.Jobs[0].Steps[0].Features[0].Origin; origin == nil || origin.File == "" || origin.Line == 0 {
		t.Fatalf("missing source location: %#v", origin)
	}
}
