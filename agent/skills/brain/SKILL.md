---
name: brain
description: "Use the persistent, project-scoped brain to recall prior learnings before working and to save durable, reusable knowledge after working, without inflating context."
metadata:
  version: "0.1.0"
  author: deep-agent-ginga
---

# Brain Skill

The brain is a persistent, project-scoped knowledge base shared across agents and
runs. It lives on disk (default `~/.agno/brain/<project>/`), one markdown file per
topic, with subtopics as `##` sections. Only the index (titles) is surfaced into
context; full content is fetched on demand. This keeps knowledge reusable without
loading everything into every prompt.

Use this skill whenever the brain is enabled.

## When to recall

- At the start of a task, before exploring the repository.
- When you hit a question the project likely answered before (build/test commands,
  architecture, conventions, environment quirks, known gotchas).

Tools:

- `Brain_ListTopics` — cheap overview of what is already known (titles only).
- `Brain_Recall(query, topic)` — fetch matching content. Use keywords from the
  task; pass `topic` to read one topic in full.

Treat recalled knowledge as a strong prior, not ground truth: verify it against
the live repository before relying on it, since the code may have changed.

## When to remember

Save knowledge only when it is **durable and reusable** across future runs:

- Architecture and module responsibilities.
- Stable conventions (naming, layout, error handling, testing patterns).
- Build, test, lint, and run commands that actually worked.
- Environment/setup quirks and required tooling.
- Non-obvious gotchas, pitfalls, and their fixes.

Do **not** save:

- Transient task state or one-off request details.
- Secrets, credentials, tokens, or private data.
- Speculative ideas you did not verify.
- Anything already stored (check `Brain_ListTopics` first).

Tool: `Brain_Remember(topic, subtopic, content, replace)`.

- Choose a clear `topic` (e.g. "Architecture", "Build & Test", "Gotchas") and a
  specific `subtopic`.
- Prefer refining an existing subtopic over creating near-duplicates.
- Use `replace=true` to correct outdated knowledge; append (default) to add to it.
- Keep entries concise and factual, with exact paths, commands, and symbol names.

Tool: `Brain_Forget(topic, subtopic)` — remove knowledge that is wrong or stale.

## Discipline

- Recall first, work, then record what is worth keeping.
- One fact per subtopic; keep the topic/subtopic taxonomy shallow and consistent.
- Never store the user-facing answer itself — store the reusable lesson behind it.
