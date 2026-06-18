package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/uuid"
	"github.com/7samael7/Piper/engine/internal/logs"
	"github.com/7samael7/Piper/engine/internal/pipeline/graph"
	"github.com/7samael7/Piper/engine/internal/pipeline/model"
	"github.com/7samael7/Piper/engine/internal/secrets"
)

type Executor struct {
	image string
}

func NewExecutor() *Executor {
	return &Executor{image: "ubuntu:22.04"}
}

func (e *Executor) Execute(ctx context.Context, request model.RunRequest, workflow *model.Workflow, emit logs.Emitter) error {
	masker := secrets.NewMasker(request.Secrets)
	emit(systemEvent("", "", "run_started", model.RunRunning, "Local run started."))
	emit(systemEvent("", "", "support_notice", "", "Local Docker execution is an MVP and does not exactly match hosted CI runners."))

	cli, err := connectDocker(ctx)
	if err != nil {
		return err
	}
	defer cli.Close()

	jobs, err := selectedJobs(workflow, request.JobID)
	if err != nil {
		return err
	}

	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.executeJob(ctx, cli, request, job, emit, masker); err != nil {
			return err
		}
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

func (e *Executor) executeJob(ctx context.Context, cli *client.Client, request model.RunRequest, job model.Job, emit logs.Emitter, masker secrets.Masker) error {
	emit(systemEvent(job.ID, "", "job_started", model.RunRunning, fmt.Sprintf("Job %s started.", job.Name)))
	if job.ReusableWorkflow != "" {
		return fmt.Errorf("job %s uses reusable workflow %s, which is unsupported locally", job.ID, job.ReusableWorkflow)
	}
	if job.HasContainer {
		return fmt.Errorf("job %s declares container, which is unsupported locally", job.ID)
	}
	if job.HasServices {
		return fmt.Errorf("job %s declares services, which are unsupported locally", job.ID)
	}
	if job.HasStrategy {
		return fmt.Errorf("job %s declares strategy, which is unsupported locally", job.ID)
	}
	imageName, err := resolveJobImage(e.image, job)
	if err != nil {
		return err
	}
	if err := e.pullImage(ctx, cli, imageName, emit, masker); err != nil {
		return err
	}
	containerID, err := createJobContainer(ctx, cli, imageName, request.RepoPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = cli.ContainerRemove(context.Background(), containerID, container.RemoveOptions{Force: true})
	}()

	for index, step := range job.Steps {
		stepID := step.ID
		if stepID == "" {
			stepID = fmt.Sprintf("step-%d", index+1)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		emit(systemEvent(job.ID, stepID, "step_started", model.RunRunning, fmt.Sprintf("Step %s started.", step.Name)))

		switch {
		case step.Run != "":
			if err := e.executeShellStep(ctx, cli, containerID, request, job, step, stepID, emit, masker); err != nil {
				emit(systemEvent(job.ID, stepID, "step_failed", model.RunFailed, err.Error()))
				return err
			}
			emit(systemEvent(job.ID, stepID, "step_finished", model.RunSucceeded, fmt.Sprintf("Step %s finished.", step.Name)))
		case strings.HasPrefix(step.Uses, "actions/checkout@"):
			emit(systemEvent(job.ID, stepID, "step_skipped", model.RunSucceeded, "actions/checkout is a local no-op because the repository is mounted."))
		case request.Provider == model.ProviderAzure && step.Uses == "checkout":
			emit(systemEvent(job.ID, stepID, "step_skipped", model.RunSucceeded, "Azure checkout is a local no-op because the repository is mounted."))
		case request.Provider == model.ProviderGitHub && isSetupAction(step.Uses):
			emit(systemEvent(job.ID, stepID, "step_finished", model.RunSucceeded, setupActionMessage(step, imageName)))
		case step.Uses != "":
			emit(systemEvent(job.ID, stepID, "step_unsupported", model.RunRunning, fmt.Sprintf("Action %s is unsupported locally and was skipped.", step.Uses)))
		default:
			emit(systemEvent(job.ID, stepID, "step_unsupported", model.RunRunning, "Step has no run or uses block and was skipped."))
		}
	}

	emit(systemEvent(job.ID, "", "job_finished", model.RunSucceeded, fmt.Sprintf("Job %s finished.", job.Name)))
	return nil
}

func createJobContainer(ctx context.Context, cli *client.Client, imageName, repoPath string) (string, error) {
	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image:      imageName,
		Cmd:        []string{"sleep", "infinity"},
		WorkingDir: "/workspace",
		Tty:        false,
	}, &container.HostConfig{
		AutoRemove: false,
		Mounts: []mount.Mount{{
			Type:   mount.TypeBind,
			Source: repoPath,
			Target: "/workspace",
		}},
	}, nil, nil, "piper-"+uuid.NewString())
	if err != nil {
		return "", fmt.Errorf("create job container: %w", err)
	}
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = cli.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
		return "", fmt.Errorf("start job container: %w", err)
	}
	return resp.ID, nil
}

func (e *Executor) executeShellStep(ctx context.Context, cli *client.Client, containerID string, request model.RunRequest, job model.Job, step model.Step, stepID string, emit logs.Emitter, masker secrets.Masker) error {
	workdir := "/workspace"
	if step.WorkingDirectory != "" {
		clean := path.Clean("/" + step.WorkingDirectory)
		workdir = path.Join("/workspace", strings.TrimPrefix(clean, "/"))
	}

	env := buildEnv(request, job, step)
	execResponse, err := cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		Cmd:          []string{"/bin/bash", "-lc", step.Run},
		WorkingDir:   workdir,
		Env:          env,
		Tty:          false,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return fmt.Errorf("create step process: %w", err)
	}

	attach, err := cli.ContainerExecAttach(ctx, execResponse.ID, container.ExecAttachOptions{Tty: false})
	if err != nil {
		return fmt.Errorf("attach step process: %w", err)
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
		return ctx.Err()
	case err := <-copyDone:
		if err != nil {
			return fmt.Errorf("read step output: %w", err)
		}
	}

	inspection, err := cli.ContainerExecInspect(ctx, execResponse.ID)
	if err != nil {
		return fmt.Errorf("inspect step process: %w", err)
	}
	if inspection.ExitCode != 0 {
		return fmt.Errorf("step exited with status %d", inspection.ExitCode)
	}
	return nil
}

func selectedJobs(workflow *model.Workflow, jobID string) ([]model.Job, error) {
	if jobID != "" {
		for _, job := range workflow.Jobs {
			if job.ID == jobID {
				return []model.Job{job}, nil
			}
		}
		return nil, fmt.Errorf("job %q was not found", jobID)
	}
	return graph.TopologicalSort(workflow)
}

func buildEnv(request model.RunRequest, job model.Job, step model.Step) []string {
	values := map[string]string{
		"CI":                          "true",
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
