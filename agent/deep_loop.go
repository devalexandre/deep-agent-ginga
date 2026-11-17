package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	// DeepStrategyAgent runs deep mode as one agent loop (the default).
	DeepStrategyAgent = "agent"
	// DeepStrategyPhased runs the explore -> plan -> implement -> verify ->
	// report pipeline.
	DeepStrategyPhased = "phased"
)

// deepStrategy normalizes DeepAgentConfig.DeepStrategy.
func deepStrategy(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), DeepStrategyPhased) {
		return DeepStrategyPhased
	}
	return DeepStrategyAgent
}

// deepToolCallLimit is the tool budget of the single deep agent. The phased
// pipeline gave each of its five agents ToolCallLimit calls; one agent doing
// all the work gets a comparable total.
func deepToolCallLimit(perAgent int) int {
	if perAgent <= 0 {
		perAgent = 32
	}
	return perAgent * 3
}

// runDeepLoop runs deep mode with the "agent" strategy: one agent that
// explores, plans with the Plan tool, edits, verifies and reports in a single
// loop. Each model call does useful work, instead of each phase starting
// over from a summary of the one before.
func (c *DeepAgent) runDeepLoop(ctx context.Context, task string, progress func(step string)) (string, error) {
	if progress != nil {
		progress("agent")
	}
	c.plan.Reset()

	prompt := task
	if c.brainStore != nil {
		if recalled := c.brainRecallText(task); recalled != "" {
			prompt = recalled + "\n\nTASK\n" + task
		}
	}

	startedAt := time.Now()
	resp, err := c.lead.Run(prompt)
	if err != nil {
		return "", err
	}
	c.recordDeepPhaseDuration("agent", time.Since(startedAt))
	answer := strings.TrimSpace(resp.TextContent)

	if c.brainStore != nil && c.curator != nil {
		if progress != nil {
			progress("brain-update")
		}
		curatorStartedAt := time.Now()
		_, curatorErr := c.curator.Run(fmt.Sprintf("Task:\n%s\n\nWhat the agent reported:\n%s\n\nPlan:\n%s", task, answer, FormatPlan(c.plan.Steps())))
		c.recordDeepPhaseDuration("brain-update", time.Since(curatorStartedAt))
		if curatorErr != nil {
			c.emitActivity(AgentActivity{Type: "warning", Summary: "brain update failed", Error: curatorErr.Error()})
		}
	}
	return answer, nil
}

// onPlanChange shows the plan to whoever follows the run.
func (c *DeepAgent) onPlanChange(steps []PlanStep) {
	c.emitActivity(AgentActivity{
		Type:     "plan",
		ToolName: "Plan_Update",
		Summary:  "Plan updated",
		Result:   FormatPlan(steps),
	})
}

// deepAgentInstructions guide the single deep agent. They keep the
// discipline of the phased pipeline (read before editing, plan, verify,
// honest report) in one loop, in the style of Claude Code and Codex.
func deepAgentInstructions(workspace, knowledge string) string {
	return "You are a software engineering agent. " + DeepAgentGuidance(workspace, knowledge)
}

// DeepAgentGuidance is how the deep agent works, without who it is, so an
// application can put its own identity in front of it.
func DeepAgentGuidance(workspace, knowledge string) string {
	return fmt.Sprintf(`You take a task from start to finish in this run: understand it, change the code, verify the change and report.

Workspace: %s

Workspace knowledge:
%s

How to work
- Understand before changing. Read the repository instructions that apply (.ia/, AGENTS.md, CLAUDE.md, llm.md, README.md, CONTRIBUTING.md) and the code you are going to touch. Find code with Code_Grep and Code_Glob instead of listing directories one by one.
- Ask for independent things together: when you need several files or searches, request all of them in the same turn.
- For a task with several steps, write a short plan with Plan_Update before editing and keep it current as you go. Skip the plan for a single obvious step.
- Change existing files with Code_EditFile: read the region first and copy old_string exactly. Use Code_WriteFile for new files or a full rewrite.
- Keep the change to what the task asks and follow the conventions already in the code. If you notice an unrelated problem (a failing test elsewhere, a bug in another function), mention it in the final answer instead of fixing it. Do not change tests to make them pass unless the task asks for it.
- Verify: after changing code, run the most relevant checks through the shell (tests, build, vet, lint, type check), read the output and fix what you broke. Start with the narrowest check, then the wider one. A failure that was there before your change is not yours to fix: report it.
- Do not commit, push, delete branches, rewrite history or run destructive commands unless the task asks for it.
- Never invent files, tool results, test runs or conventions. If you did not check something, say so.
- A short task like "save it" or "apply", in any language, refers to the previous deliverable given in the task text; if it is missing, say so instead of inventing a task.

Final answer
- These instructions are in English; the task may be in any language. Reply in the language of the task, keep code and identifiers in the repository's language, and quote commands and errors untranslated. Lead with the substance.
- Code change: what changed and why in 1-3 sentences, the files changed (exact paths), the checks you ran and their result, and real risks only.
- Written deliverable: its key points, then the path where you saved it.
- Question or analysis: answer directly, without section scaffolding.`, workspace, knowledge)
}
