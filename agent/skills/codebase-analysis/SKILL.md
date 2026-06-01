---
name: codebase-analysis
description: "Analyze a codebase before edits, infer local patterns, and choose focused implementation and verification steps."
metadata:
  version: "0.1.0"
  author: agno-deep-agents-port
---

# Codebase Analysis Skill

Use this skill for non-trivial code tasks before changing files. The goal is to build enough local evidence to make a small, correct change.

## Operating Rules

1. Start from the user's concrete request and the current workspace.
2. Read `.ia/**/*.md` first when the directory exists, because these files are the workspace's AI-facing instructions.
3. Read the remaining workspace `*.md` files after `.ia` so project documentation can refine the task.
4. Inspect the smallest useful set of files before editing.
5. Prefer exact evidence: file paths, symbols, commands, test names, package names, and observed errors.
6. Infer local conventions from nearby code instead of introducing a new style.
7. Keep unrelated refactors out of scope.
8. Preserve user changes. Never revert files only because they are inconvenient.

## Discovery Checklist

- Identify the project language, package/module boundaries, and entry points.
- Look for `.ia` markdown guidance before general README or docs files.
- Find the files most likely to own the behavior.
- Check existing tests before adding or changing behavior.
- Look for helper APIs, builders, fixtures, and local patterns.
- Note risky integration points such as CLIs, persistence, external services, concurrency, and generated files.

## Go DDD Architecture Bias

For Go repositories, compare new or moved code against the `devalexandre/golang-ddd-template` structure unless local docs or existing code say otherwise:

- `cmd/main.go` as the application entry point.
- `internal/domain/<context>/` for business rules, contracts, factories, services, repositories, mocks, and colocated tests.
- `internal/infra/` for infrastructure adapters such as database and OpenAPI integrations.
- `internal/helpers/` for shared support packages such as config, errors, and logger.
- Tests named `*_test.go` beside the code they cover.

## Implementation Guidance

- Edit only the files needed for the requested behavior.
- Prefer existing abstractions and naming.
- Add comments only where they clarify non-obvious behavior.
- Update docs when the user-facing behavior or command surface changes.
- When a task is uncertain, state the assumption in the phase output before implementing.

## Verification

- Run targeted checks near the modified code first.
- Expand to broader tests when the change touches shared behavior.
- Report exact commands and outcomes.
- If checks cannot run, explain the blocking reason and what remains unverified.

## Final Report

Keep the final output compact:

- what changed;
- files changed;
- verification commands and results;
- residual risk or blockers.
