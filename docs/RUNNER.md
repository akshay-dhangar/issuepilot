# Pinned Go repository runner

The `issuepilot` CLI exports a committed local Go repository into a fresh snapshot and executes its tests in Docker. The existing HTTP API still exposes only the bundled deterministic demo; this CLI is the foundation for repository-backed agent runs.

## Use

Requirements: Go 1.25+, Git, and Docker with a local daemon that can access the working directory.

```sh
docker pull golang@sha256:56961d79ea8129efddcc0b8643fd8a5416b4e6228cfd477e3fd61deb2672c587
go run ./cmd/issuepilot \
  -repo /absolute/path/to/repository \
  -commit FULL_40_CHARACTER_COMMIT_SHA \
  -timeout 2m
```

Get the SHA using `git -C /absolute/path/to/repository rev-parse HEAD`. Branch names and abbreviated SHAs are deliberately rejected. Only tracked files from that exact commit are exported; working-tree modifications, untracked files, and Git credentials/configuration are not copied.

The JSON result contains the commit, immutable image digest, command, actual container exit code, bounded output, duration, and timeout flag. CLI exit codes are 0 for passing tests, 1 for failing tests, and 2 for preparation/infrastructure errors. `go run` wraps nonzero program exit codes; build the binary when scripting around the exact codes:

```sh
go build -o bin/issuepilot ./cmd/issuepilot
```

This version supports modules with no external dependencies, or dependencies already vendored in the commit. Downloads and toolchain auto-installation are disabled. No arbitrary command or shell argument is accepted from the issue or repository.

## Isolation contract

- Pinned official Go image; the runner never silently pulls a mutable tag.
- Non-root UID/GID 65534, all Linux capabilities dropped, no new privileges.
- Network disabled, no host Docker socket, no host secrets or environment variables forwarded.
- Read-only root filesystem and source mount; bounded writable temporary filesystem.
- One CPU, 768 MiB memory/no extra swap, 128 processes, bounded file descriptors.
- 32 MiB/5,000-entry snapshot limits; no symbolic/hard links, special files, duplicate files, `.git`, or path traversal.
- 1 MiB captured output; Docker's persistent log driver disabled for task containers.
- Bounded execution time and forced container cleanup on completion, cancellation, and timeout.

Snapshots are placed beneath `.issuepilot/snapshots` in the current working directory, ignored by Git, and removed after execution. On Docker Desktop/Colima the directory must be shared with the daemon. `ISSUEPILOT_SNAPSHOT_ROOT` can select another shared directory. A remote Docker daemon is unsupported.

Tests execute in Docker, but Git export and validated archive extraction run on the host. Snapshot files must be approved for the Docker host; tracked secrets are still tracked files and will be included. Containers share the daemon host's kernel. Run only repositories you trust on a development machine. A public service needs dedicated disposable runner VMs and stronger tenant isolation before accepting arbitrary hostile repositories.

The readonly workspace intentionally excludes tests that require mutating repository files. Use temporary directories within tests. Java support, writable agent edit workspaces, network-controlled dependency preparation, and model tools are future milestones.

## Tests

```sh
go test -race ./...
TEST_DOCKER=1 go test -race ./internal/sandbox -v
```

Docker integration tests exercise a passing isolation probe, a deliberately failing test, and a timeout with container cleanup. The probe checks the actual user, network, source/root write denial, missing Docker socket, privilege restriction, and absence of a host sentinel secret. Run these tests serially on an otherwise idle IssuePilot runner daemon.

Docker flag behavior: https://docs.docker.com/reference/cli/docker/container/run/
