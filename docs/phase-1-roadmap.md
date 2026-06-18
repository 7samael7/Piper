# Phase 1 Roadmap

## Current gap

Piper can validate and execute a normalized workflow, but users cannot inspect the complete resolved execution decision before Docker starts. Condition events are result-oriented rather than full traces, historical events are not reopened in the desktop, and targeted rerun/cancellation modes are limited.

## Smallest coherent design

1. Add an inspection-only preflight API returning raw/resolved YAML, normalized model, expanded jobs, dependency graph, images, conditions, unresolved expressions, services, storage expectations, consent requirements, and support records without connecting to Docker.
2. Persist expression traces with original/interpolated forms, operands, relevant redacted contexts, results, errors, and skip reasons.
3. Reopen historical structured events, states, matrix values, images, artifacts, caches, cancellation details, and cleanup errors.
4. Add redacted debug-bundle export.
5. Add explicit run-selection modes for a job alone, transitive dependencies, downstream dependents, failed jobs, and safe restart-from-step behavior.
6. Add queued-job, running-job, and workflow cancellation controls with deterministic cleanup reporting.

Likely files: pipeline plan/model packages, expression evaluator, API and persistence contracts, structured events, shared TypeScript types, desktop inspector/history/log components, and new preflight/debug-bundle tests.

Compatibility risks: persisted event/schema evolution, redaction of context traces, stable matrix/job identifiers, and avoiding any preflight code path that downloads remote content or connects to Docker.

Phase 1 must preserve registry-driven support notices and must not start containers for preflight inspection.
