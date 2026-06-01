---
name: context-compaction
description: "Compacts large contexts into a smaller working context using required, deferred, and discardable context classes."
metadata:
  version: "0.1.0"
  author: devalexandre
---

# Context Compaction Skill

This skill compacts large contexts into a smaller working context that fits within a target token budget. It classifies context into required, deferred, and discardable categories to determine what must be included, what can be summarized, and what can be removed.

Required context must be included in the compacted context.

If required context does not fit within the target token budget, the result must be marked as `incomplete`.

Deferred context may be excluded from the compacted context because it is not required for the active task.

Discardable context may be removed.

## Context Classes

Before compacting, classify the context into three groups.

### Required Context

Required context is information needed to continue the active task correctly.

Examples:

- The user's current goal
- Explicit requirements and constraints
- Decisions already made
- Pending tasks and next steps
- Exact code needed for the next action
- Exact error messages needed for debugging
- File paths, commands, IDs, schemas, configs, or API responses needed for correctness

Required context must appear in the compacted context.

### Deferred Context

Deferred context is useful background that is not needed for the active task.

Examples:

- Older discussion history
- Alternative approaches not currently selected
- Background explanations
- Related but inactive tasks
- Previously explored options

Deferred context may be summarized as a reference or excluded from the compacted context.

### Discardable Context

Discardable context does not affect the active task.

Examples:

- Duplicate messages
- Obsolete attempts
- Irrelevant side discussions
- Repeated explanations
- Low-value filler content

Discardable context may be removed.

## Tie-Breaker Policy

If the target token budget cannot contain all required context:

1. Mark the compaction as `incomplete`.
2. Include the user's current goal.
3. Include the highest-priority required context that fits.
4. State that safe continuation may require a larger token budget, external memory, files, or user-provided context.
5. Do not present the result as sufficient for safe continuation.

A truthful incomplete result is better than a complete-looking result that lacks required context.

## Compaction Strategy

When compacting:

1. Identify the active objective.
2. Classify context as required, deferred, or discardable.
3. Include required context.
4. Summarize deferred context only when it helps orientation.
5. Remove discardable context.
6. Preserve exact technical details when correctness depends on exact values.
7. Keep the result focused on the active task.
8. Mark the result as `incomplete` if required context does not fit.

## Output Format

```markdown
# Compacted Context

## Compaction Status
complete or incomplete

If incomplete, explain why safe continuation may require more context.

## Current Goal
Describe the user's current objective.

## Required Context
Include the information needed to continue the active task correctly.

## Key Decisions
List decisions already made.

## Technical Details
Include exact commands, code snippets, errors, schemas, paths, IDs, API responses, or configuration values required for correctness.

## Constraints and Preferences
List requirements, limitations, and user preferences.

## Pending Work
List what still needs to be done.

## Risks and Open Questions
List anything uncertain, risky, unresolved, or dependent on missing information.

## Deferred Context References
List only non-required topics or references that may be useful later.
```

## Rules

- Do not mark the result as `complete` unless all required context is included.
- Do not silently remove required context.
- Do not place required context under `Deferred Context References`.
- Do not summarize code when exact code is required for the next action.
- Do not change the meaning of previous decisions.
- Do not invent missing information.
- Do not hide uncertainty.
- Prefer active-task correctness over historical completeness.

## Verification

After compaction, verify that:

- If status is `complete`, the active task can continue from the compacted context.
- Required context is present.
- Exact technical details are preserved where needed.
- Deferred context is clearly separated from required context.
- Discardable context has been removed.
- Open questions and next steps are clear.
- The result is marked `incomplete` when required context does not fit.

## Failure Mode

If required context does not fit within the target token budget, return an incomplete compacted context.

Example:

```markdown
# Compacted Context

## Compaction Status
incomplete

## Reason
The required context for the active task exceeds the target token budget.

## Current Goal
...

## Required Context Included
...

## Continuation Requirement
Safe continuation may require a larger token budget, external memory, files, or user-provided context.
```