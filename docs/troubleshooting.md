# Troubleshooting

## A condition fails to evaluate

Piper emits `condition_evaluation_error` with a code and source position. Check that referenced outputs exist before evaluation.

## A matrix is rejected

The default limit is 128 expanded jobs. Reduce the matrix or raise `maxExpandedJobs`, up to 1024.

## A service does not become ready

Check `service_log` events, image health checks, required environment variables, and the startup timeout.

## A remote action will not run

Use the desktop consent prompt or call `run.prepare` with `allowThirdPartyCode=true`, then pass its token to `run.start`.

## PowerShell is missing

Azure PowerShell handlers require `pwsh` in the selected job image.

## Artifact or cache paths are rejected

Absolute paths, traversal, escaping symlinks, and files outside the job workspace are intentionally blocked.
