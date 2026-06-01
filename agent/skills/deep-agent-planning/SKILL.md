---
name: deep-agent-planning
description: "Planning discipline for long-running coding tasks with phased discovery, implementation, validation, and concise delivery."
metadata:
  version: "0.1.0"
  author: agno-deep-agents-port
---

# Deep Agent Planning Skill

Use this skill for larger tasks, ambiguous requests, multi-file changes, or work that should be executed in phases.

## Goal

Turn a broad coding request into a controlled loop:

1. understand the task and constraints;
2. discover the relevant code;
3. choose a small implementation plan;
4. make scoped edits;
5. verify the result;
6. report clearly.

The workflow should be explicit enough for smaller or cheaper models to follow
without relying on hidden assumptions.

## Operating Loop

- Restate the concrete goal in implementation terms.
- Track constraints from the user and repository.
- Load repository instructions in this order: `.ia/**/*.md` first, then the other workspace `*.md` files.
- Split the work into discover, plan, implement, verify, and report phases.
- Keep phase outputs short enough that the next phase can use them directly.
- Update the plan when evidence changes.
- Do not skip verification unless it is impossible.

## Phase Contracts

Each phase should return a compact state packet:

- Goal
- Constraints
- Evidence read
- Decisions
- Files involved
- Commands run or planned
- Risks and open questions
- Next action

Use exact file paths, symbol names, commands, errors, versions, IDs, and test
results. If something was not inspected or not run, mark it as unchecked.

## Planning Format

Use a compact plan with these fields when useful:

- Goal
- Files likely to change
- Files to inspect
- Implementation steps
- Verification commands
- Risks or assumptions

## Context Rules

- Preserve file paths, symbol names, commands, errors, versions, IDs, and test results exactly.
- Summarize long outputs, but keep the lines needed to diagnose failures.
- Prefer repository evidence over general advice.
- Answer in the user's language unless the user asks for another language.
- Never invent tool output, files, tests, or repository rules.
- Prefer one complete, verified slice over several speculative edits.
- If instructions conflict, follow this order: explicit user request, repository `.ia` instructions, nearest `llm.md`, existing code, general best practice.

## Implementation Rules

- Read the target file before editing it.
- Keep changes scoped to the plan and user request.
- Preserve unrelated user changes.
- Prefer simple code and local conventions over broad abstractions.
- Avoid new dependencies unless the plan clearly justifies them.
- After editing, inspect the changed file or diff before reporting completion.
- If the plan proves wrong, make the smallest evidence-based adjustment and state why.

## Go DDD Planning

When planning Go changes, prefer the `devalexandre/golang-ddd-template` architecture unless repository documentation or existing code establishes a stronger local convention:

- entry points in `cmd/`;
- business rules in `internal/domain/<context>/`;
- infrastructure adapters in `internal/infra/`;
- support packages in `internal/helpers/`;
- tests colocated with the code they verify.

## Execution Rules

- Do not commit, push, create releases, delete branches, or run destructive commands unless explicitly requested.
- Use shell commands for inspection, formatting, tests, and builds.
- Keep edits reversible and easy to review.
- If the task becomes too large, complete the safest useful slice and report what remains.
- Run the fastest relevant verification first, then broader checks when useful.
- Do not claim a check passed unless it was run successfully or a previous phase recorded the exact result.

## Final Delivery

The final answer should lead with the outcome, then verification, then any remaining risk. Avoid long process narration unless the user asked for it.
