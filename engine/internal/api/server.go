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
	"sync"
	"time"

	dockerexecutor "github.com/pipeline-workbench/engine/internal/executor/docker"
	"github.com/pipeline-workbench/engine/internal/logs"
	"github.com/pipeline-workbench/engine/internal/persistence"
	"github.com/pipeline-workbench/engine/internal/pipeline/model"
	"github.com/pipeline-workbench/engine/internal/providers/azure"
	"github.com/pipeline-workbench/engine/internal/providers/github"
	"github.com/pipeline-workbench/engine/internal/providers/gitlab"
)

type Server struct {
	in        io.Reader
	out       io.Writer
	store     *persistence.Store
	executor  *dockerexecutor.Executor
	providers map[model.ProviderID]model.Provider

	writeMu  sync.Mutex
	active   map[string]context.CancelFunc
	activeMu sync.Mutex
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
		active: map[string]context.CancelFunc{},
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
	case "run.cancel":
		var request cancelRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		return s.cancelRun(request.RunID), nil
	case "run.history":
		var request historyRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		return s.store.ListRuns(ctx, request.RepoPath, request.Limit)
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

	record, err := s.store.CreateRun(ctx, request)
	if err != nil {
		return model.RunStartResponse{}, err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.activeMu.Lock()
	s.active[record.ID] = cancel
	s.activeMu.Unlock()

	go s.executeRun(runCtx, record, request, workflow)
	return model.RunStartResponse{RunID: record.ID}, nil
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
	}()

	_ = s.store.UpdateRunStatus(context.Background(), record.ID, model.RunRunning, "", false)
	emit := func(event logs.Event) {
		if event.RunID == "" {
			event.RunID = record.ID
		}
		if event.Time.IsZero() {
			event.Time = time.Now().UTC()
		}
		_ = s.store.AppendEvent(context.Background(), event)
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
