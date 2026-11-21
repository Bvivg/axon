# 0007. No comments in source code; the "why" lives in Markdown

- Status: accepted
- Rule: [`.claude/rules/comments.md`](../../.claude/rules/comments.md)

## Context

A reviewer opening `owner.go` finds lease, fence and in-flight invariants and not one line of prose next to them. That looks like neglect, so this ADR records that it is deliberate and where the explanations are instead.

The repository is developed with coding agents working under explicit rules in `.claude/rules/`, with plans written before code. An agent re-reads a file every time it touches it, and every comment costs context on every read. Across a monorepo of seven Go modules, a web client and the deploy configuration, that cost grows faster than it looks at the scale of one file. Comments also drift: the code changes, the comment above it does not, and nothing fails.

## Decision

- Source code has no comments: no `// why` notes, no doc comments on exported identifiers, no `/* */` blocks, no JSDoc. This covers Go, TypeScript and TSX, SQL migrations, `.proto`, YAML (Compose, workflows), Dockerfiles and shell scripts.
- Exceptions are directives that a compiler or linter parses: `//go:build`, `// +build`, `//go:generate`, `//go:embed`, `//nolint:…`, `//lint:ignore …`, and a shebang line. Generated code is not edited by hand.
- Markdown is not code, and this is where explanations go:

| What would have been a comment | Where it goes instead |
|---|---|
| What a function does | Its name and signature, small single-purpose packages, sentinel errors with wrapped context |
| Invariants of a package | A package README, such as [`owner`](../../core/services/game/internal/owner/README.md), [`tournament`](../../core/services/tournament/README.md) or [`core/deploy`](../../core/deploy/README.md) |
| Why an architecture was chosen | An ADR in this directory, or the phase plan in `.claude/plans/` with its results and deviations |
| A workaround, a race, a limit of an external system | The commit message or the PR description |
| Proof that something works | A test named as a sentence (`TestSuccessorWaitsForTheLeaseAndTheZombieStopsAcking`) and the recorded results in [benchmarks](../benchmarks.md) |

## Alternatives rejected

- Doc comments on exported identifiers only: still paid on every read, and most of them restate the name.
- Free-form comments with a review checklist: drift is not caught by any tool.

## Consequences

- A human reader has to know where to look; this ADR and the package READMEs are the map, and they have to be kept current by hand.
- `go doc` and IDE hovers show nothing for this repository's packages.
- Mechanical stripping left a few orphan blank lines, for example between the methods of the `Game` interface in `core/services/game/internal/engine/engine.go`; these are cosmetic.
- The rule is enforced by review, not by a linter.

## Evidence

- The rule and its exceptions: [`.claude/rules/comments.md`](../../.claude/rules/comments.md).
- `golangci-lint` runs on every module with 0 issues ([phase 7c results](../../.claude/plans/platform-phase-7c-tournaments.md#результаты-2026-11-20)).
