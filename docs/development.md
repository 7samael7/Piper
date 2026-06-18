# Development Guide

This guide covers the local toolchain, repository layout, common commands, engine utilities, packaging, update configuration, and the release workflow.

## Toolchain

The project currently targets:

- Node.js 24 in the primary CI workflow
- Node.js 22 in the release-packaging workflow
- npm workspaces
- Go 1.25 from `engine/go.mod`
- Electron 39
- TypeScript 5.8
- React 19
- Docker Engine API through the Go Docker SDK

Newer compatible Node and Go versions can work, but the CI versions are the reproducible baseline.

## Repository layout

```text
.
├── apps/desktop/              Electron Forge desktop application
│   ├── src/main/              Electron main process and update service
│   ├── src/preload/           Context-isolated renderer bridge
│   ├── src/renderer/          React UI
│   └── assets/                Application icons and artwork
├── engine/
│   ├── cmd/daemon/            JSON-RPC sidecar entry point
│   ├── cmd/cli/               Workflow discovery CLI
│   ├── internal/              Providers, graph, validation, Docker, API, DB
│   └── migrations/            Reference SQLite schema
├── packages/shared-types/     TypeScript contracts shared with the renderer
├── examples/                  Example pipelines for all providers
├── scripts/                   Development, version, and update scripts
└── docs/                      User, support, architecture, and API docs
```

## Initial setup

```sh
make install
```

This runs:

```sh
npm install
cd engine && go mod download
```

## Start the application

Build the engine and start Electron:

```sh
make engine
make desktop
```

Or use:

```sh
./scripts/dev.sh
```

Engine resolution in development is:

1. Use `engine/bin/piper-engine` when it exists.
2. Otherwise run `go run ./cmd/daemon` from `engine`.

Building the binary first gives startup and behavior closest to a packaged app.

## Tests and checks

Run all repository checks:

```sh
make test
```

This runs:

```sh
cd engine && go test ./...
npm --workspace packages/shared-types run build
npm --workspace apps/desktop run typecheck
```

Useful focused commands:

```sh
cd engine && go test ./internal/providers/github
cd engine && go test ./internal/providers/gitlab
cd engine && go test ./internal/providers/azure
cd engine && go test ./internal/executor/docker
npm --workspace apps/desktop run typecheck
```

## Standalone discovery CLI

The CLI discovers workflows for one provider and prints JSON summaries:

```sh
cd engine
go run ./cmd/cli /absolute/path/to/repository github
go run ./cmd/cli /absolute/path/to/repository gitlab
go run ./cmd/cli /absolute/path/to/repository azure
```

The provider defaults to `github` when omitted.

The CLI is a discovery/debugging tool. It does not execute workflows.

## Engine daemon

Start the newline-delimited JSON-RPC daemon:

```sh
cd engine
PIPER_DB=/tmp/piper.db go run ./cmd/daemon
```

Requests are read from stdin, responses and notifications are written to stdout, and diagnostics go to stderr. See [Engine API](engine-api.md).

## Configuration environment variables

| Variable | Used by | Purpose |
| --- | --- | --- |
| `PIPER_DB` | Desktop and engine | Overrides the SQLite database path. |
| `DOCKER_HOST` | Engine | First-choice Docker endpoint. |
| `DOCKER_CONTEXT` | Engine | Overrides the Docker context selected from Docker config. |
| `XDG_RUNTIME_DIR` | Engine on Linux | Adds the runtime Docker socket candidate. |
| `PIPER_UPDATE_TOKEN` | Desktop | Authenticates GitHub update checks/downloads for a private repository. |
| `PIPER_UPDATE_REPOSITORY` | Update-config script | Repository in `owner/name` format. |
| `APPLE_SIGN_IDENTITY` | Packager | Enables macOS signing with the named identity. |
| `APPLE_ID` | Packager | Apple ID used for notarization. |
| `APPLE_APP_SPECIFIC_PASSWORD` | Packager | Notarization app password. |
| `APPLE_TEAM_ID` | Packager | Apple developer team ID. |

The GitHub release workflow also uses `APPLE_CERTIFICATE` and `APPLE_CERTIFICATE_PASSWORD` to create a temporary signing keychain before packaging.

## SQLite schema

The runtime schema is embedded in `engine/internal/persistence/store.go`; `engine/migrations/001_initial.sql` mirrors the initial schema for reference.

The store uses:

- WAL journal mode.
- One open database connection.
- RFC 3339 nanosecond UTC timestamps.
- A `runs` table for summaries.
- A `run_events` table for ordered structured events.

If the schema changes, update both the runtime initialization and migration/reference files, and account for existing user databases.

## Adding or changing a provider

Each provider implements:

```go
type Provider interface {
    ID() ProviderID
    Discover(ctx context.Context, repoPath string) ([]WorkflowSummary, error)
    Load(ctx context.Context, repoPath, workflowPath string) (*Workflow, []byte, error)
    Validate(ctx context.Context, workflow *Workflow) ValidationReport
}
```

A provider change normally requires:

1. Map YAML into the neutral model.
2. Build graph-compatible `Needs` IDs.
3. Classify job and step support.
4. Add feature-level validation messages.
5. Add parser tests for supported, partial, and unsupported cases.
6. Register a new provider in `internal/api/server.go` if adding one.
7. Extend `ProviderID` in Go and TypeScript.
8. Add renderer labels and a default event.
9. Update the provider support documentation.

Keep parser visibility separate from executor support. Piper should preserve useful metadata and explain unsupported behavior rather than silently pretending to execute it.

## Docker executor notes

- The executor creates one container per job.
- All steps in that job use `ContainerExec`.
- Output uses Docker's multiplexed stream and is separated with `stdcopy.StdCopy`.
- Cancellation kills the active container with `SIGKILL`.
- Containers are force-removed after the job.
- The repository path is bind-mounted read-write.
- Shell execution is fixed to `/bin/bash -lc`.

When adding support for a step type, update both validation classification and executor behavior. A mismatch can cause the UI to advertise behavior the runtime does not provide.

## Build and package

Build an unpacked Electron application:

```sh
make engine
npm --workspace apps/desktop run package
```

Cross-compile the engine for the host and run the makers configured for that platform:

```sh
make package
```

Platform-specific commands are:

```sh
make dmg
make linux
make windows
```

The equivalent npm commands are:

```sh
npm run desktop:make:mac
npm run desktop:make:linux
npm run desktop:make:windows
```

`scripts/make-desktop.mjs` cross-compiles the Go sidecar for the requested target, passes that binary to Electron Forge, and writes generated output below `apps/desktop/out/`.

Run each installer maker on its native host. Linux packaging requires `fakeroot`, `dpkg`, and `rpmbuild`; Windows packaging uses Squirrel.Windows. Release artifacts are:

- macOS: `.dmg` for `arm64` and `x64`.
- Windows: Squirrel Setup `.exe` for `x64`.
- Linux: `.deb` and `.rpm` for `x64`.

## Update configuration

The packaged app reads:

```text
apps/desktop/update-config.json
```

Generate it for another GitHub repository:

```sh
PIPER_UPDATE_REPOSITORY=owner/repository node scripts/write-update-config.mjs
```

The file contains the GitHub latest-release API URL and release page URL. Update URLs must use HTTPS, except loopback HTTP URLs accepted for local development.

Update behavior is implemented in `apps/desktop/src/main/updates.ts`. Installer filenames should contain `arm64`/`aarch64` or `x64`/`amd64`/`x86_64` so the app can select the correct asset. Supported installer extensions are `.dmg`, `.exe`, `.deb`, and `.rpm`. On Linux, Piper prefers RPM on Fedora/RHEL/SUSE-family systems and DEB elsewhere.

A checksum asset should use:

```text
<installer filename>.sha256
```

## Versioning

Set the same semantic version across all npm manifests:

```sh
node scripts/set-version.mjs 0.2.0
npm install --package-lock-only --ignore-scripts
```

The script updates:

- Root `package.json`.
- `apps/desktop/package.json`.
- `packages/shared-types/package.json`.
- The desktop dependency on `@piper/shared-types`.

Commit the updated manifests and lockfile together.

## Release workflow

The `Release` GitHub Actions workflow can start from:

- A successful `CI` run on `master`.
- A semantic-version Git tag.
- A manual workflow dispatch with a version.

For a normal release:

```sh
node scripts/set-version.mjs 0.2.0
npm install --package-lock-only --ignore-scripts
git add package.json package-lock.json apps/desktop/package.json packages/shared-types/package.json
git commit -m "Prepare v0.2.0"
git push origin master
```

After CI succeeds, the workflow:

1. Resolves and validates the version.
2. Skips a tag that already has a GitHub Release.
3. Builds Go engine binaries for every desktop target.
4. Builds macOS DMGs for `arm64` and `x64`.
5. Builds a Windows `x64` Setup executable.
6. Builds Linux `x64` DEB and RPM packages.
7. Creates SHA-256 files for every installer.
8. Publishes a Git tag and GitHub Release.

Prerelease versions such as `0.2.0-beta.1` are published as GitHub prereleases.

### Apple signing

Configure these GitHub Actions secrets together for signing:

- `APPLE_CERTIFICATE`: Base64-encoded Developer ID Application `.p12`.
- `APPLE_CERTIFICATE_PASSWORD`: Password for the `.p12`.
- `APPLE_SIGN_IDENTITY`: For example, `Developer ID Application: Example, Inc. (TEAMID)`.

For notarization, also configure all three:

- `APPLE_ID`
- `APPLE_APP_SPECIFIC_PASSWORD`
- `APPLE_TEAM_ID`

Unsigned DMGs are produced when signing credentials are absent. Windows and Linux artifacts are currently unsigned.

## Clean generated output

```sh
make clean
```

This removes Node dependencies and generated Electron, TypeScript, and engine build output. It does not remove source files or the user's application-data database.
## Support contract

`engine/internal/support/registry.json` is the source of truth for feature support. After an intentional registry change:

```sh
cd engine
go run ./cmd/supportdoc -write
go test ./internal/support
```

Review and update `internal/support/testdata/contract.sha256` only when the status/runtime contract change is intentional.
