# Implementation progress

## Implemented

- Go HTTP API with an OpenAPI contract and strict input validation.
- Explicit run state machine and asynchronous worker.
- In-memory demo mode and PostgreSQL persistence.
- Single-worker ownership and interrupted-run failure recovery.
- Bundled deterministic Go bug with genuine baseline, reproduction, and verification evidence.
- CLI that snapshots an exact commit from an explicitly selected local Go repository and tests it in Docker.
- Digest-pinned Go image; no network, non-root user, readonly source/root, resource and output limits, timeout cleanup.
- Safe bounded archive extraction excluding links, special files, Git metadata, and path traversal.
- Local PostgreSQL Compose configuration and GitHub Actions checks, including Docker integration tests.

## Verification performed locally

- `go vet ./...` and `go test -race ./...` passed.
- PostgreSQL integration test passed against a disposable PostgreSQL 17 container during the foundation milestone.
- Live HTTP submission reached `awaiting_review`, with real regression/verification output saved in PostgreSQL.
- `TEST_DOCKER=1 go test -race ./...` passed for the runner milestone.
- Docker tests verified non-root execution, source/root write denial, network denial, no host sentinel secret, no Docker socket, and no-new-privileges.
- A deliberately failing container test produced the expected failure, and a timed-out run left no runner container behind.
- The actual CLI exported a committed fixture and returned successful JSON evidence with the matching commit and immutable image digest.

## Next milestone

Model-provider interface and bounded investigation/edit/verify loop, operating on the isolated runner. Tests should independently establish whether the proposed fix addresses the reported issue; model claims are not verification.

## Not implemented yet

Model-generated patches, remote repository acquisition, dedicated runner VMs, automatic checkpoint continuation, cancellation API, GitHub App and draft-PR publication, Next.js dashboard, SSE, sqlc, and versioned database migrations.

The HTTP demo patch is deterministic. The CLI tests selected local source without modifying it. Neither should be presented as an AI-generated fix. No recurring automation is configured.
