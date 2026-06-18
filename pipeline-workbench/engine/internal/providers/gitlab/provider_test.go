package gitlab

import (
	"testing"

	"github.com/pipeline-workbench/engine/internal/pipeline/model"
)

func TestParseGitLabStagesNeedsImageAndUnsupportedFeatures(t *testing.T) {
	workflow, err := parseWorkflow(".gitlab-ci.yml", []byte(`
stages: [build, test, deploy]
image: golang:1.24
variables:
  GOFLAGS: -mod=mod
include:
  - local: ci/common.yml

build:
  stage: build
  script:
    - go build ./...

test:
  stage: test
  needs:
    - build
  services:
    - postgres:16
  script: go test ./...

deploy:
  stage: deploy
  rules:
    - if: $CI_COMMIT_BRANCH == "main"
  artifacts:
    paths: [dist]
  script: echo deploy
`))
	if err != nil {
		t.Fatalf("parse workflow: %v", err)
	}
	if workflow.Provider != model.ProviderGitLab {
		t.Fatalf("provider = %s, want gitlab", workflow.Provider)
	}
	if len(workflow.Jobs) != 3 {
		t.Fatalf("jobs = %d, want 3", len(workflow.Jobs))
	}
	if workflow.Jobs[0].Image != "golang:1.24" {
		t.Fatalf("build image = %q, want golang:1.24", workflow.Jobs[0].Image)
	}
	if got := workflow.Jobs[1].Needs; len(got) != 1 || got[0] != "build" {
		t.Fatalf("test needs = %v, want [build]", got)
	}
	if workflow.Jobs[1].Support != model.SupportUnsupported {
		t.Fatalf("test support = %s, want unsupported", workflow.Jobs[1].Support)
	}
	if workflow.Jobs[2].If == "" {
		t.Fatal("expected deploy rules to be captured as condition text")
	}
	if len(workflow.Unsupported) == 0 {
		t.Fatal("expected include to be reported as workflow unsupported feature")
	}
}

func TestParseGitLabImplicitStageNeeds(t *testing.T) {
	workflow, err := parseWorkflow(".gitlab-ci.yml", []byte(`
stages: [build, test]
build:
  stage: build
  script: echo build
test:
  stage: test
  script: echo test
`))
	if err != nil {
		t.Fatalf("parse workflow: %v", err)
	}
	if got := workflow.Jobs[1].Needs; len(got) != 1 || got[0] != "build" {
		t.Fatalf("implicit needs = %v, want [build]", got)
	}
}
