package docker

import (
	"context"
	"reflect"
	"testing"

	"github.com/7samael7/Piper/engine/internal/actions"
	"github.com/7samael7/Piper/engine/internal/expression"
	"github.com/7samael7/Piper/engine/internal/logs"
	"github.com/7samael7/Piper/engine/internal/pipeline/model"
	"github.com/7samael7/Piper/engine/internal/scheduler"
	"github.com/7samael7/Piper/engine/internal/secrets"
	"github.com/7samael7/Piper/engine/internal/support"
)

func TestRuntimeGuardRejectsUnsupportedFeature(t *testing.T) {
	feature, rejected, err := rejectingFeature([]model.FeatureRef{{ID: "common.empty-step", Path: "jobs.test.steps[0]"}})
	if err != nil {
		t.Fatal(err)
	}
	if !rejected || feature.RuntimeDisposition != model.RuntimeReject {
		t.Fatalf("feature=%#v rejected=%v", feature, rejected)
	}
}

func TestRuntimeGuardMatchesEveryRegistryDisposition(t *testing.T) {
	for _, entry := range support.MustDefault().Features {
		_, rejected, err := rejectingFeature([]model.FeatureRef{{ID: entry.ID}})
		if err != nil {
			t.Fatalf("%s: %v", entry.ID, err)
		}
		wantRejected := entry.RuntimeDisposition == model.RuntimeReject
		if rejected != wantRejected {
			t.Fatalf("%s disposition=%s rejected=%v", entry.ID, entry.RuntimeDisposition, rejected)
		}
	}
}

func TestRuntimeGuardDoesNotRejectValidationOnlyOrConsent(t *testing.T) {
	for _, id := range []string{"github.runner", "github.remote-action"} {
		if _, rejected, err := rejectingFeature([]model.FeatureRef{{ID: id}}); err != nil || rejected {
			t.Fatalf("%s rejected=%v err=%v", id, rejected, err)
		}
	}
}

func TestExecuteJobRejectsUnsupportedFeaturesBeforeConditionsOrDocker(t *testing.T) {
	tests := []struct {
		name string
		job  model.Job
	}{
		{
			name: "job",
			job: model.Job{
				ID: "empty", Name: "empty",
				Condition: &model.ConditionSpec{Provider: model.ProviderGitHub, Original: "false"},
				Features:  []model.FeatureRef{{ID: "common.empty-job", Path: "jobs.empty"}},
			},
		},
		{
			name: "step",
			job: model.Job{
				ID: "invalid", Name: "invalid",
				Steps: []model.Step{{
					Name:      "invalid",
					Condition: &model.ConditionSpec{Provider: model.ProviderGitHub, Original: "false"},
					Features:  []model.FeatureRef{{ID: "common.empty-step", Path: "jobs.invalid.steps[0]"}},
				}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events := []logs.Event{}
			result := (&Executor{}).executeJob(
				context.Background(), nil,
				model.RunRequest{Provider: model.ProviderGitHub},
				model.JobInstance{ID: test.job.ID, Name: test.job.Name, Job: test.job},
				nil, func(event logs.Event) { events = append(events, event) }, secrets.NewMasker(nil),
			)
			if result.Status != model.RunFailed || result.Error == nil {
				t.Fatalf("unexpected result: %#v", result)
			}
			if len(events) != 1 || events[0].Type != "support_error" {
				t.Fatalf("unsupported feature was not rejected first: %#v", events)
			}
		})
	}
}

func TestCheckoutUsesEmulationEvent(t *testing.T) {
	event := emulationEvent("job", "step", "github.checkout", "emulated")
	if event.Type != "step_emulated" || event.Status != model.RunSucceeded {
		t.Fatalf("unexpected event: %#v", event)
	}
	if event.Data["featureId"] != "github.checkout" {
		t.Fatalf("unexpected data: %#v", event.Data)
	}
	for _, key := range []string{"feature", "category", "documentation"} {
		if event.Data[key] == nil || event.Data[key] == "" {
			t.Fatalf("missing %s in %#v", key, event.Data)
		}
	}
}

func TestSetupActionFeatureMatchesProvider(t *testing.T) {
	if got := setupActionFeatureID(model.ProviderGitHub); got != "github.setup-runtime" {
		t.Fatalf("GitHub feature = %s", got)
	}
	if got := setupActionFeatureID(model.ProviderAzure); got != "azure.task-runtime" {
		t.Fatalf("Azure feature = %s", got)
	}
}

func TestBashCommandPreservesImageEnvironment(t *testing.T) {
	got := bashCommand("go version")
	want := []string{"/bin/bash", "--noprofile", "--norc", "-eo", "pipefail", "-c", "go version"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bash command = %#v, want %#v", got, want)
	}
}

func TestWorkflowRunContextDefaultsToSuccessfulCurrentRevision(t *testing.T) {
	event := githubEventContext(model.RunRequest{EventName: "workflow_run"}, "master", "abc123")
	workflowRun, ok := event["workflow_run"].(map[string]interface{})
	if !ok {
		t.Fatalf("workflow_run context = %#v", event["workflow_run"])
	}
	if workflowRun["conclusion"] != "success" || workflowRun["head_sha"] != "abc123" || workflowRun["head_branch"] != "master" {
		t.Fatalf("unexpected workflow_run context: %#v", workflowRun)
	}
}

func TestSkippedDependencyMakesSuccessConditionFalse(t *testing.T) {
	runtime := newRuntimeContext(
		model.RunRequest{Provider: model.ProviderGitHub},
		model.JobInstance{Job: model.Job{}},
		map[string]scheduler.Result{"prepare": {Status: model.RunSkipped}},
	)
	result := expression.Evaluate(defaultJobCondition(model.ProviderGitHub, runtime.dependencies), runtime.expressionContext(runtime.status))
	if result.Error != nil || result.Value {
		t.Fatalf("unexpected condition result: %#v", result)
	}
}

func TestMissingGitHubSecretResolvesToEmptyJobEnvironment(t *testing.T) {
	runtime := newRuntimeContext(
		model.RunRequest{Provider: model.ProviderGitHub},
		model.JobInstance{Job: model.Job{Env: map[string]string{
			"APPLE_CERTIFICATE": "${{ secrets.APPLE_CERTIFICATE }}",
		}}},
		nil,
	)
	env := runtime.base["env"].(map[string]interface{})
	if env["APPLE_CERTIFICATE"] != "" {
		t.Fatalf("APPLE_CERTIFICATE = %#v", env["APPLE_CERTIFICATE"])
	}
}

func TestSupportedActionRuntimeRejectsUnknownNodeAndDockerfileActions(t *testing.T) {
	for _, action := range []*actions.Action{
		nil,
		{Using: "node999"},
		{Using: "docker", Image: "Dockerfile"},
		{Using: "python"},
	} {
		if supportedActionRuntime(action) {
			t.Fatalf("unexpected supported action: %#v", action)
		}
	}
	for _, action := range []*actions.Action{
		{Using: "node20"},
		{Using: "node24"},
		{Using: "composite"},
		{Using: "docker", Image: "docker://alpine:3.20"},
	} {
		if !supportedActionRuntime(action) {
			t.Fatalf("unexpected unsupported action: %#v", action)
		}
	}
}
