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


## Mechanical tracer

`ap-codebase-bench-smoke` is a deterministic Codebase-arm tracer. It creates an
isolated Git repository from the fixture, runs `status` + `scan`, executes the
three benchmark tasks through Codebase API operations, normalizes the resulting
facts, and scores them with the same scorer.

The fixture commit uses fixed author/committer dates so identical fixture content
produces a stable repository revision.

This tracer is **not** the raw-vs-Codebase agent benchmark. It exists to prove
that the Codebase arm and scorer compose correctly before spending evaluation
budget on isolated agent runs.

A raw agent run is invalid when the evaluating context has already seen the
fixture layout or expected facts. Use a fresh isolated context for each arm and
task family, with no transcript or intermediate-output leakage.


## Blind agent bundle

Real raw-vs-Codebase agent runs must use `prompts.json`, not `tasks.json`.

The evaluator-owned `tasks.json` contains expected facts and must stay hidden
from the evaluated agent. The agent-visible bundle contains only:

```text
benchmarks/v1/fixture/
benchmarks/v1/prompts.json
```

`prompts.json` carries only benchmark version, fixture identity, task IDs, and
task prompts. It intentionally contains no expected facts or scorer material.

The committed blind manifest is validated against `tasks.json` so task order,
IDs, and prompts cannot silently drift. Tests also scan the blind JSON for every
expected fact and fail if oracle material leaks.

To regenerate the blind manifest deterministically:

```sh
go run ./cmd/ap-codebase-bench-blind benchmarks/v1/tasks.json
```

The command writes the blind JSON to stdout; the evaluator may compare that
output with the committed `prompts.json`.

For a valid comparative run, create a fresh context for each arm. Give that
context the same fixture revision and the same blind prompt, but never
`tasks.json`, scorer output, another arm's transcript, discovered paths, or
previous answers.


## Verified comparison submissions

Fresh external agent runs should be saved as submission envelopes rather than
plain scorer runs.

A submission contains:

- the normal benchmark run;
- the canonical blind-bundle digest;
- the canonical fixture digest;
- a fresh-context identifier;
- isolation attestations recording whether evaluator material or another arm's
  output was visible.

Generate the canonical identities with:

```sh
go run ./cmd/ap-codebase-bench-identity \
  benchmarks/v1/prompts.json \
  benchmarks/v1/fixture
```

Then compare two fresh submissions for the same task with:

```sh
go run ./cmd/ap-codebase-bench-compare \
  benchmarks/v1/tasks.json \
  benchmarks/v1/prompts.json \
  benchmarks/v1/fixture \
  raw.json \
  codebase.json
```

The comparator fails closed when the bundle or fixture digests differ, when the
repository revisions differ, when both runs claim the same context, when both
runs use the same arm, or when the isolation attestation reports evaluator/oracle
or other-arm leakage.

The isolation fields are attestations, not cryptographic proof of a fresh model
context. They prevent accidental invalid comparisons and make contamination
explicit in durable evidence. A trusted harness still owns actual process/model
isolation.

The comparison output deliberately contains both scored arms and no synthetic
winner field. Correctness and cost remain separate evidence axes.
