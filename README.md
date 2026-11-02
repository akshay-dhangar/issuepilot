# IssuePilot

A Go-based issue-to-PR assistant, starting with an honest, executable workflow foundation.

**Current status: authenticated durable repair API, model-driven repair CLI, isolated repository runner, and deterministic demo.** The bundled fixture reproduces a divide-by-zero bug, applies a known patch, and runs real Go tests. A separate CLI can test a pinned commit from an explicitly selected local Go repository in Docker. The new repair CLI integrates the OpenAI Responses API, proposes a regression and bounded source edits, verifies them in Docker, and exports a patch for review. The provider has been tested with mocked HTTP responses; a live model run is not yet verified. GitHub App publication is not implemented.

See [the durable API guide](docs/API.md) for authenticated submission, progress polling, and patch downloads.

See [the model repair guide](docs/REPAIR.md) for the new workflow.

See [the repository runner guide](docs/RUNNER.md) for the CLI, isolation limits, and integration tests.

## Run locally

Requirements: Go 1.25+. Docker is optional for PostgreSQL.

```sh
go run ./cmd/server
```

The server binds to `127.0.0.1:8080`. Without `DATABASE_URL`, runs live in memory and disappear on restart. This default is demo-only mode. Repair mode requires PostgreSQL, an API token, a model/key, and a repository allowlist; see the API guide. Keep the service bound to localhost.

In a second terminal:

```sh
curl -i http://127.0.0.1:8080/v1/runs \
  -H 'Content-Type: application/json' \
  -d '{"scenario":"empty-average"}'
```

Copy the `Location` response header and poll it:

```sh
curl http://127.0.0.1:8080/v1/runs/REPLACE_WITH_ID
```

A successful run ends at `awaiting_review`, with baseline, reproduction, verification, and patch evidence. This state means the demo evidence is available; it does not mean a PR exists.

## Durable local mode

```sh
docker compose up -d --wait
export DATABASE_URL='postgres://issuepilot:localdev@localhost:5432/issuepilot?sslmode=disable'
go run ./cmd/server
```

The prototype creates its table and queue index on startup. Queued runs survive a restart. Interrupted active runs are marked `failed` with an explanation on startup, and can be resubmitted. There is no automatic checkpoint continuation yet. A PostgreSQL advisory lock limits the database to one active server/worker.

The Compose credentials are disposable local-development credentials. Do not expose this database publicly. `.env.example` documents settings; the application does not load `.env` itself.

## Workflow

```text
queued -> investigating -> reproducing -> editing -> verifying -> awaiting_review
                         any active stage -> failed
```

The worker:

1. Writes only a bundled, trusted Go fixture into a temporary directory.
2. Runs the existing test and requires it to pass.
3. Adds a regression test and requires the expected divide-by-zero failure.
4. Applies a fixed demonstration patch, explicitly labeled as deterministic.
5. Runs both tests and saves evidence before ending the run.
6. Removes the temporary workspace.

Each run has a two-minute budget and each test invocation a 30-second timeout. The child environment excludes application credentials and disables dependency downloads. The fixture has no third-party dependencies. A shared temporary Go build cache speeds repeated demos.

**The local fixture executor is not a sandbox.** Never extend it to accept user source or repository URLs. Repository tests must use the separate Docker runner described above.

## API and layout

The contract is in [`api/openapi.yaml`](api/openapi.yaml).

- `POST /v1/runs`: accept `{"scenario":"empty-average"}` and return `202` plus `Location`.
- `GET /v1/runs/{id}`: return state, timestamps, events, and evidence.
- `GET /healthz`: process liveness only, not database readiness.

```text
cmd/server          HTTP server, shutdown, worker lifecycle
cmd/issuepilot      Pinned repository test CLI
cmd/repair          Model-driven repair CLI and local evidence artifacts
internal/model      Provider interface and OpenAI Responses implementation
internal/agent      Bounded regression/repair/verification workflow
internal/sandbox    Snapshot validation and Docker execution
internal/httpapi    Request validation and JSON API
internal/runs       State machine, memory and PostgreSQL stores
internal/runner     Trusted demonstration fixture and worker
api                 OpenAPI contract
```

The initial SQL is deliberately small and explicit through pgx. Versioned migrations and sqlc generation are planned before expanding the schema. The service currently runs API and worker in one process; they are separate packages so a later deployment can split them.

## Verification

```sh
go vet ./...
go test -race ./...
```

PostgreSQL integration tests require an **isolated test database**, with no other runs or server using it:

```sh
TEST_DATABASE_URL='postgres://issuepilot:localdev@localhost:5432/issuepilot_test?sslmode=disable' \
  go test -race ./internal/runs -run TestPostgres -v
```

Tests cover invalid request bodies, missing runs, state-transition guards, concurrent claims, real failing/passing test evidence, cancellation, database ownership, and restart recovery. Database tests skip explicitly without `TEST_DATABASE_URL`.

## Next milestones

- [x] Local Docker runner: resource limits, disabled network, no host credentials/socket, pinned image.
- [ ] Dedicated disposable runner VMs before public/untrusted use.
- [x] Read-only snapshot of a selected local Go repository at an exact commit.
- [ ] GitHub-connected repository acquisition.
- [x] Model-provider interface and bounded regression/repair/verify loop for root Go packages.
- [ ] Live-model evaluation and tool-driven repository exploration.
- [ ] Independent verification tests and a reproducible evaluation suite.
- [x] Durable repair jobs, authenticated polling, final reports, and verified patch downloads.
- [ ] Resumable checkpoints, cancellation API, idempotency keys, queue admission limits.
- [ ] GitHub App, review tied to the exact patch/base commit, idempotent draft-PR publication.
- [ ] Next.js dashboard, streamed progress, evidence and diff review.
- [ ] Java/Maven/JUnit target repositories.

No external contributions or daily automated pushes are configured. The product will publish draft PRs only after review, and will never claim a patch is correct solely because the model says so.
