# Agent Guidelines

Always prefer simplicity over pathological correctness. Follow YAGNI, KISS, and
DRY. Do not add backward-compatibility shims or fallback paths unless they are
free and add no cyclomatic complexity.

## Project context

- `droidperm` is a Go CLI that manages Android runtime permissions and AppOps
  through an external `adb` binary.
- `droidperm.yaml` is a partial desired-state policy. Unmentioned values must
  never be changed.
- Runtime permissions are applied before AppOps and every applied value must be
  verified by reading it back.
- `capture` generates a reviewable policy starting point. It is not a complete
  device backup.
- Keep output deterministic and machine output stable. Never interpolate
  package names or policy values into a shell command; pass them as argv.

## Workflow

1. Use plan mode for non-trivial tasks and verification.
2. Prefer focused subagents for independent research or analysis.
3. Find root causes and keep changes minimal.
4. If work goes sideways, stop and re-plan.
5. Before finishing, run relevant tests and demonstrate correctness.
6. After a user correction, record the reusable lesson in `z-ai/lessons.md`.

For non-trivial work, pause before delivery and consider whether the result can
be made simpler or more elegant without expanding scope.

## Pre-commit documentation check

Before committing, verify that these documents still reflect the change:

- `README.md` for architecture, commands, or features
- `AGENTS.md` for agent-facing context
- `CLAUDE.md` for Claude Code instructions
- related files under `docs/`

Skip documents with no impact.

## Link handling

When the user provides an `x.com` or `twitter.com` status link, use the Bird
skill to fetch and inspect the post.
