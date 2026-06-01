# deep-agent-ginga

A reusable **deep-agent engine + Go SDK** for building staged, autonomous coding/engineering agents on top of [agno-golang](https://github.com/devalexandre/agno-golang).

It provides the generic machinery — a multi-phase deep workflow (intake → explore → plan → implement → verify → final-report), an interactive chat agent with session history, model selection across providers, workspace knowledge indexing, context compaction, and built-in skills — while leaving the **persona and prompt customization to the consumer**.

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

## Customizing prompts (the `agent` package)

The lower-level `agent` package lets you inject per-role instructions and a display name, while keeping generic defaults when you don't:

```go
import "github.com/devalexandre/deep-agent-ginga/agent"

a, err := agent.NewCoderAgentWithConfig(agent.CoderAgentConfig{
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

## Built-in skills

Default skills (Go/Python/Node/Rust/Elixir programming, codebase analysis, planning, etc.) are embedded in the binary and materialized to a per-version cache directory on first use — no working-directory assumptions. Add your own via `SkillsPath` or `SkillURLs` in the config.

## Requirements

- Go 1.25+
- A model provider key (e.g. `OPENAI_API_KEY` / `GINGA_API_KEY`) depending on the configured model.
