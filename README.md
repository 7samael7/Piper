<p align="center">
  <img src="apps/desktop/assets/piper.png" alt="Piper logo" width="150">
</p>

# Piper

Piper is a desktop workbench for inspecting, validating, visualizing, and running CI/CD pipelines on your own machine.

Open a repository, choose GitHub Actions, GitLab CI/CD, or Azure Pipelines, and Piper translates the pipeline into one provider-neutral job graph. Shell steps can then run in local Docker containers while structured logs stream back to the desktop app.

> [!IMPORTANT]
> Piper is a local approximation, not a hosted-runner emulator. Review the support labels before running a pipeline. Conditions, cloud services, credentials, runner images, and many provider-specific features do not behave exactly as they do in hosted CI.

## What Piper does

- Discovers supported pipeline YAML files in a local repository.
- Builds an interactive dependency graph from jobs, stages, and `needs`/`dependsOn`.
- Shows jobs, steps, raw YAML, validation issues, and feature-level support.
- Runs an entire workflow or one selected job in Docker.
- Accepts an event name, inputs, environment variables, and secrets for a local run.
- Streams stdout, stderr, lifecycle events, and compatibility notices.
- Masks supplied secret values in emitted logs.
- Stores local run summaries and events in SQLite.
- Checks GitHub Releases for an installer matching the current operating system and architecture.

## Install

[Download the latest Piper release](https://github.com/7samael7/Piper/releases/latest)

Automated releases publish:

- macOS DMGs for Apple Silicon (`arm64`) and Intel (`x64`).
- A Windows Squirrel Setup executable for `x64`.
- Linux packages for `x64` in Debian/Ubuntu (`.deb`) and Fedora/RHEL (`.rpm`) formats.

Some releases may be unsigned. macOS Gatekeeper and Windows SmartScreen can warn about unsigned applications; only bypass an operating-system warning if you trust the downloaded build and its source.

### Run from source

Requirements:

- Node.js 24 (the primary CI baseline)
- npm
- Go 1.25 or newer
- Docker Desktop, OrbStack, Colima, or another Docker-compatible daemon for local execution

Docker is not required to discover, inspect, validate, or visualize workflows.

```sh
git clone https://github.com/7samael7/Piper.git
cd Piper
make install
make engine
make desktop
```

After the first installation, this shorter command rebuilds the engine and launches the desktop app:

```sh
./scripts/dev.sh
```

## Your first local run

1. Start Piper and select **Open Repository**.
2. Choose the **GitHub**, **GitLab**, or **Azure** provider tab.
3. Select a discovered workflow.
4. Review its graph, validation report, and support badges.
5. Optionally enter an event name, inputs, environment variables, or secrets.
6. Select **Run Workflow**.
7. Watch the **Live Logs** panel and cancel the run if needed.

Clicking a job node changes the action to **Run Job**. A job-only run executes that job by itself; it does not execute its dependencies first. To return to a workflow run, select the workflow in the sidebar again.

The repository contains examples for each provider:

```text
examples/github-actions
examples/gitlab-ci
examples/azure-pipelines
```

See the [User Guide](docs/user-guide.md) for a tour of every part of the interface and the local execution model.

## Run configuration

The Inputs, Environment, and Secrets fields accept one `KEY=value` entry per line. Blank lines and lines beginning with `#` are ignored.

```text
# Inputs become INPUT_* variables
target=staging

# Environment and secrets keep their supplied names
LOG_LEVEL=debug
API_TOKEN=replace-me
```

Input names are uppercased and non-alphanumeric characters become underscores, so `release-channel=beta` becomes `INPUT_RELEASE_CHANNEL=beta`.

Default event names are:

| Provider | Default event |
| --- | --- |
| GitHub Actions | `workflow_dispatch` |
| GitLab CI/CD | `web` |
| Azure Pipelines | `manual` |

The event name configures local variables such as `GITHUB_EVENT_NAME`, `CI_PIPELINE_SOURCE`, or `BUILD_REASON` and feeds supported job/step conditions. It does not prove that the hosted provider would trigger the pipeline.

## Provider discovery

| Provider | Files Piper discovers |
| --- | --- |
| GitHub Actions | Any `.yml` or `.yaml` file below `.github/workflows/` |
| GitLab CI/CD | `.gitlab-ci.yml` and `.gitlab-ci.yaml` at the repository root |
| Azure Pipelines | Root `azure-pipelines.yml`/`.yaml`, plus YAML below `.azure-pipelines/`, `azure-pipelines/`, and `pipelines/` |

All three providers support YAML inspection, graph construction, validation, and Bash-based Docker execution. Provider-specific behavior and limitations are documented in the [Provider Support Reference](docs/provider-support.md).

## Understanding support labels

Piper labels workflows, jobs, steps, and individual features:

- `supported-local`: implemented for Piper's local model.
- `emulated`: deterministic local substitute with documented hosted differences.
- `partial`: executable or represented with missing semantics.
- `validation-only`: inspected but never allowed to affect execution.
- `requires-consent`: remote code that cannot run without explicit approval.
- `unsupported`: rejected rather than silently skipped.

Support classifications come from the machine-readable registry used by validation, events, badges, and the generated provider reference. Unsupported executable behavior produces a structured error.

## How local execution works

- Piper pulls a Docker image and creates one container per job.
- Every step in a job runs in the same container.
- Jobs run through a dependency-aware scheduler with configurable concurrency.
- Shell commands run through `/bin/bash -lc` by default; steps that declare `pwsh` or `powershell` run through `pwsh` instead.
- The repository is bind-mounted at `/workspace`.
- A failed shell step stops that job; unrelated jobs still run, and the run's final status is failed.
- Common GitHub, GitLab, and Azure job/step conditions are evaluated and false conditions produce explicit skip reasons.
- Static GitHub and Azure matrices expand into independently selectable graph nodes.
- Service containers, local artifacts, and local caches are emulated with Piper-managed Docker networks and storage.
- Unsupported actions and tasks fail visibly. Remote GitHub actions require explicit consent before download or execution.

> [!WARNING]
> The bind mount is writable. Pipeline commands can modify or delete files in the opened repository. Review untrusted YAML before running it, and use a disposable worktree when appropriate.

## Images and runtimes

- The default image is `ubuntu:22.04`.
- A GitLab job's `image` is used when present.
- GitHub `actions/setup-node` and `actions/setup-dotnet` select a matching Node or .NET SDK image when Piper can map the requested version.
- GitHub `runs-on` and Azure `pool` values are shown but do not select a hosted runner image.
- Every selected image must contain `/bin/bash`; minimal images such as Alpine often do not.

## Data and secrets

The desktop app stores run metadata and masked events in a local SQLite database. The default packaged-app database is `piper.db` in Electron's per-user application-data directory. Set `PIPER_DB` before launching Piper to use another path.

Run inputs, environment variables, and secrets are not written to the run record. They are passed to the job container as environment variables for the active run. Exact secret values of at least three characters are replaced in emitted logs, but masking cannot protect shorter, transformed, encoded, split, or otherwise altered values.

Treat locally executed pipelines as trusted code. Containers can access the mounted repository, their configured environment, and Docker's normal network.

## Documentation

- [User Guide](docs/user-guide.md) — installation, interface, running pipelines, updates, data, and troubleshooting
- [Provider Support Reference](docs/provider-support.md) — generated GitHub, GitLab, and Azure compatibility contract
- [Phase 0 Audit](docs/audits/phase-0-feature-support-audit.md) — evidence, contradictions, risks, and test gaps
- [Development Guide](docs/development.md) — setup, commands, packaging, testing, and releases
- [Architecture](docs/architecture.md) — desktop, engine, persistence, and execution design
- [Engine API](docs/engine-api.md) — newline-delimited JSON-RPC protocol and methods
- [Security](docs/security.md) — trust, workspaces, networking, secrets, and mock identity
- [Troubleshooting](docs/troubleshooting.md) — Docker, actions, services, and local storage

## Common development commands

```sh
make install       # Install Node and Go dependencies
make engine        # Build engine/bin/piper-engine
make desktop       # Start Electron Forge in development mode
make test          # Run Go tests, shared-types build, and desktop typecheck
make dmg           # Build the engine and create a local macOS package
make linux         # Create x64 DEB and RPM packages on Linux
make windows       # Create an x64 Setup executable on Windows
make clean         # Remove generated build output and dependencies
```

## Current boundaries

Piper remains a local approximation. Consult the generated support reference for the exact parser and runtime contract; no parsed key should be assumed executable merely because it appears in the graph or inspector.

If local behavior and hosted CI disagree, hosted CI remains the source of truth.
