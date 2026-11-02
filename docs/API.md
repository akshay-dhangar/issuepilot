# Durable repair API

The server can now queue the model repair workflow, persist progress and final reports in PostgreSQL, and serve verified patches. It is a local single-operator service, with one active worker per database.

## Configure

Repair mode requires all five settings:

- `DATABASE_URL`: PostgreSQL connection string.
- `OPENAI_API_KEY`: key for the selected provider.
- `ISSUEPILOT_MODEL`: explicit model name with Responses structured-output support.
- `ISSUEPILOT_API_TOKEN`: a random bearer token, at least 32 bytes.
- `ISSUEPILOT_REPOSITORIES`: JSON mapping simple aliases to absolute existing local Git repository paths.

Example repository setting:

```sh
export ISSUEPILOT_REPOSITORIES='{"sample":"/absolute/path/to/sample"}'
```

Provision secrets in your shell or secret manager; the server does not automatically read `.env`. Generate a local API token with `openssl rand -hex 32` and keep it separate from the model API key. Pull the pinned runner image as described in [RUNNER.md](RUNNER.md), then run:

```sh
go run ./cmd/server
```

The default listener is `127.0.0.1:8080`. Keep it local; bearer auth is not a substitute for TLS or public-service hardening. Source from configured repositories may be sent to OpenAI when an authenticated caller queues a repair. See [REPAIR.md](REPAIR.md) for exactly which source and feedback is sent.

Incomplete repair configuration fails startup. With neither a repository map nor model configured, the server retains demo-only mode. A token can also be configured alone to protect previously saved reports without enabling new repair jobs. Previously saved repair records cannot be retrieved if authentication is disabled.

## Submit, poll, download

```sh
curl -i http://127.0.0.1:8080/v1/repairs \
  -H "Authorization: Bearer $ISSUEPILOT_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"repository":"sample","commit":"FULL_40_CHARACTER_COMMIT_SHA","issue":"Average(nil) should return zero without panicking."}'
```

Replace the example SHA with the exact commit. The response is `202 Accepted`, with a `Location` header such as `/v1/runs/ID`. Only configured repository aliases are accepted. Clients cannot choose filesystem paths, URLs, models, shell commands, or repair budgets.

```sh
curl http://127.0.0.1:8080/v1/runs/ID \
  -H "Authorization: Bearer $ISSUEPILOT_API_TOKEN"

curl --fail http://127.0.0.1:8080/v1/runs/ID/patch \
  -H "Authorization: Bearer $ISSUEPILOT_API_TOKEN" \
  -o patch.diff
```

Repair states are `queued → investigating → repairing → awaiting_review`, or `failed`. While `repairing`, persisted timestamped events describe baseline testing, regression generation, reproduction, and repair attempts. The final `report` contains the detailed model/test evidence. Patch download returns `409` until a verified patch is available and its SHA-256 matches the report. Its ETag contains the quoted digest. No automatic publication or application occurs.

All endpoints except `/healthz` require the bearer token when one is configured, including the existing demo endpoint. `/healthz` is process liveness only. JSON responses and patch downloads use `Cache-Control: no-store`.

## Persistence and restarts

- Requests are acknowledged only after queue insertion succeeds.
- Successful and failed final reports are saved, along with progress events.
- Completed reports and patches survive restart unchanged.
- Queued jobs survive restart. A changed model or removed repository alias causes a queued repair to fail rather than silently use different settings.
- Interrupted active jobs are marked failed during recovery; they are not automatically replayed or resumed. This avoids silently repeating model calls.
- A failed progress save stops the workflow. A failed final save stops the worker/server; recovery handles the last durable state on restart.

This milestone does not implement request idempotency, queue admission limits, multi-user authorization, cancellation endpoints, SSE, or a dashboard. Retrying a successful submission creates another job and can incur another model charge. A single operator should submit deliberately and use `Location` to poll.

## Validation

The end-to-end integration test uses a scripted model, a real HTTP server, the real worker, Docker and PostgreSQL. It checks a failing regression, a verified patch, persistence across reopening the database, patch download, and an unchanged source repository.

```sh
TEST_DOCKER=1 TEST_DATABASE_URL='postgres://.../isolated_test_db' \
  go test -race -p 1 ./internal/integration ./internal/runs -v
```

Never point these tests at a production database. A live provider call remains unverified until an API key is configured and a live evaluation is explicitly run.
