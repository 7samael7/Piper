package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	dockerexecutor "github.com/7samael7/Piper/engine/internal/executor/docker"
	"github.com/7samael7/Piper/engine/internal/logs"
	"github.com/7samael7/Piper/engine/internal/persistence"
	"github.com/7samael7/Piper/engine/internal/pipeline/model"
	"github.com/7samael7/Piper/engine/internal/providers/azure"
	"github.com/7samael7/Piper/engine/internal/providers/github"
	"github.com/7samael7/Piper/engine/internal/providers/gitlab"
	"github.com/7samael7/Piper/engine/internal/secrets"
	"github.com/google/uuid"
)

type Server struct {
	in        io.Reader
	out       io.Writer
	store     *persistence.Store
	executor  *dockerexecutor.Executor
	providers map[model.ProviderID]model.Provider

	writeMu    sync.Mutex
	active     map[string]context.CancelFunc
	activeMu   sync.Mutex
	prepared   map[string]preparedRun
	preparedMu sync.Mutex
	approvals  map[string]chan bool
	approvalMu sync.Mutex
}

type preparedRun struct {
	ExpiresAt     time.Time
	RemoteActions bool
	Deployments   bool
}

func NewServer(in io.Reader, out io.Writer, store *persistence.Store) *Server {
	githubProvider := github.NewProvider()
	gitlabProvider := gitlab.NewProvider()
	azureProvider := azure.NewProvider()
	return &Server{
		in:       in,
		out:      out,
		store:    store,
		executor: dockerexecutor.NewExecutor(),
		providers: map[model.ProviderID]model.Provider{
			githubProvider.ID(): githubProvider,
			gitlabProvider.ID(): gitlabProvider,
			azureProvider.ID():  azureProvider,
		},
		active:    map[string]context.CancelFunc{},
		prepared:  map[string]preparedRun{},
		approvals: map[string]chan bool{},
	}
}

func (s *Server) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(s.in)
	scanner.Buffer(make([]byte, 0, 1024*1024), 32*1024*1024)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var request rpcRequest
		if err := json.Unmarshal(line, &request); err != nil {
			s.writeResponse(json.RawMessage("null"), nil, &rpcError{Code: -32700, Message: err.Error()})
			continue
		}
		s.handle(ctx, request)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func (s *Server) handle(ctx context.Context, request rpcRequest) {
	result, err := s.dispatch(ctx, request.Method, request.Params)
	if err != nil {
		s.writeResponse(request.ID, nil, &rpcError{Code: -32000, Message: err.Error()})
		return
	}
	s.writeResponse(request.ID, result, nil)
}

func (s *Server) dispatch(ctx context.Context, method string, params json.RawMessage) (interface{}, error) {
	switch method {
	case "provider.list":
		return s.providerList(), nil
	case "workflow.discover":
		var request discoverRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		provider, err := s.provider(request.Provider)
		if err != nil {
			return nil, err
		}
		return provider.Discover(ctx, request.RepoPath)
	case "workflow.get":
		var request workflowRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		provider, err := s.provider(request.Provider)
		if err != nil {
			return nil, err
		}
		workflow, _, err := provider.Load(ctx, request.RepoPath, request.WorkflowPath)
		return workflow, err
	case "workflow.validate":
		var request workflowRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		provider, err := s.provider(request.Provider)
		if err != nil {
			return nil, err
		}
		workflow, _, err := provider.Load(ctx, request.RepoPath, request.WorkflowPath)
		if err != nil {
			return nil, err
		}
		return workflow.Validation, nil
	case "run.start":
		var request model.RunRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		return s.startRun(ctx, request)
	case "run.prepare":
		var request prepareRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		return s.prepareRun(ctx, request)
	case "run.cancel":
		var request cancelRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		return s.cancelRun(request.RunID), nil
	case "run.approve", "run.reject":
		var request runDecisionRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		return map[string]bool{"accepted": s.decideRun(request.RunID, method == "run.approve")}, nil
	case "run.history":
		var request historyRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		return s.store.ListRuns(ctx, request.RepoPath, request.Limit)
	case "run.get":
		var request runGetRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		record, err := s.store.GetRun(ctx, request.RunID)
		if err != nil {
			return nil, err
		}
		events, err := s.store.ListEvents(ctx, request.RunID)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"run": record, "events": events}, nil
	case "artifact.list":
		var request artifactListRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		if s.executor.Artifacts() == nil {
			return []model.ArtifactRecord{}, nil
		}
		return s.executor.Artifacts().List(request.RunID)
	case "cache.list":
		var request cacheRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		if s.executor.Caches() == nil {
			return []model.CacheRecord{}, nil
		}
		return s.executor.Caches().List(request.Scope)
	case "cache.clear":
		var request cacheRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		if s.executor.Caches() != nil {
			if err := s.executor.Caches().Clear(request.Scope); err != nil {
				return nil, err
			}
		}
		return map[string]bool{"cleared": true}, nil
	case "settings.get":
		return s.store.GetSettings(ctx)
	case "settings.update":
		var settings model.Settings
		if err := decodeParams(params, &settings); err != nil {
			return nil, err
		}
		if err := validateSettings(settings); err != nil {
			return nil, err
		}
		if err := s.store.UpdateSettings(ctx, settings); err != nil {
			return nil, err
		}
		return settings, nil
	case "trust.list":
		var request trustRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		return s.store.ListTrust(ctx, request.RepoPath)
	case "trust.update":
		var request trustRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		if err := s.store.UpdateTrust(ctx, request.RepoPath, request.Reference, request.ResolvedSHA, request.Trusted); err != nil {
			return nil, err
		}
		return map[string]bool{"updated": true}, nil
	default:
		return nil, fmt.Errorf("unknown method %q", method)
	}
}

func (s *Server) provider(id model.ProviderID) (model.Provider, error) {
	if id == "" {
		id = model.ProviderGitHub
	}
	provider, ok := s.providers[id]
	if !ok {
		return nil, fmt.Errorf("provider %q is not registered", id)
	}
	return provider, nil
}

func (s *Server) providerList() []providerInfo {
	return []providerInfo{
		{
			ID:           model.ProviderGitHub,
			Name:         "GitHub Actions",
			Description:  "Discover, validate, visualize, and locally execute GitHub Actions workflows.",
			Capabilities: commonCapabilities(),
		},
		{
			ID:           model.ProviderGitLab,
			Name:         "GitLab CI/CD",
			Description:  "Discover, validate, visualize, and locally execute GitLab CI/CD pipelines.",
			Capabilities: commonCapabilities(),
		},
		{
			ID:           model.ProviderAzure,
			Name:         "Azure Pipelines",
			Description:  "Discover, validate, visualize, and locally execute Azure Pipelines YAML.",
			Capabilities: commonCapabilities(),
		},
	}
}

func (s *Server) startRun(ctx context.Context, request model.RunRequest) (model.RunStartResponse, error) {
	if request.Provider == "" {
		request.Provider = model.ProviderGitHub
	}
	if request.EventName == "" {
		request.EventName = defaultEventName(request.Provider)
	}
	if request.Inputs == nil {
		request.Inputs = map[string]string{}
	}
	if request.Env == nil {
		request.Env = map[string]string{}
	}
	if request.Secrets == nil {
		request.Secrets = map[string]string{}
	}
	settings, settingsErr := s.store.GetSettings(ctx)
	if settingsErr == nil {
		if request.Concurrency == 0 {
			request.Concurrency = settings.Concurrency
		}
		if request.MaxExpandedJobs == 0 {
			request.MaxExpandedJobs = settings.MaxExpandedJobs
		}
		if request.WorkspaceMode == "" {
			request.WorkspaceMode = settings.WorkspaceMode
		}
		if request.NetworkAccess == "" {
			request.NetworkAccess = settings.NetworkAccess
		}
		if !request.MockOIDC {
			request.MockOIDC = settings.MockOIDC
		}
		if request.JobTimeoutSeconds == 0 {
			request.JobTimeoutSeconds = settings.JobTimeoutSeconds
		}
		if request.StepTimeoutSeconds == 0 {
			request.StepTimeoutSeconds = settings.StepTimeoutSeconds
		}
		if request.MemoryMB == 0 {
			request.MemoryMB = settings.MemoryMB
		}
		if request.CPUs == 0 {
			request.CPUs = settings.CPUs
		}
		if request.PidsLimit == 0 {
			request.PidsLimit = settings.PidsLimit
		}
	}

	provider, err := s.provider(request.Provider)
	if err != nil {
		return model.RunStartResponse{}, err
	}
	workflow, _, err := provider.Load(ctx, request.RepoPath, request.WorkflowPath)
	if err != nil {
		return model.RunStartResponse{}, err
	}
	if !workflow.Validation.Valid {
		return model.RunStartResponse{}, fmt.Errorf("workflow is invalid and cannot be run locally")
	}
	remoteActions, _ := runRequirements(workflow)
	if remoteActions {
		trusted, trustErr := s.store.ListTrust(ctx, request.RepoPath)
		if trustErr == nil && allRemoteActionsTrusted(workflow, trusted) {
			remoteActions = false
			request.AllowRemoteActions = true
		}
	}
	if remoteActions {
		s.preparedMu.Lock()
		prepared, ok := s.prepared[request.PreparedToken]
		if ok && time.Now().After(prepared.ExpiresAt) {
			delete(s.prepared, request.PreparedToken)
			ok = false
		}
		s.preparedMu.Unlock()
		if !ok || !prepared.RemoteActions {
			return model.RunStartResponse{}, fmt.Errorf("this run requires preparation and explicit consent before remote actions can execute")
		}
		request.AllowRemoteActions = true
	}

	record, err := s.store.CreateRun(ctx, request)
	if err != nil {
		return model.RunStartResponse{}, err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.activeMu.Lock()
	s.active[record.ID] = cancel
	s.activeMu.Unlock()
	s.approvalMu.Lock()
	s.approvals[record.ID] = make(chan bool, 1)
	s.approvalMu.Unlock()

	go s.executeRun(runCtx, record, request, workflow)
	return model.RunStartResponse{RunID: record.ID}, nil
}

func (s *Server) prepareRun(ctx context.Context, request prepareRequest) (map[string]interface{}, error) {
	provider, err := s.provider(request.Provider)
	if err != nil {
		return nil, err
	}
	workflow, _, err := provider.Load(ctx, request.RepoPath, request.WorkflowPath)
	if err != nil {
		return nil, err
	}
	remoteActions, deployments := runRequirements(workflow)
	requirements := []string{}
	if remoteActions {
		requirements = append(requirements, "third_party_code")
	}
	if deployments {
		requirements = append(requirements, "deployment_approval")
	}
	response := map[string]interface{}{
		"requirements":     requirements,
		"requiresConsent":  remoteActions,
		"requiresApproval": deployments,
	}
	if (!remoteActions || request.AllowThirdPartyCode) && (!deployments || request.ApproveDeployments) {
		token := uuid.NewString()
		s.preparedMu.Lock()
		s.prepared[token] = preparedRun{
			ExpiresAt:     time.Now().Add(15 * time.Minute),
			RemoteActions: request.AllowThirdPartyCode,
			Deployments:   request.ApproveDeployments,
		}
		s.preparedMu.Unlock()
		response["preparedToken"] = token
		response["expiresAt"] = time.Now().Add(15 * time.Minute).UTC()
	}
	return response, nil
}

func runRequirements(workflow *model.Workflow) (remoteActions, deployments bool) {
	for _, job := range workflow.Jobs {
		if job.Environment != "" {
			deployments = true
		}
		for _, step := range job.Steps {
			if step.Uses == "" || strings.HasPrefix(step.Uses, "./") || isBuiltinActionReference(step.Uses) {
				continue
			}
			if workflow.Provider == model.ProviderGitHub && strings.Contains(step.Uses, "@") {
				remoteActions = true
			}
		}
	}
	return
}

func allRemoteActionsTrusted(workflow *model.Workflow, trusted []map[string]string) bool {
	allowed := map[string]bool{}
	for _, entry := range trusted {
		allowed[entry["reference"]] = true
	}
	found := false
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if workflow.Provider != model.ProviderGitHub || step.Uses == "" || strings.HasPrefix(step.Uses, "./") ||
				isBuiltinActionReference(step.Uses) {
				continue
			}
			if strings.Contains(step.Uses, "@") {
				found = true
				if !allowed[step.Uses] {
					return false
				}
			}
		}
	}
	return found
}

func isBuiltinActionReference(reference string) bool {
	return strings.HasPrefix(reference, "actions/checkout@") ||
		strings.HasPrefix(reference, "actions/setup-") ||
		strings.HasPrefix(reference, "actions/upload-artifact@") ||
		strings.HasPrefix(reference, "actions/download-artifact@") ||
		strings.HasPrefix(reference, "actions/cache@")
}

func validateSettings(settings model.Settings) error {
	if settings.Concurrency < 1 || settings.Concurrency > 64 {
		return fmt.Errorf("concurrency must be between 1 and 64")
	}
	if settings.MaxExpandedJobs < 1 || settings.MaxExpandedJobs > 1024 {
		return fmt.Errorf("maxExpandedJobs must be between 1 and 1024")
	}
	switch settings.WorkspaceMode {
	case "writable", "read-only", "isolated":
	default:
		return fmt.Errorf("workspaceMode must be writable, read-only, or isolated")
	}
	switch settings.NetworkAccess {
	case "enabled", "disabled", "internal":
	default:
		return fmt.Errorf("networkAccess must be enabled, disabled, or internal")
	}
	if settings.JobTimeoutSeconds < 0 || settings.StepTimeoutSeconds < 0 {
		return fmt.Errorf("timeout values cannot be negative")
	}
	if settings.MemoryMB < 0 || settings.CPUs < 0 || settings.PidsLimit < 0 {
		return fmt.Errorf("resource limits cannot be negative")
	}
	return nil
}

func commonCapabilities() []capability {
	return []capability{
		{Name: "discover", Support: model.SupportSupported},
		{Name: "validate", Support: model.SupportSupported},
		{Name: "graph", Support: model.SupportSupported},
		{Name: "run shell steps", Support: model.SupportPartial},
	}
}

func defaultEventName(provider model.ProviderID) string {
	switch provider {
	case model.ProviderGitLab:
		return "web"
	case model.ProviderAzure:
		return "manual"
	default:
		return "workflow_dispatch"
	}
}

func (s *Server) executeRun(ctx context.Context, record model.RunRecord, request model.RunRequest, workflow *model.Workflow) {
	defer func() {
		s.activeMu.Lock()
		delete(s.active, record.ID)
		s.activeMu.Unlock()
		s.approvalMu.Lock()
		delete(s.approvals, record.ID)
		s.approvalMu.Unlock()
	}()

	_ = s.store.UpdateRunStatus(context.Background(), record.ID, model.RunRunning, "", false)
	masker := secrets.NewMasker(request.Secrets)
	emit := func(event logs.Event) {
		if event.RunID == "" {
			event.RunID = record.ID
		}
		if event.Time.IsZero() {
			event.Time = time.Now().UTC()
		}
		event.Message = masker.Mask(event.Message)
		if event.Data != nil {
			if masked, ok := masker.MaskValue(event.Data).(map[string]interface{}); ok {
				event.Data = masked
			}
		}
		_ = s.store.AppendEvent(context.Background(), event)
		_ = s.store.UpsertExecutionStatus(context.Background(), event)
		s.writeNotification("run.event", event)
	}

	for _, feature := range workflow.Validation.Features {
		if feature.Support != model.SupportSupported {
			emit(logs.Event{
				Time:    time.Now().UTC(),
				Type:    "support_feature",
				Stream:  "system",
				Message: feature.Message,
				Data: map[string]interface{}{
					"feature": feature.Feature,
					"path":    feature.Path,
					"support": feature.Support,
				},
			})
		}
	}

	request.RunID = record.ID
	request.WaitForApproval = func(waitCtx context.Context, environment string) (bool, error) {
		s.approvalMu.Lock()
		channel := s.approvals[record.ID]
		s.approvalMu.Unlock()
		if channel == nil {
			return false, fmt.Errorf("approval channel is unavailable for %s", environment)
		}
		select {
		case approved := <-channel:
			return approved, nil
		case <-waitCtx.Done():
			return false, waitCtx.Err()
		}
	}
	err := s.executor.Execute(ctx, request, workflow, emit)
	switch {
	case err == nil:
		_ = s.store.UpdateRunStatus(context.Background(), record.ID, model.RunSucceeded, "succeeded", true)
	case errors.Is(err, context.Canceled):
		emit(logs.Event{
			Time:    time.Now().UTC(),
			Type:    "run_cancelled",
			Stream:  "system",
			Status:  model.RunCancelled,
			Message: "Local run was cancelled.",
		})
		_ = s.store.UpdateRunStatus(context.Background(), record.ID, model.RunCancelled, "cancelled", true)
	default:
		emit(logs.Event{
			Time:    time.Now().UTC(),
			Type:    "run_failed",
			Stream:  "system",
			Status:  model.RunFailed,
			Message: err.Error(),
		})
		_ = s.store.UpdateRunStatus(context.Background(), record.ID, model.RunFailed, "failed", true)
	}
}

func (s *Server) cancelRun(runID string) cancelResponse {
	s.activeMu.Lock()
	cancel, ok := s.active[runID]
	s.activeMu.Unlock()
	if ok {
		cancel()
	}
	return cancelResponse{Cancelled: ok}
}

func (s *Server) decideRun(runID string, approved bool) bool {
	s.approvalMu.Lock()
	channel, ok := s.approvals[runID]
	s.approvalMu.Unlock()
	if !ok {
		return false
	}
	select {
	case channel <- approved:
		return true
	default:
		return false
	}
}

func (s *Server) writeResponse(id json.RawMessage, result interface{}, rpcErr *rpcError) {
	response := rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
		Error:   rpcErr,
	}
	s.writeJSON(response)
}

func (s *Server) writeNotification(method string, params interface{}) {
	s.writeJSON(rpcNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
}

func (s *Server) writeJSON(value interface{}) {
	bytes, err := json.Marshal(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal JSON-RPC message: %v\n", err)
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, _ = s.out.Write(append(bytes, '\n'))
}

func decodeParams(params json.RawMessage, target interface{}) error {
	if len(params) == 0 || string(params) == "null" {
		params = []byte("{}")
	}
	return json.Unmarshal(params, target)
}
