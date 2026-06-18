# Phase 0 Feature-Support Audit

This audit records the evidence reviewed for Phase 0. Parser recognition alone is not counted as support: parsing, normalization, validation, planning, runtime behavior, structured events, UI representation, cancellation/cleanup, tests, and documentation were considered separately.

## 1. Current architecture

Piper is an Electron/React desktop application connected through an isolated preload bridge to a Go JSON-RPC sidecar. Provider adapters normalize YAML into a shared workflow model. Graph and plan packages validate dependencies and expand static matrices; the scheduler runs dependency-ready instances; the Docker executor manages workspaces, images, services, actions/tasks, shell execution, artifacts, caches, conditions, outputs, cancellation, and cleanup. SQLite stores run summaries and structured events.

Phase 0 establishes `engine/internal/support/registry.json` as the support contract. Provider parsers emit stable feature references; validation resolves them into full records; the API, compatibility events, UI badges, runtime guards, generated provider reference, and CI synchronization checks consume the same records.

## 2. Features proven by tests

- Provider discovery and core GitHub, GitLab, and Azure parsing fixtures.
- Missing/cyclic dependency rejection, graph construction, static GitHub/Azure matrix expansion, expansion limits, and bounded scheduling.
- A subset of provider-aware condition parsing, evaluation, and interpolation.
- GitLab local includes, nested include-cycle rejection, hidden templates, and `extends` merging.
- Workspace traversal/symlink checks, artifact/cache path restrictions, Docker endpoint selection, runtime-image mapping, and SQLite persistence.
- Registry schema validation, generated-document synchronization, status/runtime golden synchronization, runtime rejection for every `reject` entry, structured compatibility payloads, and six-state badge contracts.
- Explicit rejection of empty jobs, non-executable steps, ambiguous steps, unknown Azure tasks, unsupported provider job policies, templates, containers, child/reusable pipelines, and unknown recognized keys covered by fixtures.

## 3. Features claimed only by documentation or not proven end to end

- JavaScript, composite, and Docker action execution across all pre/main/post and cancellation paths.
- Deployment approval, restricted networking, mock OIDC, and cleanup after cancellation.
- PostgreSQL/Redis service interaction in the normal CI workflow; the Docker integration test remains environment-gated.
- Cross-platform desktop packaging, IPC, path handling, and workspace behavior.
- Renderer accuracy for all badges and historical run/event reopening.

These remain documented as partial, emulated, or unavailable rather than exact hosted behavior.

## 4. Partially implemented features

- GitHub environment precedence, expressions, outputs, matrices, services, setup actions, local/remote actions, and `continue-on-error`.
- GitLab variables, images/tags, scripts, rules, defaults, artifacts/caches, services, manual jobs, and `allow_failure`.
- Azure variables, conditions, matrices, script/runtime tasks, storage tasks, services, deployment jobs, and shell selection.
- Provider runner labels/pools are inspection metadata; local Linux images do not reproduce hosted runners.
- Artifacts, caches, approvals, checkout, services, and deployment behavior are deterministic local emulations with explicit hosted differences.

The generated provider reference contains the exact parser contract, runtime disposition, local behavior, hosted difference, security note, fallback, and test paths for each record.

## 5. Features silently ignored

The audit found and corrected these false-success paths:

- Empty jobs could complete successfully without executing anything.
- Non-mapping and empty steps could reach a generic fallback without a stable support identifier.
- A step with multiple executable forms could run the first form and silently ignore another.
- GitLab top-level `cache` and `services` defaults were accepted but not applied.
- Azure `NodeTool`/`UseNode` normalized to setup-node image selection but then fell through as an unsupported step.
- Unsupported downloaded action runtimes and Dockerfile actions failed without the registry-backed `support_error` contract.

Remaining unknown keys are rejected at the recognized workflow/job/step scopes. GitLab top-level mappings are inherently ambiguous because ordinary jobs are also top-level mappings; unrecognized job keys are rejected once a mapping is classified as a job.

## 6. Documentation contradictions

Corrected contradictions included:

- `docs/engine-api.md` still described the removed three-state `supported`/`partial`/`unsupported` schema and identical provider capability lists.
- Development documentation said the workspace was always read-write and shell execution was always Bash despite read-only/isolated modes and PowerShell handling.
- Architecture documentation said only partial/unsupported compatibility events were emitted, while runtime emits every non-`supported-local` feature.
- The generated provider reference omitted parser contracts, security implications, and test evidence even though those fields were required registry data.

Hosted CI is now consistently described as authoritative, and no generated support row claims exact hosted-runner parity.

## 7. Security risks

- Pipeline scripts, actions, images, and services are arbitrary code; containers are not a perfect sandbox.
- Writable workspace and outbound networking remain compatibility defaults.
- Containers commonly run as root, and crash/orphan recovery is absent.
- Secret masking is exact-value and best effort; transformed, encoded, split, or short values may escape.
- Remote actions execute after consent and SHA resolution, but mutable references still require careful review.
- Docker socket restrictions, archive-abuse coverage, resource-exhaustion coverage, and persisted cleanup recovery remain incomplete.

Use isolated workspace mode and internal/disabled networking for unfamiliar pipelines.

## 8. Testing gaps

- No normalized-model golden suite or compact provider conformance fixture suite.
- Incomplete variable-precedence, output-chain, expression-coercion, retry, timeout, and cancellation suites.
- Docker integration coverage is not guaranteed in the primary CI workflow.
- No renderer component tests for support badges, compatibility details, or run history.
- No macOS/Windows filesystem, IPC, or packaging test matrix.
- Related registry tests are verified as real test files, but most features still need dedicated behavioral assertions rather than shared package-level evidence.

## 9. Recommended implementation phases

1. Phase 1: inspection-only preflight plan, condition traces, historical event reopening, debug bundles, targeted reruns, and granular cancellation.
2. Phase 2: provider-specific expressions, variables, outputs, and failure semantics.
3. Phase 3: reusable workflows, includes/templates, provenance, limits, and unified remote consent.
4. Phases 4–10: execution topology, actions/tasks, runner profiles, event simulation, artifacts/caches, security/isolation, and authoring tools.

Phase 1 must consume the support registry and must not start Docker during preflight.

## 10. Exact files likely to change

Phase 0 changed or verified:

- `engine/internal/support/registry.json`, `registry.go`, `docs.go`, registry tests, and contract golden.
- `engine/internal/pipeline/model/types.go` and `engine/internal/pipeline/validation/validation.go`.
- GitHub, GitLab, and Azure provider adapters and tests.
- `engine/internal/executor/docker/executor.go` and tests.
- `engine/internal/api/server.go`, API types/tests, and shared TypeScript contracts.
- Desktop support badges/inspector styles and components.
- `docs/provider-support.md`, architecture, security, user guide, troubleshooting, development, Engine API, README, this audit, and the Phase 1 roadmap.
- `Makefile` and `.github/workflows/ci.yml` for synchronization checks.

The likely Phase 1 write set is `engine/internal/pipeline/plan`, `engine/internal/expression`, `engine/internal/api`, `engine/internal/persistence`, structured log contracts, shared TypeScript types, desktop inspector/history/log components, and new preflight/debug-bundle tests and documentation.
