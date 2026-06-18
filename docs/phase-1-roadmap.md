# Phase 1 Roadmap

Phase 1 should build debugging foundations on top of the support registry:

1. Add an inspection-only preflight API returning raw/resolved YAML, normalized model, expanded jobs, dependency graph, images, conditions, unresolved expressions, services, storage expectations, consent requirements, and support records without connecting to Docker.
2. Persist expression traces with original/interpolated forms, operands, relevant redacted contexts, results, errors, and skip reasons.
3. Reopen historical structured events, states, matrix values, images, artifacts, caches, cancellation details, and cleanup errors.
4. Add redacted debug-bundle export.
5. Add explicit run-selection modes for a job alone, transitive dependencies, downstream dependents, failed jobs, and safe restart-from-step behavior.
6. Add queued-job, running-job, and workflow cancellation controls with deterministic cleanup reporting.

Phase 1 must preserve registry-driven support notices and must not start containers for preflight inspection.
