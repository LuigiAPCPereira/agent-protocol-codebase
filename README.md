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

Only the contract foundation is implemented in the first slice. Parsing, persistence, graph traversal, language adapters, and remote execution providers come later.

## Evidence model

Results are expected to carry enough metadata for an agent to reason about what was actually observed:

- exact repository/workspace identity;
- engine and schema version;
- index freshness;
- declared capabilities;
- provenance/resolution information for structural facts.

The derived index is evidence acceleration, not repository truth. Code, Git state, tests, and runtime observations remain authoritative.

## Safety defaults

The future engine should remain read-only by default and must not assume permission to:

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
```

Validate a request envelope:

```bash
printf '%s\n' '{"schema_version":1,"request_id":"example","operation":"status","repository":{"revision":{"commit":"abc123"}}}' \
  | go run ./cmd/ap-codebase validate-request
```
