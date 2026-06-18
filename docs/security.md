# Security

Pipeline definitions and downloaded actions are untrusted code.

- Remote GitHub actions require an explicit `run.prepare` consent flow. Piper records the resolved commit SHA and warns about mutable tags and branches.
- Deployment jobs pause until the user approves them. Their Docker network is internal-only.
- Workspace modes are `writable` (compatibility default), `read-only`, and `isolated` (recommended for unfamiliar pipelines).
- Artifact and cache paths must remain inside the prepared workspace after symlink resolution.
- Every job gets a separate Docker network; services and job containers are force-removed during cleanup.
- Secrets are exact-value masked in messages, logs, and structured event data. Encoded or transformed secrets may still escape masking.
- Mock OIDC is disabled by default. Its token begins with `PIPER_MOCK_OIDC`, uses an invalid provider issuer, and includes `piper_mock=true`.
- Remote includes, real cloud identity, cloud artifact backends, and unrestricted host mounts are not silently emulated.

Use an isolated workspace and disabled network access when inspecting code you do not trust.
