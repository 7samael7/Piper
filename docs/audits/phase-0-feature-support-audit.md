# Phase 0 Feature-Support Audit

This audit records the implementation state at the start of the truthful support-contract work. A parser recognizing a key is not treated as proof that Piper validates, plans, executes, logs, displays, cancels, cleans up, tests, or documents that feature correctly.

## Architecture

Piper is an Electron/React desktop application connected through an isolated preload bridge to a Go JSON-RPC sidecar. Provider adapters parse YAML into a neutral workflow model. The graph and plan packages validate dependencies and expand static matrices; the scheduler runs ready job instances; the Docker executor manages images, job/service containers, actions, artifacts, caches, conditions, outputs, cancellation, and cleanup. SQLite stores runs and structured events.

Phase 0 adds `engine/internal/support/registry.json` as the contract joining parser occurrences, validation, API capabilities, compatibility events, UI badges, runtime rejection, and generated documentation.

## Layer-by-layer findings

| Area | Proven by tests | Partial or documentation-only behavior | Gaps and risks |
| --- | --- | --- | --- |
| Model and parsing | Basic GitHub, GitLab, and Azure jobs, steps, dependencies, variables, matrices, and selected metadata | Many provider keys were retained only as booleans or messages | Parser recognition previously implied support without a stable identifier or runtime contract |
| Composition | GitLab local includes, reverse deep `extends`, and cycle detection | Remote includes were retained but not fetched; GitHub reusable workflows and Azure templates were only detected | Source provenance through nested includes is incomplete |
| Validation | Missing jobs, dependency errors/cycles, and expression syntax | Feature messages were duplicated in parsers and validation | No registry synchronization, unknown-key policy, or runtime-classification conformance existed |
| Planning and graph | Static GitHub/Azure matrices, expansion limits, and graph construction | Provider-specific scheduling remains approximate | Provider loaders previously swallowed plan-compilation errors |
| Expressions and variables | A small condition/interpolation subset for all providers | Context construction, precedence, coercion, outputs, rules, and status semantics are incomplete | Unsupported syntax can only be tested case by case; secrets in derived values remain difficult to redact |
| Runtime | Shell jobs, selected actions/tasks, services, artifacts, caches, timeouts, cancellation, approvals, and local workspace modes exist in code | Docker/action/task behavior has limited automated execution coverage | Empty steps could silently succeed; unsupported job policy keys could be ignored; containers are not a perfect sandbox |
| Structured logs | Run/job/step lifecycle, conditions, services, artifacts, caches, approvals, and support notices | Historical reopening is available in the API but not the desktop | Compatibility events lacked stable IDs and complete contract metadata |
| Desktop UI | Workflow graph, inspector, badges, live events, settings, artifacts/caches, and run summaries | No automated renderer tests | Three support states could not distinguish emulation, validation-only behavior, or consent |
| Cancellation and cleanup | Scheduler cancellation and Docker cleanup paths exist | Only service startup has an integration test; crash recovery is absent | Orphaned containers, networks, workspaces, locks, and downloads are not recovered after crashes |
| Persistence | Runs, events, job/step states, settings, and trust records are tested | UI does not reopen historical events | Debug bundles and long-term cleanup policies are absent |
| Security | Workspace traversal/symlink checks, artifact/cache restrictions, exact-value masking, consent, and network/workspace modes | Writable/networked defaults, root containers, mutable action trust, and weak transformed-secret masking remain | No Docker-socket policy, archive hardening suite, orphan recovery, or comprehensive resource-exhaustion tests |

## Features proven by tests

- Provider discovery/parsing fundamentals and selected support classification.
- GitLab local include and `extends` resolution with cycle detection.
- Static matrix expansion and dependency fan-out.
- Dependency graph validation and bounded scheduler concurrency.
- Selected expression functions and interpolation.
- Action metadata loading and remote-consent rejection.
- Artifact/cache workspace path enforcement.
- Docker endpoint discovery and runtime-image mapping.
- SQLite run/event/settings persistence.

## Claims not yet proven end to end

- JavaScript, composite, and Docker action execution across pre/main/post phases.
- Deployment approval, restricted networking, mock OIDC, and cancellation cleanup.
- Full PostgreSQL/Redis service interaction in normal CI.
- Cross-platform desktop packaging, IPC, paths, and workspace behavior.
- Historical event reopening, artifact reveal behavior, and renderer badge accuracy.

## Previously silent or contradictory behavior

- GitLab `dependencies`, `resource_group`, `coverage`, `retry`, and `timeout` were detected but could be ignored by execution.
- Azure parameters, templates, resources, and provider timeout declarations did not produce a single authoritative runtime decision.
- GitHub permissions, concurrency, workflow-level defaults, and unknown keys had no stable support record.
- Empty steps emitted a running-status unsupported event and allowed the job to succeed.
- GitLab local includes/`extends`, `allow_failure`, Azure deployments, `continueOnError`, and expression interpolation were described inconsistently across code and documentation.

Phase 0 resolves these by assigning stable feature IDs and making every `reject` disposition fail with `support_error`; emulated checkout now emits `step_emulated`.

## Testing gaps

Future phases still need golden normalized models, exhaustive parser fixtures, output/precedence suites, retry/timeout tests, enabled Docker integration jobs, action/task execution fixtures, security abuse cases, renderer tests, and macOS/Windows path and packaging tests.

## Recommended phases

1. Phase 1: inspectable execution plans, condition traces, historical logs, debug bundles, targeted reruns, and cancellation controls.
2. Phase 2: provider-aware expressions, variables, outputs, and failure semantics.
3. Phase 3: reusable workflows, includes, templates, and provenance.
4. Phases 4–10: topology, actions/tasks, runner profiles, events, storage, isolation, and authoring tools.

## Primary Phase 0 files

- `engine/internal/support/registry.json`, registry loader, generator, and tests.
- Provider adapters, neutral model, validation, API compatibility events, and Docker runtime guard.
- Shared TypeScript contracts, support badges/inspector, generated provider reference, CI checks, and this audit.
