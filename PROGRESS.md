# Implementation progress

## Implemented

- Authenticated durable repair API: configured repository aliases/model, persisted progress/report data, and integrity-checked patch downloads.

- Model repair CLI with an OpenAI Responses provider, structured proposals, frozen regression, at most two repair attempts, and local evidence/patch artifacts.
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

## Model-loop milestone verification

- Provider contract tested against a fake HTTP server: request configuration, structured parsing, refusals, incomplete responses, oversized responses, cancellation, and redacted upstream errors.
- Workflow tests cover baseline failure, actual named regression failure, root-package identity, protected-file rejection, bounded retries, frozen regression, and applicable patch output.
- Real Docker test reproduced an empty-input panic, verified a scripted model fix, applied the exported patch to a disposable repository, and re-tested the delivered artifact.
- No live model call was made: OPENAI_API_KEY was not configured. Scripted model responses are test doubles, not evidence of model quality.

## Durable API milestone verification

- Auth, allowlist/input validation, protected historical reports, digest integrity, worker dispatch and deep-copy isolation tests passed.
- A real HTTP → worker → Docker → PostgreSQL test completed a repair with scripted model responses, then reopened the database and verified the report content and exact patch were preserved.
- PostgreSQL tests cover single-worker ownership, interrupted-run recovery, and queued repair persistence.
- A failed progress save stops work before model execution. Disabled/changed execution configuration is rejected.
- API key remains unconfigured; no live provider call was made.

## Next milestone

Review dashboard for job progress, execution evidence, and patch inspection, plus live-model evaluation on curated cases. Independent evaluation cases and human review remain necessary.

## Not implemented yet

Live-provider verification, remote repository acquisition, dedicated runner VMs, automatic checkpoint continuation, cancellation API, GitHub App and draft-PR publication, Next.js dashboard, SSE, sqlc, and versioned database migrations.

The HTTP demo is still deterministic. The repair CLI is model-backed, but local/CI verification uses scripted model responses. All repository changes are made in disposable snapshots and emitted as reviewable patches; the selected repository stays unchanged. No recurring automation is configured.
