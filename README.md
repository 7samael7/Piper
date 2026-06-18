# Piper

Piper is an MVP desktop application for DevOps engineers who want to inspect and run CI/CD pipelines locally. The first-class providers are GitHub Actions, GitLab CI/CD, and Azure Pipelines YAML, all mapped through provider-neutral engine contracts.

The workbench is intentionally honest about local execution. It does not claim parity with hosted CI runners. Validation and run output mark features as supported, partially supported, or unsupported.

## What is included

- Electron Forge desktop app with React, TypeScript, Vite, Monaco Editor, React Flow, xterm.js, Zustand, and TanStack Query.
- Go sidecar engine launched by Electron over newline-delimited JSON-RPC on stdin/stdout.
- GitHub Actions workflow discovery under `.github/workflows`.
- GitLab CI/CD discovery from `.gitlab-ci.yml` or `.gitlab-ci.yaml`.
- Azure Pipelines discovery from `azure-pipelines.yml`, `azure-pipelines.yaml`, `.azure-pipelines/`, `azure-pipelines/`, or `pipelines/`.
- YAML parsing, graph construction, validation, and unsupported feature reporting.
- Local Docker execution for shell `run` steps, with sequential job execution for the MVP.
- Real-time structured JSON log events and cancellation via `context.Context`.
- SQLite-backed run history.

## Quick start

```sh
make install
make engine
make desktop
```

The desktop app can open a local repository. To try the bundled samples, open one of:

```text
examples/github-actions
examples/gitlab-ci
examples/azure-pipelines
```

Docker Desktop, OrbStack, Colima, or another Docker-compatible daemon must be running to execute jobs locally. The engine checks `DOCKER_HOST`, the active Docker CLI context, and common local socket paths. Discovery, validation, graph visualization, and history work without Docker.

## GitHub releases

The root GitHub Actions workflows run Go and desktop checks on branches and pull requests. The `Release` workflow builds Intel and Apple Silicon DMGs, generates SHA-256 checksum files, and publishes them to a GitHub Release.

Create a release by pushing a semantic-version tag:

```sh
git tag v0.2.0
git push origin v0.2.0
```

Alternatively, open **Actions → Release → Run workflow** and enter a version such as `0.2.0`. The workflow sets the packaged application version automatically; the version does not need to be edited in the repository first.

Packaged builds check the repository's latest GitHub Release and offer the architecture-matching DMG. Public repositories work without credentials. Private repositories can provide a fine-grained token with Contents read access through `PIPER_UPDATE_TOKEN` when launching the app.

Unsigned DMGs are produced when no Apple credentials are configured. For trusted distribution, configure these GitHub Actions secrets:

- `APPLE_CERTIFICATE`: Base64-encoded Developer ID Application `.p12` certificate.
- `APPLE_CERTIFICATE_PASSWORD`: Password for the `.p12` certificate.
- `APPLE_SIGN_IDENTITY`: Certificate identity, such as `Developer ID Application: Example, Inc. (TEAMID)`.
- `APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD`, and `APPLE_TEAM_ID`: Optional notarization credentials; configure all three together.

## Current Provider Support

Supported locally for all providers:

- Workflow discovery and YAML parsing.
- Job dependency graph visualization.
- Shell steps inside Docker with the repository mounted at `/workspace`; the default image is `ubuntu:22.04`.
- GitLab job `image` values as per-job Docker images when present.
- User-provided event name, inputs, environment variables, and secrets.
- Secret masking in emitted logs.

Partially supported:

- Job-level and step-level environment variables.
- GitHub `actions/checkout` and Azure `checkout` as local no-ops, because the repository is already mounted.
- GitHub `actions/setup-dotnet` and `actions/setup-node` through matching per-job SDK/runtime Docker images. Action caching and hosted tool-cache behavior are not emulated; .NET framework roll-forward may be used when the selected SDK image lacks an older target runtime.
- GitHub job-level `defaults.run.working-directory` and `defaults.run.shell`.
- GitLab stage ordering and Azure stage ordering are approximated through dependency edges.
- Provider expression syntax and conditional rules are preserved and reported, but not fully evaluated.
- Workflow-level triggers are parsed for display and local run configuration only.

Unsupported in the MVP:

- GitHub reusable workflow jobs with `jobs.<id>.uses`.
- GitLab `include`, `extends`, child pipelines, artifacts, cache, services, and parallel expansion.
- Azure templates, tasks, resources, deployment jobs, job containers, services, and strategy matrix expansion.
- Hosted runner image parity, artifacts, cache, OIDC, deployment environments, concurrency, permissions, and hosted service semantics.

See [docs/architecture.md](docs/architecture.md) for the implementation architecture.
