package gitlab

import (
	"testing"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
)

func TestParseGitLabStagesNeedsImageAndUnsupportedFeatures(t *testing.T) {
	workflow, err := parseWorkflow(".gitlab-ci.yml", []byte(`
stages: [build, test, deploy]
image: golang:1.26
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
    - postgres:18
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
	if workflow.Jobs[0].Image != "golang:1.26" {
		t.Fatalf("build image = %q, want golang:1.26", workflow.Jobs[0].Image)
	}
	if got := workflow.Jobs[1].Needs; len(got) != 1 || got[0] != "build" {
		t.Fatalf("test needs = %v, want [build]", got)
	}
	if workflow.Jobs[1].Support != model.SupportSupported {
		t.Fatalf("test support = %s, want supported services", workflow.Jobs[1].Support)
	}
	if workflow.Jobs[2].If == "" {
		t.Fatal("expected deploy rules to be captured as condition text")
	}
	if !hasFeature(workflow.Features, "gitlab.include-local") {
		t.Fatal("expected local include feature reference")
	}
}

func hasFeature(features []model.FeatureRef, id string) bool {
	for _, feature := range features {
		if feature.ID == id {
			return true
		}
	}
	return false
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

func TestUnsupportedGitLabPoliciesAndRemoteIncludesAreExplicit(t *testing.T) {
	workflow, err := parseWorkflow(".gitlab-ci.yml", []byte(`
include:
  - remote: https://example.invalid/ci.yml
test:
  script: echo test
  retry: 2
  timeout: 10m
`))
	if err != nil {
		t.Fatal(err)
	}
	if !hasFeature(workflow.Features, "gitlab.include-remote") {
		t.Fatal("remote include must be explicit")
	}
	if !hasFeature(workflow.Jobs[0].Features, "gitlab.unsupported-job-policy") {
		t.Fatal("ignored job policies must be explicit")
	}
}

func TestGitLabGlobalDefaultsAndEmptyJobsCannotSilentlySucceed(t *testing.T) {
	workflow, err := parseWorkflow(".gitlab-ci.yml", []byte(`
cache:
  paths: [.cache]
services:
  - redis:8
EMPTY_GLOBAL: value
empty:
  stage: test
`))
	if err != nil {
		t.Fatal(err)
	}
	if !hasFeature(workflow.Features, "gitlab.unsupported-global-policy") {
		t.Fatal("top-level cache/services must be explicitly rejected")
	}
	if !hasFeature(workflow.Features, "gitlab.unknown") {
		t.Fatal("unknown non-job top-level key must be explicit")
	}
	if len(workflow.Jobs) != 1 || !hasFeature(workflow.Jobs[0].Features, "common.empty-job") {
		t.Fatalf("empty job was not classified: %#v", workflow.Jobs)
	}
}
