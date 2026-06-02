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
topic, with subtopics as `##` sections. Each entry carries a one-line
**description** and free-form **metadata** (search hints), stored as YAML
frontmatter. Only the lightweight layer — title + description + metadata — is
surfaced into context; the full body is fetched on demand. This keeps knowledge
findable and reusable without loading everything into every prompt.

## Entry format

Each entry is a `##` subtopic with optional YAML frontmatter:

```markdown
# Config

## Env bool parser
---
description: Reads a boolean flag from the environment with a default
metadata:
  method: "func envBool(name string, def bool) bool"
  tags: [env, config]
  file: agent/config.go
---
Returns the default when the var is unset; treats 0/false/no/off/empty as false.
```

The `description` is the one line the agent reads in context to decide whether to
recall the entry. The `metadata` holds search hints — method/function signatures,
tags, file paths, short comments — that make the entry locatable: `Brain_Recall`
matches against them too, not just the title and body. Entries without frontmatter
are still valid (they parse with an empty description and no metadata), so older
brain files keep working.

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

Tool: `Brain_Remember(topic, subtopic, content, description, metadata, replace)`.

- Choose a clear `topic` (e.g. "Architecture", "Build & Test", "Gotchas") and a
  specific `subtopic`.
- **Always write a `description`** — one line summarizing the entry, so future
  runs can judge relevance from the index without recalling the body.
- **Fill `metadata`** with search hints that locate the exact point: method or
  function signatures, tags, file paths, and short comments. These feed the
  search index, so an entry can be found by symbol name or path, not just title.
- Prefer refining an existing subtopic over creating near-duplicates. On append,
  content is appended, a non-empty description replaces the old one, and metadata
  keys are merged (new keys win); `replace=true` overwrites all three.
- Use `replace=true` to correct outdated knowledge; append (default) to add to it.
- Keep entries concise and factual, with exact paths, commands, and symbol names.

Tool: `Brain_Forget(topic, subtopic)` — remove knowledge that is wrong or stale.

## Discipline

- Recall first, work, then record what is worth keeping.
- One fact per subtopic; keep the topic/subtopic taxonomy shallow and consistent.
- Never store the user-facing answer itself — store the reusable lesson behind it.
