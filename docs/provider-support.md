# Provider Support Reference

Piper maps GitHub Actions, GitLab CI/CD, and Azure Pipelines YAML into a common local model. This reference describes what the current parser recognizes and what the Docker executor actually does.

`supported` means supported within Piper's local model, not identical to a hosted runner.

## Common behavior

| Capability | Support | Local behavior |
| --- | --- | --- |
| YAML discovery and parsing | Supported | Reads recognized files from the selected repository. |
| Job graph | Supported | Builds nodes and dependency edges and rejects missing dependencies or cycles. |
| Shell steps | Supported | Runs with `/bin/bash -lc` in Docker. |
| Whole-workflow execution | Supported | Runs dependency-ready jobs concurrently within the configured limit. |
| Single-job execution | Supported | Runs only the selected job, without dependencies. |
| Repository checkout | Partial | Repository is bind-mounted at `/workspace`; provider checkout is not performed. |
| Job and step environment | Partial | Parsed values become environment variables; expression expansion is not performed. |
| Conditions and expressions | Partial | Common provider expressions are evaluated with structured errors and skip reasons; full hosted expression parity is not claimed. |
| Hosted runner parity | Partial | Local Linux images replace hosted runner environments. |
| Parallel jobs | Supported | Dependency-ready jobs run concurrently within a configurable limit. |
| Artifacts and caches | Partial | Piper-managed local emulation supports common provider declarations and actions. |
| Service containers | Supported | Services run on isolated per-job Docker networks with health/startup checks. |
| Matrix expansion | Supported | Static GitHub and Azure matrices expand into deterministic job instances. |
| Deployment environments and OIDC | Partial | Deployment approvals and clearly marked mock OIDC are locally emulated; real cloud identity is never produced. |

Unsupported actions and tasks produce actionable failures rather than being silently skipped. Unsupported job-level execution models can fail before the first step.

## GitHub Actions

### Discovery

Piper recursively discovers `.yml` and `.yaml` files below:

```text
.github/workflows/
```

### Supported or approximated

| GitHub feature | Support | Local behavior |
| --- | --- | --- |
| Workflow `name` | Supported | Displayed; filename stem is used when absent. |
| `jobs` and `needs` | Supported | Mapped to jobs and graph edges. |
| `runs-on` | Partial | Displayed only; does not choose a hosted image. |
| Job `name` | Supported | Used as graph label. |
| Job and step `env` | Partial | Added to the shell environment without expression evaluation. |
| Job `defaults.run.working-directory` | Supported | Applied to run steps without an explicit working directory. |
| Job `defaults.run.shell` | Partial | Parsed, but execution still uses Bash. |
| Step `working-directory` | Supported | Resolved below `/workspace`. |
| `run` | Supported | Executes as Bash. |
| `actions/checkout@...` | Partial | Successful no-op because the repository is mounted. |
| `actions/setup-node@...` | Partial | Chooses `node:<version>-bookworm`; caching and tool-cache behavior are not reproduced. |
| `actions/setup-dotnet@...` | Partial | Chooses a .NET SDK image; `DOTNET_ROLL_FORWARD=Major` is set for local compatibility. |
| `workflow_dispatch` inputs | Partial | Parsed for metadata; users enter values manually in Piper. |
| Other triggers | Partial | Parsed for display only. |
| Job/step `if` | Supported | Common status, boolean, comparison, string, context, and output expressions are evaluated. |
| Expressions inside scripts or env | Partial | Supported `${{ }}` values are interpolated immediately before execution. |

Only one setup-runtime image can represent a job. A job that requires conflicting Node and .NET images, or multiple conflicting versions, fails image resolution.

### Unsupported

| GitHub feature | Local result |
| --- | --- |
| Reusable workflow job using `jobs.<id>.uses` | Reported as unsupported; job execution fails. |
| Job `container` | Reported as unsupported; job execution fails. |
| Job `services` | Started on an isolated job network. |
| Job `strategy`/matrix | Static dimensions, include/exclude, fail-fast metadata, and max-parallel are normalized. |
| Other `uses` actions | Local and consented remote JavaScript/composite actions plus `docker://` actions execute; Dockerfile actions and unsupported runtimes fail visibly. |
| `workflow_call` execution | Reported as unsupported. |
| Permissions and provider concurrency groups | Reported but not reproduced exactly. |

Workflow-level `env` is not currently mapped into the neutral job model. Put values at job or step level, or enter them in Piper's Environment field.

## GitLab CI/CD

### Discovery

Piper checks:

```text
.gitlab-ci.yml
.gitlab-ci.yaml
```

It does not search other filenames or resolve `include`.

### Supported or approximated

| GitLab feature | Support | Local behavior |
| --- | --- | --- |
| Top-level jobs | Supported | Reserved keys and hidden jobs beginning with `.` are excluded. |
| `stages`/`types` | Partial | Used to infer dependencies between adjacent populated stages. |
| `needs` | Supported | Scalar and `job:` forms become graph edges. |
| Global/job `variables` | Partial | Merged into each job environment without GitLab expansion semantics. |
| Root/default/job `image` | Partial | Used as the Docker image. Other `default` keys are not expanded. |
| Runner `tags` | Partial | Displayed as runner metadata only. |
| `script` | Supported | Combined into a Bash step. |
| Global and job `before_script` | Partial | Executed as additional Bash steps. |
| Global and job `after_script` | Partial | Executed as ordinary Bash steps only if earlier steps succeeded. |
| `rules`, `only`, and `except` | Partial | Common variable, branch/tag, and local changed-file forms are evaluated. |
| `workflow: rules` | Partial | Displayed but not evaluated. |
| Pipeline source | Partial | User event becomes `CI_PIPELINE_SOURCE`. |

The selected image must provide `/bin/bash`. A common GitLab image such as Alpine does not include Bash by default and will fail local shell execution.

### Unsupported

| GitLab feature | Local result |
| --- | --- |
| Local `include` | Resolved recursively with include-cycle detection. |
| Remote/project/template `include` | Reported but not fetched without a future consent-aware resolver. |
| `extends` and hidden templates | Resolved with reverse deep merging and cycle detection. |
| `services` | Locally emulated on a per-job Docker network. |
| `parallel` or matrix | Reported as unsupported; job execution fails. |
| `trigger` child or multi-project pipeline | Reported as unsupported; job execution fails. |
| `artifacts`, `cache`, `dependencies` | Common artifacts and caches use Piper-managed local storage. |
| `environment`, `resource_group`, `coverage`, `retry`, `timeout` | Reported but not emulated. |
| `allow_failure` | Displayed but does not change the local conclusion. |

GitLab stage dependencies are an approximation. Explicit `needs: []` is respected as no dependencies, while a missing `needs` can inherit the previous populated stage.

## Azure Pipelines

### Discovery

Piper checks these root files:

```text
azure-pipelines.yml
azure-pipelines.yaml
```

It also recursively discovers YAML below:

```text
.azure-pipelines/
azure-pipelines/
pipelines/
```

### Supported or approximated

| Azure feature | Support | Local behavior |
| --- | --- | --- |
| Root `steps` pipeline | Supported | Wrapped in one synthetic job named `pipeline`. |
| Root `jobs` | Supported | Mapped directly to jobs. |
| `stages` | Partial | Stage names prefix job IDs and stage order becomes dependency edges. |
| Job/stage `dependsOn` | Supported | Mapped to graph edges where resolvable. |
| `displayName` | Supported | Used as the visible job or step name. |
| Root/stage/job `variables` | Partial | Merged into the job environment; variable groups/templates are not resolved. |
| `pool`/`vmImage` | Partial | Displayed only; the local image remains the default Ubuntu image. |
| `bash` | Supported | Executes as Bash. |
| `script` | Supported | Executes as Bash, even though hosted shell selection can differ. |
| `workingDirectory` | Supported | Resolved below `/workspace`. |
| Step `env` | Partial | Added without Azure expression expansion. |
| `checkout` | Partial | Successful no-op because the repository is mounted. |
| Job/step `condition` | Supported | Common status, equality, and boolean functions are evaluated. |
| Triggers, PRs, and schedules | Partial | Parsed for display; local event remains user-controlled. |
| `parameters` | Partial | Shown in YAML but not evaluated with template semantics. |

Stage job IDs are represented as `<stage>.<job>` so dependencies remain unique.

### Unsupported

| Azure feature | Local result |
| --- | --- |
| `task` steps | Bash, PowerShell, CmdLine, Node, artifact, and cache handlers are locally supported; unknown tasks fail. |
| Step templates | Reported and skipped; templates are not expanded. |
| PowerShell/`pwsh` steps | Execute when `pwsh` exists in the selected container. |
| Root `extends` template | Reported but not expanded. |
| `resources` | Reported; repositories, pipelines, packages, and containers are not fetched. |
| Deployment jobs | Parsed for visibility but not executed with deployment semantics; their strategy causes local execution to fail. |
| Job `container` | Reported as unsupported; job execution fails. |
| Job `services` | Locally emulated on the job network. |
| Matrix job `strategy` | Named Azure matrix legs expand locally. |
| `timeoutInMinutes` | Displayed but not enforced. |
| `continueOnError` | Displayed but does not change the local conclusion. |

## Environment and expression differences

Provider expressions are not translated into shell values:

```yaml
run: echo "${{ inputs.target }}"
```

may reach Bash unchanged and fail with shell syntax errors. For local testing, enter `target=staging` in Inputs and use:

```yaml
run: echo "$INPUT_TARGET"
```

The same principle applies to GitLab and Azure expression languages. Piper's local environment is explicit by design; it does not attempt to impersonate each provider's full expression engine.

## Interpreting runnable but unsupported workflows

The combined workflow badge answers “does every recognized feature have local support?” It does not answer “will every shell step fail?”

Typical outcomes:

- An unsupported external action fails its job with an actionable message.
- An unsupported artifact declaration is reported, but shell steps still run.
- A service container or matrix strategy fails the job before steps begin.
- An invalid graph blocks the run completely.

Read the feature messages and live compatibility notices to distinguish these cases.
