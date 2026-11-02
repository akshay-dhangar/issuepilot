# IssuePilot: one-week MVP finish plan

Active week: **2–8 November 2026 (Asia/Kolkata)**.

Scope: a locally runnable review dashboard over the existing Go repair API, with job submission, progress, evidence inspection, and verified patch download. Preserve the CLI, bounded execution, and honest model-validation status.

- [x] Day 1 — Integrate the three tested backend PRs into main; define the dashboard acceptance criteria and weekly scope.
- [ ] Day 2 — Add a runnable dashboard shell with an explicit local API connection and authenticated job submission.
- [ ] Day 3 — Add run polling, progress events, failure display, and reconnect behavior.
- [ ] Day 4 — Add evidence and patch inspection with digest-aware downloads.
- [ ] Day 5 — Add a reproducible scripted end-to-end demo and meaningful UI/API checks.
- [ ] Day 6 — Improve setup, error states, and documented evaluation cases; run a live model check only if credentials and authorization are available.
- [ ] Day 7 — Verify the acceptance checklist, save demo instructions and a factual LinkedIn draft, and document any unfinished work.

## MVP acceptance checklist

- [ ] A documented local setup starts the Go API and dashboard.
- [ ] A user can submit an approved repository alias, exact commit, and issue.
- [ ] A user can follow progress and inspect successful/failed execution evidence.
- [ ] Patch download checks the stored evidence digest and never applies or publishes changes automatically.
- [ ] Critical user flows and backend tests pass in CI.
- [ ] A repeatable demo works with scripted responses and is labeled as such.
- [ ] Live model capability is distinguished from live model validation; missing credentials are disclosed.
- [ ] README and demo clearly describe the limited root-package Go scope and remaining work.

The target is a demonstrable MVP, not a production service or proof of model quality. If a checklist item is incomplete after seven days, record the gap rather than declaring it finished.

## Daily execution

The user approved automatic meaningful daily development and pushes, and merging our own portfolio PRs after review and passing required checks. Respect protections and do not touch unrelated work. Use one active project at a time; no empty commits or altered dates. Portfolio scheduling is managed in the parent workspace's portfolio-schedule.json.
