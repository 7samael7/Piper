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
| Workflow, job, and step `env` | Partial | Merged into the shell environment (workflow, then job, then step) without expression evaluation. |
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

### Advanced features and job execution models

| GitHub feature | Support | Local result |
| --- | --- | --- |
| Job `strategy` matrix | Supported | Static dimensions, include/exclude, fail-fast metadata, and max-parallel are normalized and expanded into independent job instances. |
| Job `services` | Supported | Started on an isolated per-job Docker network with health/startup checks. |
| Other `uses` actions | Partial | Local and consented remote JavaScript/composite actions plus `docker://` actions execute; Dockerfile actions and unsupported runtimes fail visibly. |
| Permissions and provider concurrency groups | Partial | Reported but not reproduced exactly. |
| Reusable workflow job using `jobs.<id>.uses` | Unsupported | Reported as unsupported; job execution fails. |
| `workflow_call` execution | Unsupported | Reported as unsupported. |
| Job `container` | Unsupported | Reported as unsupported; job execution fails. |
| Job `strategy` without a `matrix` | Unsupported | Reported as unsupported; job execution fails. |

Workflow-level `env` is merged into every job and can be overridden by job- and step-level `env`. Expression expansion is still not performed, so enter literal values or use Piper's Environment field.

## GitLab CI/CD

### Discovery

Piper checks:

```text
.gitlab-ci.yml
.gitlab-ci.yaml
```

It does not search other filenames. Local `include` directives are resolved recursively with cycle detection; remote, project, and template includes are reported but not fetched.

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

### Advanced features and job execution models

| GitLab feature | Support | Local result |
| --- | --- | --- |
| Local `include` | Supported | Resolved recursively with include-cycle detection. |
| `extends` and hidden templates | Supported | Resolved with reverse deep merging and cycle detection. |
| `services` | Supported | Locally emulated on a per-job Docker network. |
| `artifacts` and `cache` | Partial | Common declarations use Piper-managed local storage. |
| Remote/project/template `include` | Partial | Reported but not fetched without a future consent-aware resolver. |
| `environment`, `resource_group`, `coverage`, `retry`, `timeout` | Partial | Reported but not emulated. |
| `allow_failure` | Partial | Displayed but does not change the local conclusion. |
| `dependencies` | Unsupported | Reported but not emulated. |
| `parallel` or matrix | Unsupported | Reported as unsupported; job execution fails. |
| `trigger` child or multi-project pipeline | Unsupported | Reported as unsupported; job execution fails. |

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

### Advanced features and job execution models

| Azure feature | Support | Local result |
| --- | --- | --- |
| Matrix job `strategy` | Supported | Named Azure matrix legs expand into independent job instances. |
| Job `services` | Supported | Locally emulated on the per-job network. |
| `task` steps | Partial | Bash, PowerShell, CmdLine, Node, and artifact/cache handlers run locally; unknown tasks fail visibly. |
| PowerShell/`pwsh` steps | Partial | Execute via `pwsh` when it exists in the selected container. |
| Step templates | Partial | Reported and skipped; templates are not expanded. |
| Root `extends` template | Partial | Reported but not expanded. |
| `resources` | Partial | Reported; repositories, pipelines, packages, and containers are not fetched. |
| `timeoutInMinutes` | Partial | Displayed but not enforced. |
| `continueOnError` | Partial | Displayed but does not change the local conclusion. |
| Deployment jobs | Unsupported | Parsed for visibility; their deployment strategy causes local execution to fail. |
| Job `container` | Unsupported | Reported as unsupported; job execution fails. |

Azure `NodeTool@*` and `UseNode@*` tasks select a Node Docker image through the same setup-node path as GitHub Actions.

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
- An artifact or cache declaration that Piper cannot emulate is reported, but shell steps still run.
- An unsupported job execution model — a reusable workflow, job container, GitLab `parallel`, child pipeline, or deployment strategy — fails the job before steps begin.
- An invalid graph blocks the run completely.

Read the feature messages and live compatibility notices to distinguish these cases.
