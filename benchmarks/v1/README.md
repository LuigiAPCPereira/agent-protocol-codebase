# Codebase benchmark v1

This benchmark tests whether a codebase-intelligence workflow reduces repository
exploration cost without reducing factual correctness.

It is deliberately not a product-ranking benchmark. Report correctness and cost
axes separately.

## Arms

Run each task against the same fixture revision and with the same task prompt.

- `raw`: ordinary repository/file/search tools; no precomputed code graph.
- `codebase`: Agent Protocol Codebase API may be used.
- `graphify`: reserved comparison arm for Graphify under equivalent repository
  access. Do not record this arm until the harness actually provides Graphify.

An arm may use its normal navigation primitives, but must not receive another
arm's intermediate output.

## Required run record

Each run records:

- benchmark version;
- task ID;
- arm;
- exact fixture revision or fixture content identity;
- neutral factual claims;
- tool calls;
- files opened;
- input/output tokens when the harness exposes them;
- elapsed milliseconds when the harness exposes them;
- optional notes describing unavailable metrics.

The scorer does not infer facts from prose. The harness must normalize the
answer into the neutral fact strings defined by `tasks.json`.

## Metrics

Correctness:

- matched expected facts;
- missing expected facts;
- unexpected submitted facts;
- complete: every expected fact matched and no unexpected fact was submitted.

Cost:

- tool calls;
- files opened;
- input tokens;
- output tokens;
- elapsed milliseconds.

A missing cost metric is unknown, not zero.

## Fairness rules

1. Same fixture content and prompt for every arm.
2. No cross-arm leakage of paths, answers, indexes, or transcripts.
3. Count setup cost separately from per-task cost when an arm requires indexing.
4. Report cold and warm Codebase runs separately once persistent-index reuse is
   evaluated.
5. Do not claim superiority from this synthetic fixture alone.
6. Graphify results remain unmeasured until the external tool is actually run
   under equivalent access.
7. Keep raw-tool results as a first-class baseline; the Codebase API must earn
   its complexity.

## Initial tasks

The first fixture contains repeated generic names and package boundaries so a
solution must resolve actual repository structure rather than answer from naming
alone.

The benchmark starts with three questions:

1. locate a specific declaration;
2. identify a direct package dependency;
3. explain the structural-dependent chain from a symbol to an importing package.

The initial suite is intentionally small. Expand only after these measurements
are reliable and sensitive to known-bad submissions.
