# Architecture

Piper is split into a desktop shell and a local execution engine.

## Desktop

The desktop app lives in `apps/desktop` and uses Electron Forge with Vite. Electron's main process owns the Go sidecar process and exposes a small IPC API to the renderer through the preload script.

Renderer responsibilities:

- Select a local repository.
- Discover workflows through the engine.
- Render workflow graphs with React Flow.
- Show YAML and job details with Monaco Editor.
- Configure run inputs, environment variables, and secrets.
- Stream run events into an xterm.js terminal.
- Show SQLite-backed run history.

The renderer never talks to the engine directly. It calls Electron IPC methods, and the main process forwards JSON-RPC requests to the sidecar.

## Engine

The Go engine lives in `engine`. `cmd/daemon` starts a newline-delimited JSON-RPC server over stdin/stdout. This keeps the MVP packaging simple and still lets a future version move the same API to sockets or gRPC.

Core packages:

- `internal/pipeline/model`: provider-neutral workflow, job, graph, validation, and run types.
- `internal/providers/github`: GitHub Actions discovery and YAML parser.
- `internal/providers/gitlab`: GitLab CI/CD discovery and YAML parser.
- `internal/providers/azure`: Azure Pipelines YAML discovery and parser.
- `internal/pipeline/graph`: dependency graph construction and topological sorting.
- `internal/pipeline/validation`: support classification and validation.
- `internal/executor/docker`: Docker Engine SDK based local executor.
- `internal/persistence`: SQLite run history.
- `internal/logs`: structured event types.
- `internal/secrets`: log masking.
- `internal/api`: JSON-RPC server and method handlers.

## Provider model

Providers implement a neutral interface:

```go
type Provider interface {
    ID() string
    Discover(ctx context.Context, repoPath string) ([]model.WorkflowSummary, error)
    Load(ctx context.Context, repoPath, workflowPath string) (*model.Workflow, []byte, error)
    Validate(ctx context.Context, workflow *model.Workflow) model.ValidationReport
}
```

Additional providers can be added by implementing the same contract and mapping provider YAML concepts into the neutral model. GitHub Actions, GitLab CI/CD, and Azure Pipelines all use this interface in the MVP.

## Execution model

Local execution is deliberately narrow:

1. The engine validates and stores a run record.
2. The Docker executor builds a job order from the graph.
3. Shell steps run sequentially in Docker containers with the repository mounted at `/workspace`. The default image is `ubuntu:22.04`; GitLab job images are used when present.
4. Structured run events are persisted and emitted to the Electron app.
5. Cancellation flows through Go `context.Context` and kills the active container.

The MVP favors transparency over hidden emulation. Unsupported provider features are reported before and during execution.
