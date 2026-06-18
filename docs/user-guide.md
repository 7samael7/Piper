# User Guide

This guide covers installing Piper, navigating the desktop app, configuring a local run, understanding its execution model, managing updates and local data, and diagnosing common problems.

## Before you begin

Piper can inspect pipelines without Docker. Docker is required only when you select **Run Workflow** or **Run Job**.

Local execution is intentionally narrower than GitHub-hosted runners, GitLab runners, or Azure agents. Before running an unfamiliar pipeline:

1. Read the raw YAML.
2. Review every `partial` and `unsupported` feature in the Validation section.
3. Remember that the repository is mounted read-write.
4. Use a clean or disposable worktree for untrusted scripts.

## Installation

### macOS

Download the DMG matching your Mac from [GitHub Releases](https://github.com/7samael7/Piper/releases/latest):

- `arm64` for Apple Silicon.
- `x64` for Intel.

Open the DMG and move Piper into Applications. Some builds may be unsigned and trigger a Gatekeeper warning.

### Windows

Download `Piper-<version>-x64-Setup.exe` from [GitHub Releases](https://github.com/7samael7/Piper/releases/latest) and run it. Windows SmartScreen may warn when a build is unsigned.

### Linux

Download the `x64` package for your distribution:

- Debian, Ubuntu, Mint, and related distributions: `.deb`.
- Fedora, RHEL, Rocky Linux, AlmaLinux, openSUSE, and related distributions: `.rpm`.

Install it with your graphical package manager or the distribution's normal package command.

### From source

Install Node.js 24 (the primary CI baseline), npm, and Go 1.25+. Then:

```sh
make install
make engine
make desktop
```

To execute jobs, start a Docker-compatible daemon before launching a run.

## Interface tour

### Repository and provider sidebar

**Open Repository** opens a directory picker. Piper does not require a `.git` directory, but the selected directory should have the provider's expected pipeline files and all files used by its scripts.

The provider tabs control discovery:

- **GitHub** searches `.github/workflows/` recursively.
- **GitLab** checks the two standard root filenames.
- **Azure** checks standard root filenames and known pipeline directories.

Changing the repository or provider clears the selected workflow, job, live events, and active-run state.

Changing either one does not send a cancellation request to a run that is already executing. Cancel the active run before switching repositories or providers; otherwise the engine can continue it in the background while the UI loses its Cancel target.

### Workflow list

Each discovered workflow shows its name, provider, and combined support label. Invalid YAML can appear by filename with an `unsupported` label, but it cannot be opened or run successfully until corrected.

Piper automatically selects the first discovered workflow when no workflow is selected.

### Run history

The sidebar lists up to 25 recent local runs for the selected repository. Each entry shows:

- Workflow filename.
- Selected job or `workflow`.
- Local start time.
- Stored run status.

The current application displays run summaries only. Historical log-event reopening is not yet exposed in the UI.

### Top bar

The top bar contains:

- The selected workflow and path.
- The local event name.
- **Run Workflow** or **Run Job**.
- **Cancel** while a run is active.

While the UI retains an active run ID, it disables additional runs in that window.

### Configuration strip

Inputs, Environment, and Secrets accept newline-separated `KEY=value` text:

```text
# This is a comment
TARGET=staging
DEBUG=true
```

Parsing rules:

- Empty lines are ignored.
- Lines whose trimmed form starts with `#` are ignored.
- A line without `=`, or with an empty key, is ignored.
- Text before the first `=` is the key.
- Everything after the first `=` is the value, including additional `=` characters.
- If a key appears more than once, the last value wins.

The Secrets field obscures text on screen. It does not fetch values from a provider secret store.

### Workflow graph

Each node is a job and each edge represents a dependency. The graph supports panning, zooming, a minimap, fit controls, and movable nodes.

Select a node to inspect that job and change the run action to **Run Job**. A job-only run does not include upstream dependencies. Select the workflow in the sidebar again to clear the job selection.

GitLab and Azure stage ordering is converted into dependency edges when explicit dependencies are absent. This is an approximation of hosted stage scheduling.

### Job and YAML inspector

The **Job** tab shows:

- Workflow provider, path, job count, and support.
- Selected job ID, runner/pool, stage, image, dependencies, and steps.
- Blocking validation issues.
- Every recognized feature and its local support message.

The **YAML** tab is a read-only view. Edit the actual file in your editor, then reopen the repository or switch away from and back to the provider to reload discovery and workflow data.

### Live logs

The terminal displays lifecycle events, compatibility notices, image-pull output, stdout, stderr, and final status. Entries include a local display time and, when relevant, a job and step scope.

Starting a new run clears the visible log stream. Events are also persisted to SQLite.

## Support and validation

Support labels describe local behavior:

| Label | Meaning |
| --- | --- |
| `supported` | Piper implements the feature for its local execution model. |
| `partial` | Piper displays or approximates the feature, but behavior differs from hosted CI. |
| `unsupported` | Piper does not emulate the feature. |

Workflow support is cumulative. One unsupported feature can make the workflow badge `unsupported`, even when other jobs or steps remain locally runnable.

Blocking validation errors currently include:

- No jobs.
- A dependency on a missing job.
- A dependency cycle.

Warnings and unsupported features do not always block the **Run** button. Review the Validation section to determine whether a run will fail, skip a step, or merely differ from hosted CI.

## Configuring a run

### Event name

Piper defaults to:

| Provider | Default | Environment variable |
| --- | --- | --- |
| GitHub | `workflow_dispatch` | `GITHUB_EVENT_NAME` |
| GitLab | `web` | `CI_PIPELINE_SOURCE` |
| Azure | `manual` | `BUILD_REASON` |

You can enter any value. It is passed to scripts but does not cause Piper to evaluate triggers, `if`, `rules`, `only`, `except`, or Azure conditions.

### Inputs

Inputs become environment variables prefixed with `INPUT_`. Names are uppercased, and characters outside `A-Z`, `a-z`, `0-9`, and `_` become `_`.

```text
release-channel=beta
```

becomes:

```text
INPUT_RELEASE_CHANNEL=beta
```

Piper does not automatically populate defaults declared in `workflow_dispatch`; enter the desired value explicitly.

### Environment

Environment entries are added to every shell step using the names supplied:

```text
LOG_LEVEL=debug
FEATURE_FLAG=true
```

### Secrets

Secrets are also passed as environment variables:

```text
API_TOKEN=secret-value
```

Piper masks exact secret values from emitted stdout, stderr, and image-pull messages. Masking has limits:

- Values shorter than three characters are not masked.
- Encoded, hashed, transformed, or split secrets are not recognized.
- Common secret values of three or more characters may unintentionally mask matching ordinary text.
- A process inside the container still has access to the original value.
- Docker or operating-system diagnostics outside Piper's event stream may expose data independently.

### Environment precedence

When the same variable name is defined more than once, later sources win:

1. Piper built-ins.
2. Inputs converted to `INPUT_*`.
3. User Environment entries.
4. User Secrets entries.
5. Parsed provider/job variables.
6. Step environment variables.

This means a job or step can override a value entered in Piper, including a secret with the same name.

### Built-in variables

Every provider receives:

| Variable | Value |
| --- | --- |
| `CI` | `true` |
| `PIPER_PROVIDER` | `github`, `gitlab`, or `azure` |
| `PIPER_EVENT` | The configured event name |

Provider-specific variables:

| GitHub | GitLab | Azure |
| --- | --- | --- |
| `GITHUB_ACTIONS=false` | `GITLAB_CI=false` | `TF_BUILD=false` |
| `GITHUB_WORKSPACE=/workspace` | `CI_PROJECT_DIR=/workspace` | `BUILD_SOURCESDIRECTORY=/workspace` |
| `GITHUB_EVENT_NAME=<event>` | `CI_PIPELINE_SOURCE=<event>` | `BUILD_REASON=<event>` |

The `false` values intentionally signal that this is not the real hosted environment.

## Running a workflow

Select **Run Workflow** to execute every parsed job in dependency order.

Piper runs dependency-ready jobs concurrently up to the configured limit. Failures do not stop unrelated jobs; downstream conditions determine whether dependents run or are skipped.

### Running one job

Select a graph node and choose **Run Job**. Piper runs exactly that job:

- Dependencies are not run first.
- Dependency outputs and artifacts are not available.
- The job still receives the mounted repository and configured environment.

This mode is useful for fast iteration on self-contained jobs.

### Cancelling

Select **Cancel** to cancel the active run. If a shell step is running, Piper kills the active job container and records the run as `cancelled`.

Cancellation does not undo changes already made to the bind-mounted repository.

## Container behavior

### Image selection

Piper chooses an image in this order:

1. GitLab job or global/default `image`, when parsed.
2. A supported GitHub setup-action image.
3. `ubuntu:22.04`.

GitHub runtime mappings:

- `actions/setup-node` → `node:<version>-bookworm`
- `actions/setup-dotnet` → `mcr.microsoft.com/dotnet/sdk:<version>`

Piper supports one setup runtime image per job. If setup actions require conflicting images, the job fails before execution.

`runs-on` and Azure `pool` are display metadata only. A workflow that names Windows or macOS still runs in a Linux container.

### Shell and filesystem

Every `run`, GitLab script block, Azure `bash`, or Azure `script` step runs as:

```text
/bin/bash -lc <script>
```

The configured image must include `/bin/bash`. If it does not, use a Bash-capable image for local execution.

The repository is mounted at `/workspace`. Relative working directories are resolved below that path. GitHub `defaults.run.working-directory` and parsed step working directories are honored. Declared shell values do not change the Bash executor.

All steps in one job share a container, so files and installed packages survive between that job's steps. Each later job receives a fresh container but sees any changes written to the mounted repository.

### Network and image pulls

Docker pulls the selected image before each job. Docker may reuse its local layer cache. Containers use Docker's standard network configuration, so scripts can make network requests unless the daemon or host restricts them.

## Run history and local data

The desktop app places `piper.db` in Electron's user-data directory. Typical locations are:

| Platform | Typical database path |
| --- | --- |
| macOS | `~/Library/Application Support/Piper/piper.db` |
| Windows | `%APPDATA%\Piper\piper.db` |
| Linux | `~/.config/Piper/piper.db` |

The exact directory can vary with operating-system or Electron configuration.

To choose another database:

```sh
PIPER_DB=/absolute/path/to/piper.db make desktop
```

Close Piper before moving or deleting the database and its optional `-wal` and `-shm` companion files.

The standalone Go engine uses `~/.piper/piper.db` when `PIPER_DB` is not set. The desktop app overrides that default.

## Updates

Packaged macOS, Windows, and Linux builds check the configured GitHub repository for a newer release at startup. Use the refresh icon in the lower-left corner to check again.

When an update is available, Piper:

1. Selects the installer matching the current operating system and architecture.
2. Downloads it to the user's Downloads directory.
3. Verifies the accompanying SHA-256 checksum when one is published.
4. Opens the installer with the operating system.

Piper does not silently install or replace the running app.

For a private release repository, launch Piper with a fine-grained GitHub token that has Contents read access:

```sh
PIPER_UPDATE_TOKEN=github_pat_... /Applications/Piper.app/Contents/MacOS/Piper
```

On Windows or Linux, set the same environment variable before launching Piper using that platform's normal shell syntax.

## Troubleshooting

### No workflows found

- Confirm the correct provider tab is selected.
- Confirm the filename is in one of the documented discovery locations.
- Confirm the extension is `.yml` or `.yaml`.
- GitLab discovery does not follow `include`; the root file itself must exist.
- Reselect the repository or provider after adding a file.

### The workflow is listed but cannot be opened

The YAML may be malformed or use a root shape Piper cannot parse. Validate it with a YAML-aware editor and review the Electron/engine console when running from source.

### Run is disabled

- Select a workflow.
- Wait for the current run to finish or cancel it.
- If a job is selected, the button reads **Run Job**.

### Docker is unavailable

Start Docker Desktop, OrbStack, Colima, or another compatible daemon. Piper checks:

- `DOCKER_HOST`.
- The active Docker CLI context.
- Docker's default endpoint.
- Common Docker Desktop, OrbStack, Colima, Rancher Desktop, and Linux runtime sockets.

If a nonstandard daemon is in use, set `DOCKER_HOST` before starting Piper.

### `/bin/bash` is missing

The selected image does not contain Bash. Alpine images commonly have `/bin/sh` only. Use an Ubuntu, Debian, Bash-enabled, or otherwise compatible image.

### A `${{ ... }}` expression fails or prints literally

Common provider expressions are evaluated and `${{ }}` values are interpolated before execution. Unsupported syntax produces a structured evaluation error.

### A conditional job ran unexpectedly

Common GitHub `if`, GitLab `rules`/`only`/`except`, and Azure `condition` forms are evaluated. The graph and inspector show results and skip reasons.

### An external action or task did not run

Unsupported GitHub actions and Azure tasks fail visibly. Local and consented remote JavaScript/composite actions plus common Azure script, Node, artifact, and cache tasks have local handlers.

### A job fails before its first step

Check whether it declares a reusable workflow, container, services, matrix/parallel strategy, child pipeline, or deployment strategy. These job execution models are not locally supported.

### Files changed after a run

This is expected when pipeline scripts write below `/workspace`: that path is the opened repository. Inspect changes with version control and use a disposable worktree for destructive pipelines.

### Image pull fails

Verify the image name, registry access, credentials configured in Docker, network connectivity, and architecture compatibility.

### Runtime setup reports an unsupported version

Use one concrete version expression:

- Node: `20`, `20.x`, or `20.11.1`
- .NET: `8.0.x` or `8.0.100`

Multiple versions and non-version expressions cannot be mapped to one local image.

For feature-level details, continue to the [Provider Support Reference](provider-support.md).
