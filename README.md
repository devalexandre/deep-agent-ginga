# deep-agent-ginga

A reusable **deep-agent engine + Go SDK** for building staged, autonomous coding/engineering agents on top of [agno-golang](https://github.com/devalexandre/agno-golang).

It provides the generic machinery — a deep mode that takes a coding task from start to finish (one agent loop by default, or a multi-phase pipeline), an interactive chat agent with session history, model selection across providers, workspace knowledge indexing, context compaction, an optional persistent **brain** for cross-run knowledge, and built-in skills — while leaving the **persona and prompt customization to the consumer**.

> This is the open-source base extracted from the (private) Ginga coding CLI. Ginga-specific prompts and product behavior live in the consumer, not here.

## Install

```sh
go get github.com/devalexandre/deep-agent-ginga
```

## SDK quick start

```go
package main

import (
	"context"
	"fmt"

	deepagent "github.com/devalexandre/deep-agent-ginga"
)

func main() {
	out, err := deepagent.RunDeepAgent("Review the auth package and suggest improvements", deepagent.DeepAgentConfig{
		Model:       "openai:gpt-4o",
		Workspace:   ".",
		EnableShell: true,
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(out)
}
```

Reusable instance:

```go
ag, _ := deepagent.NewDeepAgent(deepagent.DeepAgentConfig{Model: "openai:gpt-4o", Workspace: "."})
res, _ := ag.Run(context.Background(), "explain the build pipeline")
```

## Deep workflow

Deep mode has two strategies, chosen with `DeepStrategy`.

**`agent` (default).** One agent loop, the way Claude Code and Codex work. The
agent reads the repository instructions, finds code with `Code_Grep` and
`Code_Glob`, writes a short plan with `Plan_Update` for multi-step tasks and
keeps it current, edits with `Code_EditFile` (exact replacement), runs the
relevant checks through the shell and reports. Every model call does useful
work: there is no phase that starts over from a summary of the previous one.
The plan is reported as an activity (`type: "plan"`, a checklist in
`result`).

**`phased`.** A staged pipeline where each phase produces a compact state
packet the next phase consumes:

| Phase | Role | Edits files? |
|-------|------|--------------|
| `brain-recall` | Recall relevant prior knowledge (only when `Brain` is enabled) | no |
| `intake` | Frame the task, workspace, and contract | no |
| `explore` | Map the repository and find relevant code | no |
| `plan` | Turn findings into a small, executable plan | no |
| `implement` | Apply the plan with scoped edits | yes |
| `verify` | Run the most relevant checks and assess risk | rarely |
| `final-report` | Deliver the user-facing answer | no |
| `brain-update` | Persist durable, reusable learnings (only when `Brain` is enabled) | no |

With `Brain` enabled, the `agent` strategy recalls knowledge before the loop
and runs the brain curator after it.

### Tools

| Tool | What it does |
|------|--------------|
| `Code_ReadFile` | Numbered lines, read in windows (`offset`, `limit`) |
| `Code_EditFile` | Replace exact text; fails when it is missing or not unique |
| `Code_WriteFile` | Create a file or replace it whole |
| `Code_Grep` | Regex search over contents (ripgrep when installed) |
| `Code_Glob` | Find files by pattern (`**/*.go`) |
| `Code_ListDirectory` | List one directory |
| `ShellTool_Execute` | Run a command (builds, tests, git); database writes need confirmation |
| `Plan_Update` | The task plan (deep `agent` strategy only) |
| `Brain_*` | Project knowledge (only when `Brain` is enabled) |

There are no git tools: git goes through the shell, so the model is never
offered a push or commit tool.

### Output tokens

`max_tokens` follows the model: 128K for the Claude 5 family and 4.6+,
GPT-5; 64K for Claude Haiku/Sonnet 4.5 and Gemini 2.5; 32K for GPT-4.1, and
so on; 8,192 for a model the table does not know. Claude requests are capped
at 16,000 while they are sent without streaming, because the Anthropic SDK
refuses longer non-streaming calls. `MaxOutputTokens` overrides it.

For short conversational tasks, use the interactive chat agent instead:

```go
reply, _ := ag.Run(context.Background(), "what does the auth middleware do?")
```

## Configuration

`DeepAgentConfig` (SDK) — the most relevant fields:

| Field | Default | Purpose |
|-------|---------|---------|
| `Model` | `gpt-4o` | Provider-prefixed model id, e.g. `openai:gpt-4o`, `ollama:llama3.1:8b` |
| `ModelBaseURL` | — | Override the provider base URL |
| `APIKey` | `$GINGA_API_KEY` | Provider key (also reads provider-specific env vars) |
| `Workspace` | `.` | Repository root the agent operates on |
| `EnableShell` | `false` | Allow the shell tool for builds/tests/inspection |
| `CompressToolResults` | `false` | Summarize long tool output to save context |
| `MaxIterations` | `8` | Tool-call budget per agent |
| `DeepStrategy` | `agent` | `agent` (one loop) or `phased` (pipeline) |
| `MaxOutputTokens` | from the model | Most tokens in one model answer |
| `Skills` / `SkillsPath` / `SkillURLs` | — | Add extra skills (names, local dir, or remote URLs) |
| `Brain` | `false` | Enable the persistent knowledge base (see below) |
| `BrainProject` | workspace dir name | Project scope for brain knowledge |
| `BrainDir` | `~/.agno/brain` | Brain root directory |

The lower-level `agent.DeepAgentConfig` exposes the same options plus per-role
`Prompts` and the chat agent `Name`.

## Customizing prompts (the `agent` package)

The lower-level `agent` package lets you inject per-role instructions and a display name, while keeping generic defaults when you don't:

```go
import "github.com/devalexandre/deep-agent-ginga/agent"

a, err := agent.NewDeepAgentWithConfig(agent.DeepAgentConfig{
	Model: "openai:gpt-4o",
	Name:  "MyAgent",
	Prompts: agent.Prompts{
		// func(workspace, knowledge string) string
		Chat:     myChatInstructions,
		Reporter: myReporterInstructions,
		// Explorer/Planner/Implementer/Verifier fall back to generic defaults
	},
})
```

Any unset role builder uses a neutral software-engineering default, so the SDK is usable standalone.

## Brain — persistent, shared knowledge

The **brain** is an optional, persistent knowledge base scoped per project and
shared across agents and runs. The agent recalls relevant prior learnings before
working and saves durable ones afterwards — so knowledge compounds over time
**without inflating context**: only a lightweight index — each entry's title, a
one-line **description**, and free-form **metadata** (search hints) — is surfaced
into prompts; the full body is fetched on demand.

```go
out, _ := deepagent.RunDeepAgent("Add a health endpoint", deepagent.DeepAgentConfig{
	Model:     "openai:gpt-4o",
	Workspace: ".",
	Brain:     true, // enable the brain
	// BrainProject: "my-service", // defaults to the workspace dir name
	// BrainDir:     "/custom/root", // defaults to ~/.agno/brain
})
```

When `Brain` is enabled:

- The deep workflow gains a **`brain-recall` first step** (recalls knowledge
  relevant to the task) and a **`brain-update` last step** (persists only durable,
  reusable learnings).
- Every agent gets a `Brain` tool (`Brain_Recall`, `Brain_ListTopics`,
  `Brain_Remember`, `Brain_Forget`) and the `brain` skill that governs its use.

### Entry format

Each entry is a `## ` subtopic with optional YAML frontmatter carrying a
`description` and `metadata`. Both are optional — entries without frontmatter
parse as plain content, so older brain files keep working unchanged.

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

The `description` is the one line the agent reads in context to judge relevance.
The `metadata` holds search hints — method/function signatures, tags, file paths,
short comments — that make the entry locatable: `Brain_Recall` matches against
them too, not just the title and body. `Brain_Remember` accepts `description` and
`metadata` alongside `topic`/`subtopic`/`content`; on append the description is
updated and metadata keys are merged (new keys win), and `replace=true`
overwrites all three.

### On-disk layout

```
~/.agno/brain/<project>/
  <topic-slug>.md   # one file per topic; subtopics are "## " sections
  index.md          # auto-generated catalogue (titles + descriptions + metadata, no bodies)
```

Knowledge is plain markdown, so it is easy to read, edit, and version by hand.
The brain stores durable facts (architecture, conventions, build/test commands,
gotchas) — never transient task state or secrets.

## Built-in skills

Default skills (Go/Python/Node/Rust/Elixir programming, codebase analysis, planning, and `brain`) are embedded in the binary and materialized to a per-version cache directory on first use — no working-directory assumptions. The `brain` skill is activated automatically when `Brain` is enabled. Add your own via `SkillsPath` or `SkillURLs` in the config.

## Requirements

- Go 1.25+
- A model provider key (e.g. `OPENAI_API_KEY` / `GINGA_API_KEY`) depending on the configured model.

## Database safety

A command that changes data in a database is never executed without the
user's explicit confirmation of that exact command. The check runs in code,
before the tool executes; it does not depend on the model following its
instructions.

```go
coder, err := agent.NewDeepAgentWithConfig(agent.DeepAgentConfig{
    ModelID: "ollama:qwen3.5:4b",
    ConfirmDataChange: func(req guard.Request) bool {
        fmt.Printf("%s\n%s\nRun it? ", req.Reason, req.Command)
        return askUser() // true only on an explicit yes
    },
})
```

With `ConfirmDataChange` unset nobody can be asked, and every flagged command
is blocked. Flagged: SQL that writes (`INSERT`, `UPDATE`, `DELETE`, `TRUNCATE`,
`DROP`, `ALTER TABLE`, `MERGE`, `COPY ... FROM`), SQL files handed to a
database client, migration and restore tools, and the write commands of
`mongosh` and `redis-cli`. See the `guard` package for the details and its
limit: it reads the command, not what a program does internally.

## Local models (Ollama)

For `ollama:` models the agent asks the server about the model at startup and
fails with the fix when it does not exist or cannot call tools. Every request
carries `num_ctx` (`ContextTokens`, default 8192), because Ollama otherwise
loads models with 4,096 tokens and silently cuts the prompt. Reasoning is
turned off for thinking models, which tend to stop without an answer.
