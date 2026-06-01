# deep-agent-ginga

A reusable **deep-agent engine + Go SDK** for building staged, autonomous coding/engineering agents on top of [agno-golang](https://github.com/devalexandre/agno-golang).

It provides the generic machinery — a multi-phase deep workflow (intake → explore → plan → implement → verify → final-report), an interactive chat agent with session history, model selection across providers, workspace knowledge indexing, context compaction, an optional persistent **brain** for cross-run knowledge, and built-in skills — while leaving the **persona and prompt customization to the consumer**.

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

A deep run is a staged pipeline where each phase produces a compact state packet
the next phase consumes:

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
**without inflating context**: only a lightweight index (topic/subtopic titles)
is surfaced into prompts; full content is fetched on demand.

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

### On-disk layout

```
~/.agno/brain/<project>/
  <topic-slug>.md   # one file per topic; subtopics are "## " sections
  index.md          # auto-generated table of contents (titles only)
```

Knowledge is plain markdown, so it is easy to read, edit, and version by hand.
The brain stores durable facts (architecture, conventions, build/test commands,
gotchas) — never transient task state or secrets.

## Built-in skills

Default skills (Go/Python/Node/Rust/Elixir programming, codebase analysis, planning, and `brain`) are embedded in the binary and materialized to a per-version cache directory on first use — no working-directory assumptions. The `brain` skill is activated automatically when `Brain` is enabled. Add your own via `SkillsPath` or `SkillURLs` in the config.

## Requirements

- Go 1.25+
- A model provider key (e.g. `OPENAI_API_KEY` / `GINGA_API_KEY`) depending on the configured model.
