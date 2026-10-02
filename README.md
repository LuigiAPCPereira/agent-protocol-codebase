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

The current tracer slices implement `status` and `scan`.

### status

`status` observes the Git root/HEAD, whether the worktree is dirty, and a deterministic SHA-256 fingerprint for tracked and untracked workspace changes. It does not execute project code.

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

The engine observes repository identity before and after scanning. If the worktree changes during the read, the operation fails with `WORKSPACE_CHANGED_DURING_SCAN` instead of returning a false `EXACT` snapshot.

Snapshots are currently returned in the result only; persistence is not implemented yet. Therefore a later `status` still reports the persistent index as `ABSENT`.

## Evidence model

Results carry enough metadata for an agent to reason about what was actually observed:

- exact Git commit;
- dirty/clean worktree state;
- workspace fingerprint when dirty;
- engine and schema version;
- snapshot/index state;
- declared capabilities;
- structured API errors.

A request may include an expected commit or workspace fingerprint. `status` returns a structured mismatch instead of pretending a different workspace is equivalent. `scan` refuses to analyze a dirty worktree unless the caller supplies its exact fingerprint.

The derived snapshot/index is evidence acceleration, not repository truth. Code, Git state, tests, and runtime observations remain authoritative.

## Safety defaults

The engine is read-only by default and does not assume permission to:

- execute project code;
- install project dependencies;
- run lifecycle scripts;
- access secrets;
- use the network;
- write to the repository.

Those capabilities must be explicit at execution boundaries.

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
