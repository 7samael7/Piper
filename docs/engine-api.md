# Engine API

The Piper desktop app communicates with its Go engine through newline-delimited JSON-RPC 2.0 over standard input and standard output.

The daemon is local and does not listen on a network port.

## Transport

- One complete JSON object per line.
- Requests are written to stdin.
- Responses and notifications are written to stdout.
- Diagnostics are written to stderr.
- Request and response order is not guaranteed once asynchronous runs start; clients must correlate responses by `id`.
- The scanner accepts messages up to 32 MiB.

Start the daemon:

```sh
cd engine
PIPER_DB=/tmp/piper.db go run ./cmd/daemon
```

A minimal request:

```json
{"jsonrpc":"2.0","id":1,"method":"provider.list","params":{}}
```

## Error behavior

Malformed JSON returns JSON-RPC error code `-32700`.

Dispatch, validation, provider, persistence, and execution-start errors return code `-32000` with a human-readable `message`.

Unknown methods also return `-32000`.

Asynchronous execution failures do not turn the original `run.start` response into an error. They are reported through `run.event` and persisted as the final run status.

## Methods

### `provider.list`

Returns registered providers and their high-level capabilities.

Request:

```json
{"jsonrpc":"2.0","id":1,"method":"provider.list","params":{}}
```

The result contains every registered provider (`github`, `gitlab`, and `azure`). All three currently expose the same capability set; only the GitHub entry is shown in full below.

```json
[
  {
    "id": "github",
    "name": "GitHub Actions",
    "description": "Discover, validate, visualize, and locally execute GitHub Actions workflows.",
    "capabilities": [
      {"name": "discover", "support": "supported"},
      {"name": "validate", "support": "supported"},
      {"name": "graph", "support": "supported"},
      {"name": "run shell steps", "support": "partial"}
    ]
  },
  {"id": "gitlab", "name": "GitLab CI/CD", "description": "Discover, validate, visualize, and locally execute GitLab CI/CD pipelines.", "capabilities": []},
  {"id": "azure", "name": "Azure Pipelines", "description": "Discover, validate, visualize, and locally execute Azure Pipelines YAML.", "capabilities": []}
]
```

### `workflow.discover`

Discovers workflows for one provider.

Parameters:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `repoPath` | string | Yes | Absolute local repository path. |
| `provider` | string | No | `github`, `gitlab`, or `azure`; defaults to `github`. |

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "workflow.discover",
  "params": {
    "repoPath": "/workspace/example",
    "provider": "github"
  }
}
```

The result is an array of workflow summaries containing `id`, `provider`, `name`, `path`, `triggers`, `jobCount`, `valid`, and `support`.

### `workflow.get`

Loads and parses one workflow.

Parameters:

| Field | Type | Required |
| --- | --- | --- |
| `repoPath` | string | Yes |
| `workflowPath` | string | Yes, relative to the repository |
| `provider` | string | No; defaults to `github` |

The result contains the workflow summary plus:

- `rawYaml`
- `jobs`
- `graph`
- `validation`
- Optional `unsupportedFeatures`

### `workflow.validate`

Uses the same parameters as `workflow.get` and returns only the validation report:

```json
{
  "valid": true,
  "support": "partial",
  "issues": [],
  "features": []
}
```

`valid=false` means `run.start` will reject the workflow.

### `run.start`

Validates, persists, and starts an asynchronous local run.

Parameters:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `repoPath` | string | Yes | Repository mounted at `/workspace`. |
| `workflowPath` | string | Yes | Repository-relative workflow path. |
| `provider` | string | No | Defaults to `github`. |
| `jobId` | string | No | Run exactly one job when supplied. |
| `eventName` | string | No | Uses a provider default when empty. |
| `inputs` | object | No | Converted to `INPUT_*` environment variables. |
| `env` | object | No | User environment variables. |
| `secrets` | object | No | Environment variables also used for log masking. |

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "run.start",
  "params": {
    "repoPath": "/workspace/example",
    "workflowPath": ".github/workflows/build.yml",
    "provider": "github",
    "eventName": "workflow_dispatch",
    "inputs": {"target": "staging"},
    "env": {"LOG_LEVEL": "debug"},
    "secrets": {"API_TOKEN": "secret"}
  }
}
```

Immediate result:

```json
{"runId":"b177a0d5-1800-4d16-b182-437af18a99b0"}
```

Execution continues asynchronously and emits `run.event` notifications.

### `run.cancel`

Parameters:

| Field | Type | Required |
| --- | --- | --- |
| `runId` | string | Yes |

Result:

```json
{"cancelled":true}
```

`cancelled=false` means the run ID was not active in the current engine process.

### `run.history`

Returns run summaries, newest first.

Parameters:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `repoPath` | string | No | Filters by exact repository path when supplied. |
| `limit` | integer | No | Defaults to 50 when outside `1..200`. |

The desktop app requests a limit of 25 for the current repository.

Persisted historical events can be retrieved with `run.get`.

### Additional execution and storage methods

- `run.prepare` reports consent requirements and returns a short-lived token after third-party-code consent.
- `run.get` returns a run record and its persisted structured events.
- `run.approve` / `run.reject` release a deployment-environment approval gate and return `{"accepted": boolean}`. `accepted=false` means no run was waiting for that decision in the current engine process.
- `artifact.list` lists Piper-managed local artifacts.
- `cache.list` / `cache.clear` inspect or clear local caches.
- `settings.get` / `settings.update` manage the defaults applied when a `run.start` field is left unset: concurrency, max expanded jobs, workspace mode, network access, mock OIDC, job/step timeouts, and memory/CPU/PID limits.
- `trust.list` / `trust.update` manage repository action trust records.

`run.start` additionally accepts `concurrency`, `maxExpandedJobs`, `workspaceMode` (`writable`, `read-only`, or `isolated`), `networkAccess` (`enabled`, `disabled`, or `internal`), `baseRef`, `preparedToken`, `mockOidc`, `mockOidcClaims`, `jobTimeoutSeconds`, `stepTimeoutSeconds`, `memoryMb`, `cpus`, and `pidsLimit`. Any of these left unset fall back to the stored settings.

## Notifications

### `run.event`

The engine emits:

```json
{
  "jsonrpc": "2.0",
  "method": "run.event",
  "params": {
    "runId": "b177a0d5-1800-4d16-b182-437af18a99b0",
    "time": "2026-01-01T12:00:00Z",
    "type": "step_log",
    "jobId": "test",
    "stepId": "step-1",
    "stream": "stdout",
    "message": "Tests passed"
  }
}
```

Event fields:

| Field | Type | Description |
| --- | --- | --- |
| `runId` | string | Persisted run ID. |
| `time` | RFC 3339 timestamp | Engine event time in UTC. |
| `type` | string | Lifecycle, support, image, or log event type. |
| `jobId` | string | Optional job scope. |
| `stepId` | string | Optional step scope. |
| `stream` | string | `stdout`, `stderr`, or `system`. |
| `status` | string | Optional run status. |
| `message` | string | Human-readable masked message. |
| `data` | object | Optional structured metadata. |

Event types, grouped by category:

- Run lifecycle: `run_started`, `run_finished`, `run_failed`, `run_cancelled`.
- Job lifecycle: `job_started`, `job_status`, `job_finished`, `job_skipped`, `job_failure_allowed`.
- Step lifecycle: `step_started`, `step_log`, `step_finished`, `step_failed`, `step_skipped`, `step_unsupported`, `step_continued`.
- Support lifecycle: `support_feature`, `support_error`, and `step_emulated`. Support payloads include `featureId`, provider, status, runtime disposition, source path/location, local behavior, hosted differences, security implications, and fallback.
- Conditions: `condition_evaluated`, `condition_evaluation_error`.
- Support and compatibility: `support_notice`, `support_feature`, `security_warning`.
- Actions and images: `action_resolved`, `image_pull`.
- Deployment approvals: `approval_required`, `approval_granted`.
- Artifacts and caches: `artifact_published`, `artifact_downloaded`, `cache_hit`, `cache_miss`, `cache_saved`.
- Services: `service_started`, `service_log`.

The Electron main process can also broadcast an internal `engine.exit` event to the renderer when the sidecar terminates. This is not a JSON-RPC notification produced by the Go server.

## Data contracts

Canonical Go contracts are in:

```text
engine/internal/pipeline/model/types.go
engine/internal/logs/events.go
```

Renderer-facing TypeScript equivalents are in:

```text
packages/shared-types/src/index.ts
```

When a protocol field changes, update both languages and run `make test`.

## Security model

The API has no authentication because it is a child-process stdio protocol. Do not expose the daemon's stdin/stdout through an untrusted network bridge without adding authentication, authorization, path validation, and resource limits.
