# Agent Protocol Codebase

Reference implementation of the Agent Protocol **Codebase API** candidate.

The project exists to give Agent Protocol a harness-independent, evidence-oriented way to inspect software repositories without making a local shell, a specific agent harness, or a specific third-party indexer part of the protocol contract.

## Status

**Candidate / experimental.**

The repository is intentionally small while the external contract is pressure-tested. It is not yet a promoted Agent Protocol subsystem and is not required for Agent Protocol to function.

## Architectural boundary

```text
Agent Protocol
      |
      v
Codebase API
      |
      v
Execution Provider
(local / workspace / CI / user-assisted)
      |
      v
AP Codebase Engine
```

The engine is an implementation of the Codebase API. Execution-provider selection belongs outside the engine.

## Initial API surface

Schema v1 reserves these operations:

- `scan`
- `status`
- `query`
- `path`
- `impact`
- `diff`

The current tracer slices implement `status`, `scan`, and local snapshot persistence.

### status

`status` observes the Git root/HEAD, whether the worktree is dirty, and a deterministic SHA-256 fingerprint for tracked and untracked workspace changes. It also compares that identity with the latest persisted snapshot and reports:

- `ABSENT` when no local index exists;
- `EXACT` or `PARTIAL` when the persisted snapshot matches the observed repository identity;
- `STALE` when the repository has changed since the persisted snapshot;
- `INVALID` when the local cache cannot be interpreted safely.

### scan

`scan` requires an exact repository/workspace identity and builds a deterministic, read-only source snapshot:

- tracked files present in the worktree;
- untracked files not excluded by Git ignore rules;
- normalized relative paths;
- regular-file/symlink kind;
- executable bit for regular files;
- SHA-256 content hashes;
- byte sizes;
- deterministic snapshot ID.

Ignored files are outside the source universe. Unsupported source kinds are omitted and make the scan `PARTIAL` rather than silently pretending completeness.

The engine observes repository identity before reading and again before and after persistence. If the worktree changes during the operation, it fails with `WORKSPACE_CHANGED_DURING_SCAN`; a snapshot already persisted in that race is subsequently observed as `STALE`.

### persistence

The current snapshot is stored in SQLite under the Git metadata area resolved by:

```text
git rev-parse --git-path agent-protocol/codebase.sqlite3
```

That keeps derived data out of the versioned worktree and naturally separates linked worktrees. The database is a disposable cache, not project truth.

The SQLite implementation is isolated behind `internal/store`. The current driver is `github.com/ncruces/go-sqlite3`, chosen because it is CGO-free and compatible with the Go 1.26 portability baseline.

## Evidence model

Results carry enough metadata for an agent to reason about what was actually observed:

- exact Git commit;
- dirty/clean worktree state;
- workspace fingerprint when dirty;
- engine and schema version;
- persisted snapshot/index state;
- declared capabilities;
- structured API errors.

A request may include an expected commit or workspace fingerprint. `status` returns a structured mismatch instead of pretending a different workspace is equivalent. `scan` refuses to analyze a dirty worktree unless the caller supplies its exact fingerprint.

The derived snapshot/index is evidence acceleration, not repository truth. Code, Git state, tests, and runtime observations remain authoritative.

## Safety defaults

The engine does not execute project code and does not assume permission to:

- install project dependencies;
- run lifecycle scripts;
- access secrets;
- use the network during repository analysis;
- write to the versioned worktree.

`scan` does write its derived SQLite cache into Git metadata. Execution providers may substitute an ephemeral or remote persistence strategy later without changing the Codebase API.

## Development

Requires Go 1.26 or newer.

```bash
go test ./...
go run ./cmd/ap-codebase version --json
go run ./cmd/ap-codebase status --json
go run ./cmd/ap-codebase scan --json
```

The generic Codebase API execution seam accepts a request envelope on stdin. For a dirty worktree, obtain the exact fingerprint with `status` first and include it in the scan request.

Validate an envelope without executing it:

```bash
printf '%s\n' '{"schema_version":1,"request_id":"example","operation":"status","repository":{"root":"."}}' \
  | go run ./cmd/ap-codebase validate-request
```
