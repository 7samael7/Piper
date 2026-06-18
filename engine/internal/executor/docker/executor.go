package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/7samael7/Piper/engine/internal/actions"
	"github.com/7samael7/Piper/engine/internal/artifacts"
	"github.com/7samael7/Piper/engine/internal/caches"
	"github.com/7samael7/Piper/engine/internal/expression"
	"github.com/7samael7/Piper/engine/internal/logs"
	"github.com/7samael7/Piper/engine/internal/pipeline/model"
	"github.com/7samael7/Piper/engine/internal/pipeline/plan"
	"github.com/7samael7/Piper/engine/internal/scheduler"
	"github.com/7samael7/Piper/engine/internal/secrets"
	"github.com/7samael7/Piper/engine/internal/support"
	"github.com/7samael7/Piper/engine/internal/workspace"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/uuid"
)

type Executor struct {
	image     string
	artifacts *artifacts.Store
	caches    *caches.Store
	actions   *actions.Resolver
}

func NewExecutor() *Executor {
	artifactStore, _ := artifacts.OpenDefault()
	cacheStore, _ := caches.OpenDefault()
	actionResolver, _ := actions.OpenDefault()
	return &Executor{image: "ubuntu:22.04", artifacts: artifactStore, caches: cacheStore, actions: actionResolver}
}

func (e *Executor) Artifacts() *artifacts.Store { return e.artifacts }
func (e *Executor) Caches() *caches.Store       { return e.caches }

func (e *Executor) Execute(ctx context.Context, request model.RunRequest, workflow *model.Workflow, emit logs.Emitter) error {
	masker := secrets.NewMasker(request.Secrets)
	emit(systemEvent("", "", "run_started", model.RunRunning, "Local run started."))
	emit(systemEvent("", "", "support_notice", "", "Local Docker execution is an MVP and does not exactly match hosted CI runners."))
	if request.MockOIDC {
		emit(systemEvent("", "", "security_warning", model.RunRunning, "Mock OIDC is enabled. Piper exposes only a clearly marked local test token, never a provider-issued identity."))
	}
	if feature, ok, err := rejectingFeature(workflow.Features); err != nil {
		return err
	} else if ok {
		emitSupportError(emit, "", "", feature)
		return fmt.Errorf("%s: %s", feature.FeatureID, feature.Fallback)
	}

	cli, err := connectDocker(ctx)
	if err != nil {
		return err
	}
	defer cli.Close()

	executionPlan, err := plan.Compile(workflow, request.MaxExpandedJobs, request.Concurrency)
	if err != nil {
		return err
	}
	jobs, err := selectedInstances(executionPlan, request.JobID)
	if err != nil {
		return err
	}

	results, scheduleErr := scheduler.Execute(ctx, jobs, executionPlan.MaxConcurrency,
		func(jobCtx context.Context, instance model.JobInstance, dependencies map[string]scheduler.Result) scheduler.Result {
			if request.JobTimeoutSeconds > 0 {
				var cancel context.CancelFunc
				jobCtx, cancel = context.WithTimeout(jobCtx, time.Duration(request.JobTimeoutSeconds)*time.Second)
				defer cancel()
			}
			result := e.executeJob(jobCtx, cli, request, instance, dependencies, emit, masker)
			if result.Status == model.RunFailed && instance.Job.AllowFailure {
				emit(systemEvent(instance.ID, "", "job_failure_allowed", model.RunSucceeded, "Job failure was allowed by the provider configuration."))
				result.Status = model.RunSucceeded
				result.Error = nil
			}
			return result
		},
		func(instance model.JobInstance, status model.RunStatus) {
			emit(systemEvent(instance.ID, "", "job_status", status, fmt.Sprintf("Job %s is %s.", instance.Name, status)))
		},
	)
	if scheduleErr != nil {
		return scheduleErr
	}
	failed := []string{}
	for id, result := range results {
		if result.Status == model.RunFailed {
			failed = append(failed, id)
		}
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		return fmt.Errorf("%d job(s) failed: %s", len(failed), strings.Join(failed, ", "))
	}
	emit(systemEvent("", "", "run_finished", model.RunSucceeded, "Local run completed."))
	return nil
}

func (e *Executor) pullImage(ctx context.Context, cli *client.Client, imageName string, emit logs.Emitter, masker secrets.Masker) error {
	emit(systemEvent("", "", "image_pull", "", fmt.Sprintf("Ensuring Docker image %s is available.", imageName)))
	reader, err := cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull Docker image %s: %w", imageName, err)
	}
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := masker.Mask(scanner.Text())
		if strings.TrimSpace(line) != "" {
			emit(logs.Event{
				Time:    logs.New("", "image_pull", "").Time,
				Type:    "image_pull",
				Stream:  "system",
				Message: line,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read Docker pull output: %w", err)
	}
	return nil
}

func (e *Executor) executeJob(ctx context.Context, cli *client.Client, request model.RunRequest, instance model.JobInstance, dependencies map[string]scheduler.Result, emit logs.Emitter, masker secrets.Masker) scheduler.Result {
	job := instance.Job
	job.ID = instance.ID
	job.Name = instance.Name
	runtime := newRuntimeContext(request, instance, dependencies)
	condition := defaultJobCondition(request.Provider, dependencies)
	if job.Condition != nil {
		condition = *job.Condition
	}
	conditionResult := expression.Evaluate(condition, runtime.expressionContext(runtime.status))
	emit(conditionEvent(job.ID, "", conditionResult))
	if conditionResult.Error != nil {
		emit(systemEvent(job.ID, "", "condition_evaluation_error", model.RunFailed, conditionResult.Error.Message))
		return scheduler.Result{Status: model.RunFailed, Error: fmt.Errorf("job condition: %s", conditionResult.Error.Message)}
	}
	if !conditionResult.Value {
		emit(systemEvent(job.ID, "", "job_skipped", model.RunSkipped, conditionResult.Reason))
		return scheduler.Result{Status: model.RunSkipped}
	}
	if feature, ok, err := rejectingFeature(job.Features); err != nil {
		return scheduler.Result{Status: model.RunFailed, Error: err}
	} else if ok {
		emitSupportError(emit, job.ID, "", feature)
		return scheduler.Result{Status: model.RunFailed, Error: fmt.Errorf("%s: %s", feature.FeatureID, feature.Fallback)}
	}
	approvalTarget := job.Environment
	if approvalTarget == "" && job.When == "manual" {
		approvalTarget = "manual job " + job.Name
	}
	if approvalTarget != "" {
		emit(systemEvent(job.ID, "", "approval_required", model.RunQueued, fmt.Sprintf("%s requires approval.", approvalTarget)))
		if request.WaitForApproval == nil {
			return scheduler.Result{Status: model.RunFailed, Error: fmt.Errorf("%s requires explicit approval", approvalTarget)}
		}
		approved, approvalErr := request.WaitForApproval(ctx, approvalTarget)
		if approvalErr != nil {
			return scheduler.Result{Status: model.RunCancelled, Error: approvalErr}
		}
		if !approved {
			emit(systemEvent(job.ID, "", "job_skipped", model.RunSkipped, fmt.Sprintf("%s was rejected.", approvalTarget)))
			return scheduler.Result{Status: model.RunSkipped}
		}
		emit(systemEvent(job.ID, "", "approval_granted", model.RunRunning, fmt.Sprintf("%s was approved.", approvalTarget)))
	}

	emit(systemEvent(job.ID, "", "job_started", model.RunRunning, fmt.Sprintf("Job %s started.", job.Name)))
	if job.ReusableWorkflow != "" {
		return scheduler.Result{Status: model.RunFailed, Error: fmt.Errorf("job %s uses reusable workflow %s, which is unsupported locally", job.ID, job.ReusableWorkflow)}
	}
	if job.HasContainer {
		return scheduler.Result{Status: model.RunFailed, Error: fmt.Errorf("job %s declares container, which is unsupported locally", job.ID)}
	}
	if job.HasStrategy && job.Matrix == nil {
		return scheduler.Result{Status: model.RunFailed, Error: fmt.Errorf("job %s declares an unsupported strategy", job.ID)}
	}
	imageName, err := resolveJobImage(e.image, job)
	if err != nil {
		return scheduler.Result{Status: model.RunFailed, Error: err}
	}
	if err := e.pullImage(ctx, cli, imageName, emit, masker); err != nil {
		return scheduler.Result{Status: model.RunFailed, Error: err}
	}
	prepared, err := workspace.Prepare(request.RepoPath, request.WorkspaceMode)
	if err != nil {
		return scheduler.Result{Status: model.RunFailed, Error: err}
	}
	defer prepared.Cleanup()
	cacheScope := request.RepoPath + ":" + request.WorkflowPath
	if err := e.restoreJobCaches(job, cacheScope, prepared.Path, runtime, emit); err != nil {
		return scheduler.Result{Status: model.RunFailed, Error: err}
	}
	resolvedActions, actionErr := e.resolveActions(ctx, request, job, prepared.Path, emit)
	if actionErr != nil {
		return scheduler.Result{Status: model.RunFailed, Error: actionErr}
	}
	internalNetwork := request.NetworkAccess == "disabled" || request.NetworkAccess == "internal" || job.Environment != ""
	networkState, err := createJobNetwork(ctx, cli, job.ID, internalNetwork)
	if err != nil {
		return scheduler.Result{Status: model.RunFailed, Error: err}
	}
	defer networkState.cleanup(cli)
	if err := startServices(ctx, cli, networkState, job.Services, emit, masker); err != nil {
		return scheduler.Result{Status: model.RunFailed, Error: err}
	}
	actionRoot := ""
	if e.actions != nil {
		actionRoot = e.actions.CacheRoot()
	}
	containerID, err := createJobContainer(ctx, cli, imageName, prepared.Path, prepared.ReadOnly, networkState.name, actionRoot, request)
	if err != nil {
		return scheduler.Result{Status: model.RunFailed, Error: err}
	}
	networkState.containers = append(networkState.containers, containerID)

	for index, step := range job.Steps {
		stepID := step.ID
		if stepID == "" {
			stepID = fmt.Sprintf("step-%d", index+1)
		}
		if err := ctx.Err(); err != nil {
			return scheduler.Result{Status: model.RunCancelled, Error: err}
		}
		stepCondition := defaultStepCondition(request.Provider)
		if step.Condition != nil {
			stepCondition = *step.Condition
		}
		conditionResult := expression.Evaluate(stepCondition, runtime.expressionContext(runtime.status))
		emit(conditionEvent(job.ID, stepID, conditionResult))
		if conditionResult.Error != nil {
			emit(systemEvent(job.ID, stepID, "condition_evaluation_error", model.RunFailed, conditionResult.Error.Message))
			return scheduler.Result{Status: model.RunFailed, Error: fmt.Errorf("step %s condition: %s", step.Name, conditionResult.Error.Message)}
		}
		if !conditionResult.Value {
			emit(systemEvent(job.ID, stepID, "step_skipped", model.RunSkipped, conditionResult.Reason))
			runtime.steps[stepID] = stepRuntime{Status: model.RunSkipped}
			continue
		}
		if feature, ok, guardErr := rejectingFeature(step.Features); guardErr != nil {
			return scheduler.Result{Status: model.RunFailed, Error: guardErr}
		} else if ok {
			emitSupportError(emit, job.ID, stepID, feature)
			return scheduler.Result{Status: model.RunFailed, Error: fmt.Errorf("%s: %s", feature.FeatureID, feature.Fallback)}
		}
		emit(systemEvent(job.ID, stepID, "step_started", model.RunRunning, fmt.Sprintf("Step %s started.", step.Name)))

		switch {
		case step.Run != "":
			stepCtx := ctx
			cancelStep := func() {}
			if request.StepTimeoutSeconds > 0 {
				stepCtx, cancelStep = context.WithTimeout(ctx, time.Duration(request.StepTimeoutSeconds)*time.Second)
			}
			outputs, stepErr := e.executeShellStep(stepCtx, cli, containerID, request, job, step, stepID, runtime, emit, masker)
			cancelStep()
			if stepErr != nil {
				emit(systemEvent(job.ID, stepID, "step_failed", model.RunFailed, stepErr.Error()))
				runtime.steps[stepID] = stepRuntime{Status: model.RunFailed, Outputs: outputs}
				runtime.status = model.RunFailed
				if step.ContinueOnError {
					emit(systemEvent(job.ID, stepID, "step_continued", model.RunSucceeded, "Step failure was allowed."))
					runtime.status = model.RunSucceeded
					continue
				}
				return scheduler.Result{Status: model.RunFailed, Error: stepErr}
			}
			runtime.steps[stepID] = stepRuntime{Status: model.RunSucceeded, Outputs: outputs}
			emit(systemEvent(job.ID, stepID, "step_finished", model.RunSucceeded, fmt.Sprintf("Step %s finished.", step.Name)))
		case strings.HasPrefix(step.Uses, "actions/checkout@"):
			emit(emulationEvent(job.ID, stepID, "github.checkout", "actions/checkout is emulated because the prepared repository is already mounted."))
		case request.Provider == model.ProviderAzure && step.Uses == "checkout":
			emit(emulationEvent(job.ID, stepID, "azure.checkout", "Azure checkout is emulated because the prepared repository is already mounted."))
		case request.Provider == model.ProviderGitHub && isSetupAction(step.Uses):
			emit(systemEvent(job.ID, stepID, "step_finished", model.RunSucceeded, setupActionMessage(step, imageName)))
		case e.isBuiltinAction(request.Provider, step.Uses):
			if actionErr := e.executeBuiltinAction(request, job, step, prepared.Path, cacheScope, runtime, emit); actionErr != nil {
				emit(systemEvent(job.ID, stepID, "step_failed", model.RunFailed, actionErr.Error()))
				if step.ContinueOnError {
					continue
				}
				return scheduler.Result{Status: model.RunFailed, Error: actionErr}
			}
			emit(systemEvent(job.ID, stepID, "step_finished", model.RunSucceeded, fmt.Sprintf("%s completed using local emulation.", step.Uses)))
		case resolvedActions[step.Uses] != nil:
			outputs, actionErr := e.executeResolvedAction(ctx, cli, containerID, request, job, step, stepID, resolvedActions[step.Uses], runtime, emit, masker)
			if actionErr != nil {
				emit(systemEvent(job.ID, stepID, "step_failed", model.RunFailed, actionErr.Error()))
				if step.ContinueOnError {
					continue
				}
				return scheduler.Result{Status: model.RunFailed, Error: actionErr}
			}
			runtime.steps[stepID] = stepRuntime{Status: model.RunSucceeded, Outputs: outputs}
			emit(systemEvent(job.ID, stepID, "step_finished", model.RunSucceeded, fmt.Sprintf("Action %s completed.", step.Uses)))
		case step.Uses != "":
			message := fmt.Sprintf("Action or task %s is unsupported locally.", step.Uses)
			emit(systemEvent(job.ID, stepID, "step_unsupported", model.RunFailed, message))
			return scheduler.Result{Status: model.RunFailed, Error: fmt.Errorf("%s", message)}
		default:
			message := "Step has no supported executable form."
			emit(systemEvent(job.ID, stepID, "support_error", model.RunFailed, message))
			return scheduler.Result{Status: model.RunFailed, Error: fmt.Errorf("%s", message)}
		}
	}

	if err := e.saveJobCaches(job, cacheScope, prepared.Path, runtime, emit); err != nil {
		return scheduler.Result{Status: model.RunFailed, Error: err}
	}
	if err := e.publishJobArtifacts(request, job, prepared.Path, emit); err != nil {
		return scheduler.Result{Status: model.RunFailed, Error: err}
	}
	emit(systemEvent(job.ID, "", "job_finished", model.RunSucceeded, fmt.Sprintf("Job %s finished.", job.Name)))
	outputs := evaluateJobOutputs(job.Outputs, runtime)
	return scheduler.Result{Status: model.RunSucceeded, Outputs: outputs}
}

func createJobContainer(ctx context.Context, cli *client.Client, imageName, repoPath string, readOnly bool, networkName, actionRoot string, request model.RunRequest) (string, error) {
	mounts := []mount.Mount{{
		Type: mount.TypeBind, Source: repoPath, Target: "/workspace", ReadOnly: readOnly,
	}}
	if actionRoot != "" {
		mounts = append(mounts, mount.Mount{
			Type: mount.TypeBind, Source: actionRoot, Target: "/piper-actions", ReadOnly: true,
		})
	}
	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image:      imageName,
		Cmd:        []string{"sleep", "infinity"},
		WorkingDir: "/workspace",
		Tty:        false,
	}, &container.HostConfig{
		AutoRemove: false,
		Mounts:     mounts,
		Resources: container.Resources{
			Memory:    request.MemoryMB * 1024 * 1024,
			NanoCPUs:  int64(request.CPUs * 1_000_000_000),
			PidsLimit: optionalInt64(request.PidsLimit),
		},
	}, &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{
		networkName: {},
	}}, nil, "piper-"+uuid.NewString())
	if err != nil {
		return "", fmt.Errorf("create job container: %w", err)
	}
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = cli.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
		return "", fmt.Errorf("start job container: %w", err)
	}
	return resp.ID, nil
}

func optionalInt64(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func (e *Executor) executeShellStep(ctx context.Context, cli *client.Client, containerID string, request model.RunRequest, job model.Job, step model.Step, stepID string, runtime *runtimeContext, emit logs.Emitter, masker secrets.Masker) (map[string]string, error) {
	workdir := "/workspace"
	if step.WorkingDirectory != "" {
		interpolated, evalErr := expression.Interpolate(request.Provider, step.WorkingDirectory, runtime.expressionContext(runtime.status))
		if evalErr != nil {
			return nil, fmt.Errorf("interpolate working directory: %s", evalErr.Message)
		}
		if strings.HasPrefix(interpolated, "/piper-actions/") {
			workdir = path.Clean(interpolated)
		} else {
			clean := path.Clean("/" + interpolated)
			workdir = path.Join("/workspace", strings.TrimPrefix(clean, "/"))
		}
	}

	command, evalErr := expression.Interpolate(request.Provider, step.Run, runtime.expressionContext(runtime.status))
	if evalErr != nil {
		return nil, fmt.Errorf("interpolate command: %s", evalErr.Message)
	}
	outputFile := "/tmp/piper-output-" + envKey(stepID)
	env, envErr := buildEnvWithContext(request, job, step, runtime, outputFile)
	if envErr != nil {
		return nil, envErr
	}
	shell := []string{"/bin/bash", "-lc", command}
	if step.Shell == "pwsh" || step.Shell == "powershell" {
		shell = []string{"pwsh", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command}
	}
	execResponse, err := cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		Cmd:          shell,
		WorkingDir:   workdir,
		Env:          env,
		Tty:          false,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create step process: %w", err)
	}

	attach, err := cli.ContainerExecAttach(ctx, execResponse.ID, container.ExecAttachOptions{Tty: false})
	if err != nil {
		return nil, fmt.Errorf("attach step process: %w", err)
	}
	defer attach.Close()

	stdout := newEventWriter(emit, masker, logs.Event{
		Type:    "step_log",
		JobID:   job.ID,
		StepID:  stepID,
		Stream:  "stdout",
		Message: "",
	})
	stderr := newEventWriter(emit, masker, logs.Event{
		Type:    "step_log",
		JobID:   job.ID,
		StepID:  stepID,
		Stream:  "stderr",
		Message: "",
	})

	copyDone := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(stdout, stderr, attach.Reader)
		stdout.Flush()
		stderr.Flush()
		copyDone <- err
	}()

	select {
	case <-ctx.Done():
		_ = cli.ContainerKill(context.Background(), containerID, "SIGKILL")
		<-copyDone
		return nil, ctx.Err()
	case err := <-copyDone:
		if err != nil {
			return nil, fmt.Errorf("read step output: %w", err)
		}
	}

	inspection, err := cli.ContainerExecInspect(ctx, execResponse.ID)
	if err != nil {
		return nil, fmt.Errorf("inspect step process: %w", err)
	}
	outputs, outputErr := readStepOutputs(ctx, cli, containerID, outputFile)
	if outputErr != nil {
		return nil, outputErr
	}
	if inspection.ExitCode != 0 {
		return outputs, fmt.Errorf("step exited with status %d", inspection.ExitCode)
	}
	return outputs, nil
}

func selectedInstances(executionPlan *model.ExecutionPlan, jobID string) ([]model.JobInstance, error) {
	if executionPlan == nil {
		return nil, fmt.Errorf("execution plan is unavailable")
	}
	if jobID != "" {
		selected := []model.JobInstance{}
		for _, instance := range executionPlan.Jobs {
			if instance.ID == jobID || instance.LogicalJobID == jobID {
				instance.Needs = nil
				instance.Job.Needs = nil
				selected = append(selected, instance)
			}
		}
		if len(selected) > 0 {
			return selected, nil
		}
		return nil, fmt.Errorf("job %q was not found", jobID)
	}
	return executionPlan.Jobs, nil
}

func buildEnv(request model.RunRequest, job model.Job, step model.Step) []string {
	values := map[string]string{
		"CI":             "true",
		"PIPER_PROVIDER": string(request.Provider),
		"PIPER_EVENT":    request.EventName,
	}
	if jobUsesAction(job, setupDotnetAction) {
		values["DOTNET_ROLL_FORWARD"] = "Major"
	}
	switch request.Provider {
	case model.ProviderGitLab:
		values["GITLAB_CI"] = "false"
		values["CI_PROJECT_DIR"] = "/workspace"
		values["CI_PIPELINE_SOURCE"] = request.EventName
	case model.ProviderAzure:
		values["TF_BUILD"] = "false"
		values["BUILD_SOURCESDIRECTORY"] = "/workspace"
		values["BUILD_REASON"] = request.EventName
	default:
		values["GITHUB_ACTIONS"] = "false"
		values["GITHUB_WORKSPACE"] = "/workspace"
		values["GITHUB_EVENT_NAME"] = request.EventName
	}
	for key, value := range request.Inputs {
		values["INPUT_"+envKey(key)] = value
	}
	for key, value := range request.Env {
		values[key] = value
	}
	for key, value := range request.Secrets {
		values[key] = value
	}
	if request.MockOIDC {
		values["PIPER_MOCK_OIDC_TOKEN"] = mockOIDCToken(request.MockOIDCClaims)
	}
	for key, value := range job.Env {
		values[key] = value
	}
	for key, value := range step.Env {
		values[key] = value
	}
	env := make([]string, 0, len(values))
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}

func buildEnvWithContext(request model.RunRequest, job model.Job, step model.Step, runtime *runtimeContext, outputFile string) ([]string, error) {
	values := map[string]string{}
	for _, entry := range buildEnv(request, job, model.Step{}) {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	for key, value := range step.Env {
		interpolated, err := expression.Interpolate(request.Provider, value, runtime.expressionContext(runtime.status))
		if err != nil {
			return nil, fmt.Errorf("interpolate environment %s: %s", key, err.Message)
		}
		values[key] = interpolated
	}
	if request.Provider == model.ProviderAzure {
		for key, value := range runtime.instance.Matrix {
			values[key] = fmt.Sprint(value)
		}
	}
	values["GITHUB_OUTPUT"] = outputFile
	values["GITHUB_ENV"] = "/tmp/piper-env"
	env := make([]string, 0, len(values))
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	sort.Strings(env)
	return env, nil
}

type stepRuntime struct {
	Status  model.RunStatus
	Outputs map[string]string
}

type runtimeContext struct {
	request      model.RunRequest
	instance     model.JobInstance
	dependencies map[string]scheduler.Result
	steps        map[string]stepRuntime
	status       model.RunStatus
	base         map[string]interface{}
	actionCaches []model.CacheSpec
}

func newRuntimeContext(request model.RunRequest, instance model.JobInstance, dependencies map[string]scheduler.Result) *runtimeContext {
	status := model.RunSucceeded
	for _, dependency := range dependencies {
		if dependency.Status == model.RunCancelled {
			status = model.RunCancelled
			break
		}
		if dependency.Status == model.RunFailed {
			status = model.RunFailed
		}
	}
	env := map[string]interface{}{}
	for key, value := range request.Env {
		env[key] = value
	}
	for key, value := range request.Secrets {
		env[key] = value
	}
	for key, value := range instance.Job.Env {
		env[key] = value
	}
	if request.Provider == model.ProviderAzure {
		for key, value := range instance.Matrix {
			env[key] = fmt.Sprint(value)
		}
	}
	branch, sha, tag := gitMetadata(request.RepoPath)
	switch request.Provider {
	case model.ProviderGitLab:
		env["CI_PIPELINE_SOURCE"] = request.EventName
		env["CI_COMMIT_BRANCH"] = branch
		env["CI_COMMIT_TAG"] = tag
		if tag != "" {
			env["CI_COMMIT_REF_NAME"] = tag
		} else {
			env["CI_COMMIT_REF_NAME"] = branch
		}
	case model.ProviderAzure:
		env["BUILD_REASON"] = request.EventName
		env["BUILD_SOURCEBRANCH"] = branch
	default:
		env["GITHUB_EVENT_NAME"] = request.EventName
	}
	inputs := map[string]interface{}{}
	for key, value := range request.Inputs {
		inputs[key] = value
	}
	base := map[string]interface{}{
		"env":    env,
		"inputs": inputs,
		"matrix": instance.Matrix,
		"github": map[string]interface{}{
			"event_name": request.EventName,
			"ref":        gitRef(branch, tag),
			"ref_name":   firstNonEmpty(tag, branch),
			"sha":        sha,
			"workspace":  "/workspace",
		},
		"runner":    map[string]interface{}{"os": "Linux", "name": "Piper"},
		"variables": env,
		"changed":   changedFiles(request.RepoPath, request.BaseRef),
	}
	return &runtimeContext{
		request: request, instance: instance, dependencies: dependencies,
		steps: map[string]stepRuntime{}, status: status, base: base,
	}
}

func (r *runtimeContext) expressionContext(status model.RunStatus) expression.Context {
	values := make(map[string]interface{}, len(r.base)+4)
	for key, value := range r.base {
		values[key] = value
	}
	steps := map[string]interface{}{}
	for id, step := range r.steps {
		steps[id] = map[string]interface{}{"result": step.Status, "outputs": stringInterfaceMap(step.Outputs)}
	}
	needs := map[string]interface{}{}
	for id, dependency := range r.dependencies {
		needs[id] = map[string]interface{}{"result": dependency.Status, "outputs": stringInterfaceMap(dependency.Outputs)}
	}
	logicalNeeds := map[string][]scheduler.Result{}
	for _, dependency := range r.dependencies {
		logicalNeeds[dependency.LogicalJobID] = append(logicalNeeds[dependency.LogicalJobID], dependency)
	}
	for logicalID, dependencies := range logicalNeeds {
		if logicalID == "" || len(dependencies) == 1 {
			continue
		}
		sort.Slice(dependencies, func(i, j int) bool {
			return fmt.Sprint(dependencies[i].Outputs) < fmt.Sprint(dependencies[j].Outputs)
		})
		status := model.RunSucceeded
		outputs := map[string]string{}
		for _, dependency := range dependencies {
			if dependency.Status == model.RunFailed {
				status = model.RunFailed
			} else if dependency.Status == model.RunCancelled && status != model.RunFailed {
				status = model.RunCancelled
			}
			for key, value := range dependency.Outputs {
				if _, exists := outputs[key]; !exists {
					outputs[key] = value
				}
			}
		}
		needs[logicalID] = map[string]interface{}{"result": status, "outputs": stringInterfaceMap(outputs)}
	}
	values["steps"] = steps
	values["needs"] = needs
	values["job"] = map[string]interface{}{"status": status}
	return expression.Context{Values: values, Status: status}
}

func defaultJobCondition(provider model.ProviderID, dependencies map[string]scheduler.Result) model.ConditionSpec {
	original := "success()"
	if provider == model.ProviderAzure {
		original = "succeeded()"
	}
	if provider == model.ProviderGitLab {
		original = "true"
	}
	return model.ConditionSpec{Provider: provider, Original: original, Kind: "default"}
}

func defaultStepCondition(provider model.ProviderID) model.ConditionSpec {
	original := "success()"
	if provider == model.ProviderAzure {
		original = "succeeded()"
	}
	if provider == model.ProviderGitLab {
		original = "true"
	}
	return model.ConditionSpec{Provider: provider, Original: original, Kind: "default"}
}

func conditionEvent(jobID, stepID string, result model.ConditionResult) logs.Event {
	status := model.RunSkipped
	if result.Value {
		status = model.RunSucceeded
	}
	event := systemEvent(jobID, stepID, "condition_evaluated", status, result.Reason)
	event.Data = map[string]interface{}{
		"expression": result.Expression,
		"evaluated":  result.Evaluated,
		"value":      result.Value,
		"reason":     result.Reason,
	}
	if result.Error != nil {
		event.Data["error"] = result.Error
		event.Status = model.RunFailed
	}
	return event
}

func evaluateJobOutputs(definitions map[string]string, runtime *runtimeContext) map[string]string {
	if len(definitions) == 0 {
		return nil
	}
	outputs := map[string]string{}
	for key, value := range definitions {
		interpolated, err := expression.Interpolate(runtime.request.Provider, value, runtime.expressionContext(runtime.status))
		if err == nil {
			outputs[key] = interpolated
		}
	}
	return outputs
}

func readStepOutputs(ctx context.Context, cli *client.Client, containerID, outputFile string) (map[string]string, error) {
	response, err := cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		Cmd:          []string{"/bin/bash", "-lc", "if [ -f " + outputFile + " ]; then cat " + outputFile + "; fi"},
		AttachStdout: true, AttachStderr: true,
	})
	if err != nil {
		return nil, fmt.Errorf("prepare step outputs: %w", err)
	}
	attach, err := cli.ContainerExecAttach(ctx, response.ID, container.ExecAttachOptions{})
	if err != nil {
		return nil, fmt.Errorf("read step outputs: %w", err)
	}
	defer attach.Close()
	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, attach.Reader); err != nil {
		return nil, fmt.Errorf("read step outputs: %w", err)
	}
	outputs := parseOutputText(stdout.String())
	if len(outputs) == 0 {
		return nil, nil
	}
	return outputs, nil
}

func parseOutputText(value string) map[string]string {
	outputs := map[string]string{}
	lines := strings.Split(value, "\n")
	for index := 0; index < len(lines); index++ {
		line := strings.TrimSpace(lines[index])
		if key, delimiter, ok := strings.Cut(line, "<<"); ok && key != "" && delimiter != "" {
			values := []string{}
			for index++; index < len(lines) && strings.TrimSpace(lines[index]) != delimiter; index++ {
				values = append(values, lines[index])
			}
			outputs[key] = strings.Join(values, "\n")
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && key != "" {
			outputs[key] = value
		}
	}
	return outputs
}

func gitMetadata(repoPath string) (branch, sha, tag string) {
	branch = runGit(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	sha = runGit(repoPath, "rev-parse", "HEAD")
	tag = runGit(repoPath, "describe", "--tags", "--exact-match")
	if branch == "HEAD" {
		branch = ""
	}
	return
}

func changedFiles(repoPath, baseRef string) []interface{} {
	seen := map[string]bool{}
	addOutput := func(output string) {
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				seen[line] = true
			}
		}
	}
	addOutput(runGit(repoPath, "diff", "--name-only"))
	addOutput(runGit(repoPath, "diff", "--cached", "--name-only"))
	if baseRef == "" {
		baseRef = "HEAD^"
	}
	addOutput(runGit(repoPath, "diff", "--name-only", baseRef+"..HEAD"))
	if output := runGit(repoPath, "ls-files", "--others", "--exclude-standard"); output != "" {
		addOutput(output)
	}
	result := make([]interface{}, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].(string) < result[j].(string) })
	return result
}

func runGit(repoPath string, args ...string) string {
	commandArgs := append([]string{"-C", repoPath}, args...)
	output, err := exec.Command("git", commandArgs...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func gitRef(branch, tag string) string {
	if tag != "" {
		return "refs/tags/" + tag
	}
	if branch != "" {
		return "refs/heads/" + branch
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func stringInterfaceMap(values map[string]string) map[string]interface{} {
	result := map[string]interface{}{}
	for key, value := range values {
		result[key] = value
	}
	return result
}

func (e *Executor) isBuiltinAction(provider model.ProviderID, uses string) bool {
	name := strings.ToLower(actionName(uses))
	if provider == model.ProviderGitHub {
		return name == "actions/upload-artifact" || name == "actions/download-artifact" || name == "actions/cache"
	}
	if provider == model.ProviderAzure {
		task := strings.ToLower(uses)
		return strings.HasPrefix(task, "publishbuildartifacts@") ||
			strings.HasPrefix(task, "downloadbuildartifacts@") ||
			strings.HasPrefix(task, "cache@")
	}
	return false
}

func (e *Executor) executeBuiltinAction(request model.RunRequest, job model.Job, step model.Step, workspacePath, cacheScope string, runtime *runtimeContext, emit logs.Emitter) error {
	uses := strings.ToLower(actionName(step.Uses))
	switch {
	case uses == "actions/upload-artifact":
		if e.artifacts == nil {
			return fmt.Errorf("artifact store is unavailable")
		}
		name := firstNonEmpty(step.With["name"], "artifact")
		paths := splitPaths(step.With["path"])
		record, err := e.artifacts.Publish(request.RunID, job.ID, name, workspacePath, paths)
		if err != nil {
			return err
		}
		event := systemEvent(job.ID, step.ID, "artifact_published", model.RunSucceeded, fmt.Sprintf("Published artifact %s.", name))
		event.Data = map[string]interface{}{"artifact": record}
		emit(event)
		return nil
	case uses == "actions/download-artifact":
		if e.artifacts == nil {
			return fmt.Errorf("artifact store is unavailable")
		}
		name := step.With["name"]
		if name == "" {
			return fmt.Errorf("download-artifact requires a name for local emulation")
		}
		target := workspacePath
		if configured := step.With["path"]; configured != "" {
			var err error
			target, err = workspace.Resolve(workspacePath, configured)
			if err != nil {
				return err
			}
		}
		if err := e.artifacts.Download(name, target); err != nil {
			return err
		}
		emit(systemEvent(job.ID, step.ID, "artifact_downloaded", model.RunSucceeded, fmt.Sprintf("Downloaded artifact %s.", name)))
		return nil
	case uses == "actions/cache":
		spec := model.CacheSpec{
			Key: step.With["key"], Paths: splitPaths(step.With["path"]),
			RestoreKeys: splitPaths(step.With["restore-keys"]),
		}
		if err := e.restoreCache(spec, cacheScope, workspacePath, runtime, emit, job.ID); err != nil {
			return err
		}
		runtime.actionCaches = append(runtime.actionCaches, spec)
		return nil
	case strings.HasPrefix(strings.ToLower(step.Uses), "publishbuildartifacts@"):
		name := firstNonEmpty(step.With["artifactName"], step.With["ArtifactName"], "artifact")
		source := firstNonEmpty(step.With["pathToPublish"], step.With["PathtoPublish"])
		record, err := e.artifacts.Publish(request.RunID, job.ID, name, workspacePath, splitPaths(source))
		if err != nil {
			return err
		}
		event := systemEvent(job.ID, step.ID, "artifact_published", model.RunSucceeded, fmt.Sprintf("Published artifact %s.", name))
		event.Data = map[string]interface{}{"artifact": record}
		emit(event)
		return nil
	case strings.HasPrefix(strings.ToLower(step.Uses), "downloadbuildartifacts@"):
		name := firstNonEmpty(step.With["artifactName"], step.With["buildType"])
		if name == "" {
			return fmt.Errorf("download artifact task requires artifactName")
		}
		return e.artifacts.Download(name, workspacePath)
	case strings.HasPrefix(strings.ToLower(step.Uses), "cache@"):
		spec := model.CacheSpec{
			Key:         firstNonEmpty(step.With["key"], step.With["Key"]),
			Paths:       splitPaths(firstNonEmpty(step.With["path"], step.With["Path"])),
			RestoreKeys: splitPaths(firstNonEmpty(step.With["restoreKeys"], step.With["restore-keys"])),
		}
		if err := e.restoreCache(spec, cacheScope, workspacePath, runtime, emit, job.ID); err != nil {
			return err
		}
		runtime.actionCaches = append(runtime.actionCaches, spec)
		return nil
	default:
		return fmt.Errorf("unsupported built-in action %s", step.Uses)
	}
}

func (e *Executor) restoreJobCaches(job model.Job, scope, workspacePath string, runtime *runtimeContext, emit logs.Emitter) error {
	for _, spec := range job.Caches {
		if spec.Policy == "push" {
			continue
		}
		if err := e.restoreCache(spec, scope, workspacePath, runtime, emit, job.ID); err != nil {
			return err
		}
	}
	return nil
}

func (e *Executor) restoreCache(spec model.CacheSpec, scope, workspacePath string, runtime *runtimeContext, emit logs.Emitter, jobID string) error {
	if e.caches == nil || spec.Key == "" || len(spec.Paths) == 0 {
		return nil
	}
	key, evalErr := expression.Interpolate(runtime.request.Provider, spec.Key, runtime.expressionContext(runtime.status))
	if evalErr != nil {
		return fmt.Errorf("interpolate cache key: %s", evalErr.Message)
	}
	record, err := e.caches.Restore(scope, key, spec.RestoreKeys, workspacePath, spec.Paths)
	if err != nil {
		return err
	}
	if record == nil {
		emit(systemEvent(jobID, "", "cache_miss", model.RunRunning, fmt.Sprintf("Cache miss for key %s.", key)))
	} else {
		event := systemEvent(jobID, "", "cache_hit", model.RunSucceeded, fmt.Sprintf("Restored cache key %s.", record.Key))
		event.Data = map[string]interface{}{"cache": record}
		emit(event)
	}
	return nil
}

func (e *Executor) saveJobCaches(job model.Job, scope, workspacePath string, runtime *runtimeContext, emit logs.Emitter) error {
	specs := append([]model.CacheSpec(nil), job.Caches...)
	specs = append(specs, runtime.actionCaches...)
	for _, spec := range specs {
		if spec.Policy == "pull" || e.caches == nil || spec.Key == "" || len(spec.Paths) == 0 {
			continue
		}
		key, evalErr := expression.Interpolate(runtime.request.Provider, spec.Key, runtime.expressionContext(runtime.status))
		if evalErr != nil {
			return fmt.Errorf("interpolate cache key: %s", evalErr.Message)
		}
		record, err := e.caches.Save(scope, key, workspacePath, spec.Paths)
		if err != nil {
			return err
		}
		event := systemEvent(job.ID, "", "cache_saved", model.RunSucceeded, fmt.Sprintf("Saved cache key %s.", key))
		event.Data = map[string]interface{}{"cache": record}
		emit(event)
	}
	return nil
}

func (e *Executor) publishJobArtifacts(request model.RunRequest, job model.Job, workspacePath string, emit logs.Emitter) error {
	if e.artifacts == nil {
		return nil
	}
	for _, spec := range job.Artifacts {
		if spec.When == "never" || len(spec.Paths) == 0 {
			continue
		}
		record, err := e.artifacts.Publish(request.RunID, job.ID, spec.Name, workspacePath, spec.Paths)
		if err != nil {
			return err
		}
		event := systemEvent(job.ID, "", "artifact_published", model.RunSucceeded, fmt.Sprintf("Published artifact %s.", spec.Name))
		event.Data = map[string]interface{}{"artifact": record}
		emit(event)
	}
	return nil
}

func splitPaths(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ','
	})
	result := []string{}
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func mockOIDCToken(configured map[string]string) string {
	claims := map[string]interface{}{
		"iss":        "https://piper.invalid/mock-oidc",
		"aud":        "piper-local-only",
		"sub":        "piper:local:test",
		"piper_mock": true,
		"iat":        time.Now().UTC().Unix(),
		"exp":        time.Now().UTC().Add(15 * time.Minute).Unix(),
	}
	for key, value := range configured {
		claims[key] = value
	}
	payload, _ := json.Marshal(claims)
	return "PIPER_MOCK_OIDC." + base64.RawURLEncoding.EncodeToString(payload)
}

func (e *Executor) resolveActions(ctx context.Context, request model.RunRequest, job model.Job, workspacePath string, emit logs.Emitter) (map[string]*actions.Action, error) {
	result := map[string]*actions.Action{}
	if e.actions == nil {
		return result, nil
	}
	allowRemote := request.AllowRemoteActions || request.PreparedToken != "" || strings.EqualFold(request.Env["PIPER_ALLOW_REMOTE_ACTIONS"], "true")
	for _, step := range job.Steps {
		if step.Uses == "" || strings.HasPrefix(step.Uses, "actions/checkout@") ||
			isSetupAction(step.Uses) || e.isBuiltinAction(request.Provider, step.Uses) ||
			(request.Provider == model.ProviderAzure && step.Uses == "checkout") {
			continue
		}
		if request.Provider != model.ProviderGitHub {
			continue
		}
		action, err := e.actions.Resolve(ctx, step.Uses, workspacePath, allowRemote)
		if err != nil {
			return nil, err
		}
		result[step.Uses] = action
		event := systemEvent(job.ID, step.ID, "action_resolved", model.RunSucceeded, fmt.Sprintf("Resolved action %s.", step.Uses))
		event.Data = map[string]interface{}{
			"reference": action.Reference, "resolvedSha": action.ResolvedSHA,
			"remote": action.Remote, "mutableRef": action.MutableRef, "using": action.Using,
		}
		emit(event)
		if action.MutableRef {
			emit(systemEvent(job.ID, step.ID, "security_warning", model.RunRunning, fmt.Sprintf("Action %s uses a mutable tag or branch; resolved %s.", step.Uses, action.ResolvedSHA)))
		}
	}
	return result, nil
}

func (e *Executor) executeResolvedAction(ctx context.Context, cli *client.Client, containerID string, request model.RunRequest, job model.Job, original model.Step, stepID string, action *actions.Action, runtime *runtimeContext, emit logs.Emitter, masker secrets.Masker) (map[string]string, error) {
	actionEnv := map[string]string{}
	for key, value := range original.With {
		actionEnv["INPUT_"+envKey(key)] = value
	}
	runStep := func(name, command, shell string, index int, extraEnv map[string]string) (map[string]string, error) {
		env := map[string]string{}
		for key, value := range actionEnv {
			env[key] = value
		}
		for key, value := range extraEnv {
			env[key] = value
		}
		step := model.Step{
			Name: name, Run: command, Shell: shell, Env: env,
			WorkingDirectory: action.ContainerPath,
		}
		if strings.HasPrefix(step.WorkingDirectory, "/workspace/") {
			step.WorkingDirectory = strings.TrimPrefix(step.WorkingDirectory, "/workspace/")
		}
		return e.executeShellStep(ctx, cli, containerID, request, job, step, fmt.Sprintf("%s-action-%d", stepID, index), runtime, emit, masker)
	}

	outputs := map[string]string{}
	mergeOutputs := func(values map[string]string) {
		for key, value := range values {
			outputs[key] = value
		}
	}
	switch {
	case strings.HasPrefix(action.Using, "node"):
		phases := []struct {
			name string
			file string
		}{{"pre", action.Pre}, {"main", action.Main}, {"post", action.Post}}
		for _, phase := range phases {
			if phase.file == "" {
				continue
			}
			values, err := e.executeNodeActionPhase(ctx, cli, containerID, request, job, original, stepID, action, phase.file, emit, masker)
			if err != nil {
				return outputs, err
			}
			mergeOutputs(values)
		}
	case action.Using == "composite":
		for index, child := range action.Steps {
			if child.Run == "" {
				return outputs, fmt.Errorf("composite action %s contains unsupported uses step %s", action.Reference, child.Uses)
			}
			values, err := runStep(firstNonEmpty(child.Name, fmt.Sprintf("composite step %d", index+1)), child.Run, firstNonEmpty(child.Shell, "bash"), index+1, child.Env)
			if err != nil {
				return outputs, err
			}
			mergeOutputs(values)
		}
	case action.Using == "docker":
		return e.executeDockerAction(ctx, cli, containerID, request, job, original, stepID, action, emit, masker)
	default:
		return nil, fmt.Errorf("action %s uses unsupported runtime %q", action.Reference, action.Using)
	}
	return outputs, nil
}

func (e *Executor) executeNodeActionPhase(ctx context.Context, cli *client.Client, jobContainerID string, request model.RunRequest, job model.Job, original model.Step, stepID string, action *actions.Action, script string, emit logs.Emitter, masker secrets.Masker) (map[string]string, error) {
	version := strings.TrimPrefix(action.Using, "node")
	if version == "" || version == "12" || version == "16" {
		version = "20"
	}
	imageName := "node:" + version + "-bookworm"
	if err := e.pullImage(ctx, cli, imageName, emit, masker); err != nil {
		return nil, err
	}
	inspection, err := cli.ContainerInspect(ctx, jobContainerID)
	if err != nil {
		return nil, err
	}
	workspaceSource := ""
	for _, mounted := range inspection.Mounts {
		if mounted.Destination == "/workspace" {
			workspaceSource = mounted.Source
			break
		}
	}
	commandDir, err := os.MkdirTemp("", "piper-node-action-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(commandDir)
	env := []string{"GITHUB_OUTPUT=/piper-command/output"}
	for key, value := range original.With {
		env = append(env, "INPUT_"+envKey(key)+"="+value)
	}
	mounts := []mount.Mount{
		{Type: mount.TypeBind, Source: workspaceSource, Target: "/workspace"},
		{Type: mount.TypeBind, Source: commandDir, Target: "/piper-command"},
	}
	if e.actions != nil && e.actions.CacheRoot() != "" {
		mounts = append(mounts, mount.Mount{
			Type: mount.TypeBind, Source: e.actions.CacheRoot(), Target: "/piper-actions", ReadOnly: true,
		})
	}
	response, err := cli.ContainerCreate(ctx, &container.Config{
		Image: imageName, WorkingDir: action.ContainerPath, Env: env,
		Cmd: []string{"node", path.Join(action.ContainerPath, script)},
	}, &container.HostConfig{
		AutoRemove: false, NetworkMode: container.NetworkMode("container:" + jobContainerID), Mounts: mounts,
	}, nil, nil, "piper-node-action-"+uuid.NewString())
	if err != nil {
		return nil, err
	}
	defer cli.ContainerRemove(context.Background(), response.ID, container.RemoveOptions{Force: true})
	if err := cli.ContainerStart(ctx, response.ID, container.StartOptions{}); err != nil {
		return nil, err
	}
	reader, _ := cli.ContainerLogs(ctx, response.ID, container.LogsOptions{ShowStdout: true, ShowStderr: true, Follow: true})
	if reader != nil {
		stdout := newEventWriter(emit, masker, logs.Event{Type: "step_log", JobID: job.ID, StepID: stepID, Stream: "stdout"})
		stderr := newEventWriter(emit, masker, logs.Event{Type: "step_log", JobID: job.ID, StepID: stepID, Stream: "stderr"})
		_, _ = stdcopy.StdCopy(stdout, stderr, reader)
		stdout.Flush()
		stderr.Flush()
		_ = reader.Close()
	}
	statusCh, errorCh := cli.ContainerWait(ctx, response.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errorCh:
		return nil, err
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return nil, fmt.Errorf("JavaScript action exited with status %d", status.StatusCode)
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	data, err := os.ReadFile(filepath.Join(commandDir, "output"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseOutputText(string(data)), nil
}

func (e *Executor) executeDockerAction(ctx context.Context, cli *client.Client, jobContainerID string, request model.RunRequest, job model.Job, original model.Step, stepID string, action *actions.Action, emit logs.Emitter, masker secrets.Masker) (map[string]string, error) {
	if !strings.HasPrefix(action.Image, "docker://") {
		return nil, fmt.Errorf("Docker action %s uses a Dockerfile; local Dockerfile action builds are not yet supported", action.Reference)
	}
	imageName := strings.TrimPrefix(action.Image, "docker://")
	if err := e.pullImage(ctx, cli, imageName, emit, masker); err != nil {
		return nil, err
	}
	inspection, err := cli.ContainerInspect(ctx, jobContainerID)
	if err != nil {
		return nil, err
	}
	workspaceSource := ""
	for _, mounted := range inspection.Mounts {
		if mounted.Destination == "/workspace" {
			workspaceSource = mounted.Source
			break
		}
	}
	if workspaceSource == "" {
		return nil, fmt.Errorf("job workspace mount was not found")
	}
	commandDir, err := os.MkdirTemp("", "piper-action-command-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(commandDir)
	env := []string{"GITHUB_OUTPUT=/piper-command/output"}
	for key, value := range original.With {
		env = append(env, "INPUT_"+envKey(key)+"="+value)
	}
	config := &container.Config{
		Image: imageName, Env: env, WorkingDir: "/workspace",
		Cmd: action.Args, AttachStdout: true, AttachStderr: true,
	}
	if action.Entrypoint != "" {
		config.Entrypoint = []string{action.Entrypoint}
	}
	response, err := cli.ContainerCreate(ctx, config, &container.HostConfig{
		AutoRemove:  false,
		NetworkMode: container.NetworkMode("container:" + jobContainerID),
		Mounts: []mount.Mount{
			{Type: mount.TypeBind, Source: workspaceSource, Target: "/workspace"},
			{Type: mount.TypeBind, Source: commandDir, Target: "/piper-command"},
		},
	}, nil, nil, "piper-action-"+uuid.NewString())
	if err != nil {
		return nil, fmt.Errorf("create Docker action: %w", err)
	}
	defer cli.ContainerRemove(context.Background(), response.ID, container.RemoveOptions{Force: true})
	if err := cli.ContainerStart(ctx, response.ID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("start Docker action: %w", err)
	}
	reader, err := cli.ContainerLogs(ctx, response.ID, container.LogsOptions{
		ShowStdout: true, ShowStderr: true, Follow: true,
	})
	if err == nil {
		stdout := newEventWriter(emit, masker, logs.Event{Type: "step_log", JobID: job.ID, StepID: stepID, Stream: "stdout"})
		stderr := newEventWriter(emit, masker, logs.Event{Type: "step_log", JobID: job.ID, StepID: stepID, Stream: "stderr"})
		_, _ = stdcopy.StdCopy(stdout, stderr, reader)
		stdout.Flush()
		stderr.Flush()
		_ = reader.Close()
	}
	waitStatus, waitErr := cli.ContainerWait(ctx, response.ID, container.WaitConditionNotRunning)
	select {
	case err := <-waitErr:
		if err != nil {
			return nil, err
		}
	case status := <-waitStatus:
		if status.StatusCode != 0 {
			return nil, fmt.Errorf("Docker action exited with status %d", status.StatusCode)
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	data, err := os.ReadFile(filepath.Join(commandDir, "output"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseOutputText(string(data)), nil
}

var invalidEnvChar = regexp.MustCompile(`[^A-Za-z0-9_]`)

func envKey(value string) string {
	value = invalidEnvChar.ReplaceAllString(value, "_")
	return strings.ToUpper(value)
}

func systemEvent(jobID, stepID, eventType string, status model.RunStatus, message string) logs.Event {
	event := logs.New("", eventType, message)
	event.JobID = jobID
	event.StepID = stepID
	event.Status = status
	return event
}

func rejectingFeature(refs []model.FeatureRef) (model.FeatureSupport, bool, error) {
	registry, err := support.Default()
	if err != nil {
		return model.FeatureSupport{}, false, err
	}
	for _, ref := range refs {
		feature, resolveErr := registry.Resolve(ref)
		if resolveErr != nil {
			return model.FeatureSupport{}, false, resolveErr
		}
		if feature.RuntimeDisposition == model.RuntimeReject {
			return feature, true, nil
		}
	}
	return model.FeatureSupport{}, false, nil
}

func emitSupportError(emit logs.Emitter, jobID, stepID string, feature model.FeatureSupport) {
	event := systemEvent(jobID, stepID, "support_error", model.RunFailed, feature.Message)
	event.Data = supportEventData(feature)
	emit(event)
}

func emulationEvent(jobID, stepID, featureID, message string) logs.Event {
	event := systemEvent(jobID, stepID, "step_emulated", model.RunSucceeded, message)
	if registry, err := support.Default(); err == nil {
		if feature, resolveErr := registry.Resolve(model.FeatureRef{ID: featureID}); resolveErr == nil {
			event.Data = supportEventData(feature)
		}
	}
	return event
}

func supportEventData(feature model.FeatureSupport) map[string]interface{} {
	return map[string]interface{}{
		"featureId": feature.FeatureID, "provider": feature.Provider, "status": feature.Support,
		"runtimeDisposition": feature.RuntimeDisposition, "path": feature.Path, "origin": feature.Origin,
		"localBehavior": feature.LocalBehavior, "hostedDifferences": feature.HostedDifferences,
		"securityImplications": feature.SecurityImplications, "fallback": feature.Fallback,
	}
}

type eventWriter struct {
	mu     sync.Mutex
	emit   logs.Emitter
	masker secrets.Masker
	base   logs.Event
	buffer []byte
}

func newEventWriter(emit logs.Emitter, masker secrets.Masker, base logs.Event) *eventWriter {
	return &eventWriter{emit: emit, masker: masker, base: base}
}

func (w *eventWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buffer = append(w.buffer, p...)
	for {
		index := strings.IndexByte(string(w.buffer), '\n')
		if index < 0 {
			break
		}
		line := string(w.buffer[:index])
		w.buffer = w.buffer[index+1:]
		w.emitLine(line)
	}
	return len(p), nil
}

func (w *eventWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buffer) > 0 {
		w.emitLine(string(w.buffer))
		w.buffer = nil
	}
}

func (w *eventWriter) emitLine(line string) {
	event := w.base
	event.Time = logs.New("", w.base.Type, "").Time
	event.Message = w.masker.Mask(strings.TrimRight(line, "\r"))
	w.emit(event)
}

var _ io.Writer = (*eventWriter)(nil)
