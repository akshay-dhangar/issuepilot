# Model-driven repair loop

The `repair` CLI asks an OpenAI model for a regression test and a bounded code fix, then executes tests in the Docker runner. It exports a reviewable patch and execution evidence. It never modifies the selected repository, opens a PR, or merges code.

This milestone has been validated with scripted model responses and real Docker execution. A live model run has not yet been validated. No API key is required for the automated tests.

## Run

Requirements: Go 1.25+, Git, the pinned Docker image from [RUNNER.md](RUNNER.md), and an OpenAI API key available as `OPENAI_API_KEY` in your shell. Choose an explicit model with Responses API structured-output support and access in your account; no model is silently selected.

```sh
go build -o bin/repair ./cmd/repair
./bin/repair \
  -repo /absolute/path/to/repository \
  -commit FULL_40_CHARACTER_COMMIT_SHA \
  -issue-file /absolute/path/to/issue.txt \
  -model YOUR_MODEL_ID \
  -output .issuepilot/reports/run-001
```

Create the parent `.issuepilot/reports` directory first. The final output directory must be new; previous evidence is never overwritten. Exit 0 means a patch is awaiting review, 1 means the attempted workflow failed (see the report), and 2 means configuration or artifact writing failed.

**Data sent to OpenAI:** the issue text, root-level Go source/tests, root `go.mod`, proposed code, and bounded test feedback. Select a repository whose source is approved for that provider. Only the Responses API is used; `store` is false, which does not imply a zero-retention guarantee. Credentials are read by the provider client and never mounted into the runner. The CLI does not automatically load `.env`.

## Steps and evidence

1. Export the exact committed tree, then run the unchanged test suite. Stop if it fails or output is truncated.
2. Send the bounded root package context to the model, requesting one new `issuepilot_regression_test.go` containing `TestIssuePilotRegression`.
3. Execute that test with the original production code. Require an actual failing test event for that name in the root module. Compilation failures, package-only failures, skipped tests, and output that merely says “fail” do not count.
4. Freeze the normalized regression file. Ask for changes to 1-4 existing root production Go files. Test files, module files, new production files, subdirectories, generated files, and compiler directives are outside this first scope.
5. Run the entire suite on an isolated candidate snapshot. Require both exit code zero and a passing event for the named regression.
6. If necessary, make one further repair attempt with failure feedback. Each attempt starts from the original commit plus the same frozen regression.
7. Save `report.json`, and on success `patch.diff`, then clean up candidate snapshots and containers.

The report includes the base commit, timestamps, events, model response IDs/model names/token usage when supplied, attempts, executed commands, container image digest, test logs, frozen regression hash, and patch SHA-256. The patch includes both the production fix and regression test. The report is also saved on workflow failure; failed runs do not emit a successful patch artifact. File permissions are 0600 inside a 0700 directory.

Review and apply only to the matching base commit:

```sh
git -C /path/to/repository apply --check /absolute/path/to/run-001/patch.diff
# After reviewing the patch:
git -C /path/to/repository apply /absolute/path/to/run-001/patch.diff
```

## Bounds and limitations

- Root-package Go bugs only; there must be production `.go` files at repository root.
- Existing suite must pass before work starts. The module must work offline (no dependencies or vendored dependencies).
- At most 40 context files / 128 KiB total, 64 KiB per file, and an 8 KiB issue.
- Exactly one regression proposal and at most two repair proposals; no automatic HTTP retries.
- Up to 8,192 output tokens per provider call, a 90-second provider timeout, a 2-minute test-run timeout, and a 10-minute overall workflow timeout.
- Model refusals, incomplete outputs, malformed proposals, and infrastructure failures stop the workflow. Upstream error bodies are not logged.
- Test and source proposals are untrusted. They run only in Docker and remain subject to the development-runner limits in RUNNER.md. Use dedicated stronger isolation before offering this publicly.
- The regression is model-generated, not an independent correctness oracle. A passing test is evidence, not proof that a patch solves the issue; human review and independent benchmark cases remain necessary.
- Reports are local artifacts. Durable server jobs, cancellation API, streamed UI, tool-driven repository search, and GitHub publication are not part of this CLI milestone.

## Verify

```sh
go vet ./...
go test -race ./...
TEST_DOCKER=1 go test -race -p 1 ./internal/sandbox ./internal/agent -v
```

The model client contract is tested against a local fake HTTP server. Workflow tests cover protected-file rejection, failed baselines, false reproduction signals, bounded retries, and a frozen regression. The Docker integration test executes a real panic, fixes it using scripted model output, exports the patch, applies it to a disposable repository, and re-tests that delivered artifact.

Provider contract: [OpenAI Structured Outputs](https://developers.openai.com/api/docs/guides/structured-outputs).
