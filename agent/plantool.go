package agent

import (
	"fmt"
	"strings"
	"sync"

	"github.com/devalexandre/agno-golang/agno/tools/toolkit"
)

// PlanTool keeps the task list of a run, as update_plan does in Codex and
// TodoWrite in Claude Code. Planning happens inside the agent loop: the plan
// costs no extra model call, and the model keeps it current as it works.
type PlanTool struct {
	toolkit.Toolkit
	mu       sync.Mutex
	steps    []PlanStep
	onChange func([]PlanStep)
}

// PlanStep is one item of the plan. Status is "todo", "doing" or "done".
type PlanStep struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

type PlanUpdateParams struct {
	Steps []string `json:"steps" description:"The whole plan, in order. Start each step with its status: \"todo: \", \"doing: \" or \"done: \". Keep exactly one step as doing while work remains." required:"true"`
}

// NewPlanTool builds the tool. onChange, when set, receives every new plan.
func NewPlanTool(onChange func([]PlanStep)) *PlanTool {
	t := &PlanTool{onChange: onChange}
	t.Toolkit = toolkit.NewToolkit()
	t.Toolkit.Name = "Plan"
	t.Toolkit.Description = "Keep the task plan."
	t.Toolkit.Register("Update", "Write the plan for a task with several steps, then update it as steps finish. Send the whole list every time. Skip it for a single obvious step.", t, t.Update, PlanUpdateParams{})
	return t
}

func (t *PlanTool) Update(params PlanUpdateParams) (interface{}, error) {
	if len(params.Steps) == 0 {
		return nil, fmt.Errorf("steps is empty")
	}
	steps := make([]PlanStep, 0, len(params.Steps))
	doing := 0
	for _, raw := range params.Steps {
		step := parsePlanStep(raw)
		if step.Text == "" {
			continue
		}
		if step.Status == "doing" {
			doing++
		}
		steps = append(steps, step)
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("steps has no text")
	}

	t.mu.Lock()
	t.steps = steps
	onChange := t.onChange
	t.mu.Unlock()
	if onChange != nil {
		onChange(append([]PlanStep(nil), steps...))
	}

	remaining := 0
	for _, step := range steps {
		if step.Status != "done" {
			remaining++
		}
	}
	answer := fmt.Sprintf("Plan updated: %d step(s), %d left.", len(steps), remaining)
	if remaining > 0 && doing != 1 {
		answer += " Mark exactly one step as doing."
	}
	return answer, nil
}

// Steps returns the current plan.
func (t *PlanTool) Steps() []PlanStep {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]PlanStep(nil), t.steps...)
}

// Reset clears the plan between runs.
func (t *PlanTool) Reset() {
	t.mu.Lock()
	t.steps = nil
	t.mu.Unlock()
}

func parsePlanStep(raw string) PlanStep {
	text := strings.TrimSpace(raw)
	lower := strings.ToLower(text)
	for _, prefix := range []struct{ marker, status string }{
		{"done:", "done"}, {"[x]", "done"}, {"doing:", "doing"}, {"in_progress:", "doing"}, {"[>]", "doing"},
		{"todo:", "todo"}, {"pending:", "todo"}, {"[ ]", "todo"},
	} {
		if strings.HasPrefix(lower, prefix.marker) {
			return PlanStep{Text: strings.TrimSpace(text[len(prefix.marker):]), Status: prefix.status}
		}
	}
	return PlanStep{Text: text, Status: "todo"}
}

// FormatPlan renders a plan as a checklist.
func FormatPlan(steps []PlanStep) string {
	var b strings.Builder
	for _, step := range steps {
		mark := "[ ]"
		switch step.Status {
		case "done":
			mark = "[x]"
		case "doing":
			mark = "[>]"
		}
		fmt.Fprintf(&b, "%s %s\n", mark, step.Text)
	}
	return b.String()
}
