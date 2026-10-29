# Implementation progress

## Foundation implemented

- Go HTTP API with an OpenAPI contract and strict input validation.
- Explicit run state machine and asynchronous worker.
- In-memory demo mode and PostgreSQL persistence.
- Single-worker ownership and interrupted-run failure recovery.
- Bundled Go bug with genuine baseline, reproduction, and verification tests.
- API, workflow, cancellation, concurrent-claim, and PostgreSQL tests.
- Local PostgreSQL Compose configuration and GitHub Actions checks.

## Verification performed locally

- `go vet ./...` passed.
- `go test -race ./...` passed; database test initially skipped without configuration.
- PostgreSQL integration test passed against a disposable PostgreSQL 17 container.
- Live HTTP submission completed through `awaiting_review`, with all six workflow states and real regression/verification output saved in PostgreSQL.

## Not implemented yet

Model-generated investigation and patches, arbitrary repository execution, Docker sandbox runner, automatic checkpoint continuation, cancellation API, GitHub App and PR publication, Next.js dashboard, SSE, sqlc, and versioned database migrations.

The demo patch is deterministic. Do not present it as an AI-generated fix. No public repository, PR, deployment, or recurring automation has been created.
