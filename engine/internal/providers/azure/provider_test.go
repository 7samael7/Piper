package azure

import (
	"testing"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
)

func TestParseAzureStagesJobsAndSteps(t *testing.T) {
	workflow, err := parseWorkflow("azure-pipelines.yml", []byte(`
name: ci
trigger:
  branches:
    include: [main]
variables:
  CONFIGURATION: Release
stages:
  - stage: Build
    jobs:
      - job: compile
        pool:
          vmImage: ubuntu-latest
        steps:
          - checkout: self
          - bash: echo build
      - job: package
        dependsOn: compile
        steps:
          - task: PublishBuildArtifacts@1
  - stage: Test
    jobs:
      - job: test
        steps:
          - script: echo test
`))
	if err != nil {
		t.Fatalf("parse workflow: %v", err)
	}
	if workflow.Provider != model.ProviderAzure {
		t.Fatalf("provider = %s, want azure", workflow.Provider)
	}
	if len(workflow.Jobs) != 3 {
		t.Fatalf("jobs = %d, want 3", len(workflow.Jobs))
	}
	if workflow.Jobs[0].ID != "Build.compile" {
		t.Fatalf("first job id = %q, want Build.compile", workflow.Jobs[0].ID)
	}
	if workflow.Jobs[0].Steps[0].Support != model.SupportPartial {
		t.Fatalf("checkout support = %s, want partial", workflow.Jobs[0].Steps[0].Support)
	}
	if got := workflow.Jobs[1].Needs; len(got) != 1 || got[0] != "Build.compile" {
		t.Fatalf("package needs = %v, want [Build.compile]", got)
	}
	if workflow.Jobs[1].Support != model.SupportPartial {
		t.Fatalf("package support = %s, want partial task support", workflow.Jobs[1].Support)
	}
	if !azureHasFeature(workflow.Jobs[1].Steps[0].Features, "azure.task-storage") {
		t.Fatal("expected stable artifact task feature id")
	}
	if got := workflow.Jobs[2].Needs; len(got) != 2 || got[0] != "Build.compile" || got[1] != "Build.package" {
		t.Fatalf("test implicit stage needs = %v, want build stage jobs", got)
	}
}

func azureHasFeature(features []model.FeatureRef, id string) bool {
	for _, feature := range features {
		if feature.ID == id {
			return true
		}
	}
	return false
}

func TestParseAzureRootSteps(t *testing.T) {
	workflow, err := parseWorkflow("azure-pipelines.yml", []byte(`
pool:
  vmImage: ubuntu-latest
steps:
  - script: echo root
`))
	if err != nil {
		t.Fatalf("parse workflow: %v", err)
	}
	if len(workflow.Jobs) != 1 || workflow.Jobs[0].ID != "pipeline" {
		t.Fatalf("unexpected root-step job mapping: %#v", workflow.Jobs)
	}
	if workflow.Jobs[0].Runner != "ubuntu-latest" {
		t.Fatalf("runner = %q, want ubuntu-latest", workflow.Jobs[0].Runner)
	}
}

func TestAzureTemplatesResourcesAndTimeoutsAreExplicit(t *testing.T) {
	workflow, err := parseWorkflow("azure-pipelines.yml", []byte(`
parameters:
  - name: target
resources:
  repositories: []
jobs:
  - job: test
    timeoutInMinutes: 10
    steps:
      - template: steps.yml
`))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"azure.parameters", "azure.resources"} {
		if !azureHasFeature(workflow.Features, id) {
			t.Fatalf("missing %s", id)
		}
	}
	if !azureHasFeature(workflow.Jobs[0].Features, "azure.timeout") {
		t.Fatal("missing timeout support reference")
	}
	if !azureHasFeature(workflow.Jobs[0].Steps[0].Features, "azure.templates") {
		t.Fatal("missing template support reference")
	}
}

func TestAzureEmptyJobsAndAmbiguousStepsCannotSilentlySucceed(t *testing.T) {
	workflow, err := parseWorkflow("azure-pipelines.yml", []byte(`
jobs:
  - job: empty
  - job: invalid
    steps:
      - echo not-a-step-mapping
      - script: echo script
        task: Bash@3
        inputs:
          script: echo task
`))
	if err != nil {
		t.Fatal(err)
	}
	if !azureHasFeature(workflow.Jobs[0].Features, "common.empty-job") {
		t.Fatal("job without steps must be explicitly unsupported")
	}
	if !azureHasFeature(workflow.Jobs[1].Steps[0].Features, "common.empty-step") {
		t.Fatal("non-mapping step must be explicitly unsupported")
	}
	if !azureHasFeature(workflow.Jobs[1].Steps[1].Features, "common.ambiguous-step") {
		t.Fatal("multiple executable forms must be rejected")
	}
}

func TestAzureNodeToolNormalizesToExecutableSetupStep(t *testing.T) {
	workflow, err := parseWorkflow("azure-pipelines.yml", []byte(`
jobs:
  - job: node
    steps:
      - task: NodeTool@0
        inputs:
          versionSpec: "22"
`))
	if err != nil {
		t.Fatal(err)
	}
	step := workflow.Jobs[0].Steps[0]
	if step.Uses != "actions/setup-node@local" || step.Name != "NodeTool@0" {
		t.Fatalf("unexpected normalized task: %#v", step)
	}
	if !azureHasFeature(step.Features, "azure.task-runtime") {
		t.Fatal("missing Azure task-runtime classification")
	}
}
