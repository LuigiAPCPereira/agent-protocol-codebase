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

The current tracer slice implements `status`: it observes the Git root/HEAD, whether the worktree is dirty, and a deterministic SHA-256 fingerprint for tracked and untracked workspace changes. No project code or lifecycle scripts are executed.

The structural index is not implemented yet, so `status` currently reports `ABSENT`.

## Evidence model

Results carry enough metadata for an agent to reason about what was actually observed:

- exact Git commit;
- dirty/clean worktree state;
- workspace fingerprint when dirty;
- engine and schema version;
- index freshness;
- declared capabilities;
- structured API errors.

A request may include an expected commit or workspace fingerprint. `status` returns a structured mismatch instead of pretending a different workspace is equivalent.

The derived index is evidence acceleration, not repository truth. Code, Git state, tests, and runtime observations remain authoritative.

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
```

The generic Codebase API execution seam accepts a request envelope on stdin:

```bash
printf '%s\n' '{"schema_version":1,"request_id":"example","operation":"status","repository":{"root":"."}}' \
  | go run ./cmd/ap-codebase execute
```

Validate an envelope without executing it:

```bash
printf '%s\n' '{"schema_version":1,"request_id":"example","operation":"status","repository":{"root":"."}}' \
  | go run ./cmd/ap-codebase validate-request
```
