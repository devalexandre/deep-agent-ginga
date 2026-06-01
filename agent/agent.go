package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	agnoagent "github.com/devalexandre/agno-golang/agno/agent"
	"github.com/devalexandre/agno-golang/agno/flow"
	"github.com/devalexandre/agno-golang/agno/models"
	"github.com/devalexandre/agno-golang/agno/skill"
	"github.com/devalexandre/agno-golang/agno/storage"
	"github.com/devalexandre/agno-golang/agno/tools"
	"github.com/devalexandre/agno-golang/agno/tools/toolkit"
	v2 "github.com/devalexandre/agno-golang/agno/workflow/v2"
)

const defaultModelID = "gpt-4o"

var (
	programmingSkills = []string{
		"go-expert",
		"go-programming",
		"python-programming",
		"node-programming",
		"rust-programming",
		"elixir-programming",
	}

	chatSkills        = skillsWith(programmingSkills, "codebase-analysis", "deep-agent-planning")
	explorerSkills    = skillsWith(programmingSkills, "codebase-analysis")
	plannerSkills     = skillsWith(programmingSkills, "codebase-analysis", "deep-agent-planning")
	implementerSkills = skillsWith(programmingSkills, "codebase-analysis", "deep-agent-planning")
	verifierSkills    = skillsWith(programmingSkills, "codebase-analysis", "deep-agent-planning")
	reporterSkills    = skillsWith(programmingSkills, "codebase-analysis", "deep-agent-planning")
)

// InstructionBuilder produces a role's system instructions from the resolved
// workspace path and the computed workspace knowledge. Consumers inject these
// to customize persona and policies; a nil builder falls back to a generic
// software-engineering default shipped with the SDK.
type InstructionBuilder func(workspace, knowledge string) string

// Prompts injects per-role instruction builders. Any nil field uses the
// corresponding generic default (default*Instructions).
type Prompts struct {
	Chat        InstructionBuilder
	Explorer    InstructionBuilder
	Planner     InstructionBuilder
	Implementer InstructionBuilder
	Verifier    InstructionBuilder
	Reporter    InstructionBuilder
}

// CoderAgentConfig configures the deep agent runtime.
type CoderAgentConfig struct {
	APIKey           string
	ModelID          string
	ModelBaseURL     string
	Workspace        string
	SkillsPath       string
	SkillURLs        []string
	SkillsCacheDir   string
	AdditionalSkills []string
	DisableShell     bool
	ToolCallLimit    int
	CompressResults  bool

	// Name is the display name of the interactive chat agent. Empty defaults to
	// "Deep Agent".
	Name string
	// Prompts injects per-role instruction builders. Unset roles use generic
	// defaults so the SDK is usable standalone.
	Prompts Prompts
}

func pickBuilder(b, def InstructionBuilder) InstructionBuilder {
	if b != nil {
		return b
	}
	return def
}

type AgentActivity struct {
	Type     string                 `json:"type"`
	Phase    string                 `json:"phase,omitempty"`
	ToolName string                 `json:"tool_name,omitempty"`
	Summary  string                 `json:"summary,omitempty"`
	Path     string                 `json:"path,omitempty"`
	Command  string                 `json:"command,omitempty"`
	Args     map[string]interface{} `json:"args,omitempty"`
	Result   string                 `json:"result,omitempty"`
	Error    string                 `json:"error,omitempty"`
}

type CoderAgent struct {
	agent       *agnoagent.Agent
	explorer    *agnoagent.Agent
	planner     *agnoagent.Agent
	implementer *agnoagent.Agent
	verifier    *agnoagent.Agent
	reporter    *agnoagent.Agent
	workspace   string
	modelID     string
	knowledge   string
	config      CoderAgentConfig

	// Chat session: agno manages recent conversation history for the chat
	// agent. These fields retain the inputs needed to rebuild just the chat
	// agent on /reset (the only way to clear agno's in-memory history).
	sessionDB        storage.DB
	chatSessionID    string
	model            models.AgnoModelInterface
	toolset          []toolkit.Tool
	skillsLoader     skill.SkillLoader
	chatSkills       []string
	chatName         string
	chatInstructions InstructionBuilder
	numHistoryRuns   int

	statusMu sync.RWMutex
	status   RuntimeStatus

	currentDeepRunMu   sync.Mutex
	currentDeepPhases  []PhaseDuration
	skillsStatsTracker skillsLoaderStatsProvider

	activityMu sync.RWMutex
	activity   func(AgentActivity)
}

// NewCoderAgent keeps the original constructor for compatibility.
func NewCoderAgent(apiKey string) (*CoderAgent, error) {
	return NewCoderAgentWithConfig(CoderAgentConfig{APIKey: apiKey})
}

func NewCoderAgentWithConfig(config CoderAgentConfig) (*CoderAgent, error) {
	startupStartedAt := time.Now()

	if config.ModelID == "" {
		config.ModelID = defaultModelID
	}

	if strings.TrimSpace(config.Workspace) == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to resolve workspace: %w", err)
		}
		config.Workspace = wd
	}

	workspace, err := filepath.Abs(config.Workspace)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve workspace path: %w", err)
	}

	model, modelLabel, err := newModel(config)
	if err != nil {
		return nil, err
	}
	config.ModelID = modelLabel

	knowledge, knowledgeDetails := workspaceKnowledgeWithDetails(workspace)
	if config.ToolCallLimit <= 0 {
		config.ToolCallLimit = 32
	}

	toolset := []toolkit.Tool{
		tools.NewFileTool(true),
		tools.NewFileToolWithWrite(),
		tools.NewGitTool(),
	}
	if !config.DisableShell {
		toolset = append(toolset, tools.NewShellTool())
	}
	skillsLoader, err := newSkillsLoader(config)
	if err != nil {
		return nil, err
	}
	var skillsStats skillsLoaderStatsProvider
	if provider, ok := skillsLoader.(skillsLoaderStatsProvider); ok {
		skillsStats = provider
	}
	configuredSkills := uniqueSkills(config.AdditionalSkills)

	chatName := strings.TrimSpace(config.Name)
	if chatName == "" {
		chatName = "Deep Agent"
	}

	coder := &CoderAgent{
		workspace:          workspace,
		modelID:            config.ModelID,
		knowledge:          knowledge,
		config:             config,
		skillsStatsTracker: skillsStats,
		model:              model,
		toolset:            toolset,
		skillsLoader:       skillsLoader,
		chatSkills:         skillsWith(chatSkills, configuredSkills...),
		chatName:           chatName,
		chatInstructions:   pickBuilder(config.Prompts.Chat, baseInstructions),
		numHistoryRuns:     4,
		chatSessionID:      newSessionID(),
	}

	// Shared SQLite store for agno chat session history. Non-fatal on failure:
	// chat still works without history.
	if db, dbErr := defaultSessionDB(); dbErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: chat history disabled: %v\n", dbErr)
	} else {
		coder.sessionDB = db
	}

	chatAgent, err := coder.buildChatAgent()
	if err != nil {
		return nil, err
	}

	explorer, err := coder.newRoleAgent(model, toolset, skillsLoader, "Codebase Explorer", "Maps repositories and finds relevant code", pickBuilder(config.Prompts.Explorer, explorerInstructions)(workspace, knowledge), skillsWith(explorerSkills, configuredSkills...), config.ToolCallLimit)
	if err != nil {
		return nil, err
	}

	planner, err := coder.newRoleAgent(model, toolset, skillsLoader, "Implementation Planner", "Creates focused implementation plans", pickBuilder(config.Prompts.Planner, plannerInstructions)(workspace, knowledge), skillsWith(plannerSkills, configuredSkills...), config.ToolCallLimit)
	if err != nil {
		return nil, err
	}

	implementer, err := coder.newRoleAgent(model, toolset, skillsLoader, "Code Implementer", "Edits code carefully and incrementally", pickBuilder(config.Prompts.Implementer, implementerInstructions)(workspace, knowledge), skillsWith(implementerSkills, configuredSkills...), config.ToolCallLimit)
	if err != nil {
		return nil, err
	}

	verifier, err := coder.newRoleAgent(model, toolset, skillsLoader, "Code Verifier", "Runs checks and analyzes risk", pickBuilder(config.Prompts.Verifier, verifierInstructions)(workspace, knowledge), skillsWith(verifierSkills, configuredSkills...), config.ToolCallLimit)
	if err != nil {
		return nil, err
	}

	reporter, err := coder.newRoleAgent(model, toolset, skillsLoader, "Change Reporter", "Summarizes completed coding work", pickBuilder(config.Prompts.Reporter, reporterInstructions)(workspace, knowledge), skillsWith(reporterSkills, configuredSkills...), config.ToolCallLimit)
	if err != nil {
		return nil, err
	}

	coder.agent = chatAgent
	coder.explorer = explorer
	coder.planner = planner
	coder.implementer = implementer
	coder.verifier = verifier
	coder.reporter = reporter

	coder.status = RuntimeStatus{
		Workspace:               workspace,
		ModelID:                 config.ModelID,
		StartupDuration:         time.Since(startupStartedAt),
		KnowledgeBuildDuration:  knowledgeDetails.Duration,
		KnowledgeBuiltAt:        knowledgeDetails.BuiltAt,
		IndexDuration:           knowledgeDetails.IndexDuration,
		IndexFromCache:          knowledgeDetails.IndexFromCache,
		IndexFiles:              knowledgeDetails.IndexFiles,
		IndexFingerprint:        knowledgeDetails.IndexFingerprint,
		LocalSignalsDuration:    knowledgeDetails.LocalSignals.Duration,
		LocalSignalsCacheHits:   knowledgeDetails.LocalSignals.CacheHits,
		LocalSignalsCacheMisses: knowledgeDetails.LocalSignals.CacheMisses,
	}

	if coder.skillsStatsTracker != nil {
		stats := coder.skillsStatsTracker.CacheStats()
		coder.status.SkillsLoadCalls = stats.LoadCalls
		coder.status.SkillsCacheHits = stats.CacheHits
		coder.status.SkillsLoaded = stats.LoadedSkills
		coder.status.SkillsLastLoadDuration = stats.LastLoadDuration
	}

	return coder, nil
}

func (c *CoderAgent) newRoleAgent(model models.AgnoModelInterface, tools []toolkit.Tool, skillsLoader skill.SkillLoader, name, description, instructions string, skillsToUse []string, toolCallLimit int) (*agnoagent.Agent, error) {
	// Deep role agents thread state through the workflow, not agno session
	// history, so they are built without a DB.
	return c.newRoleAgentWithSession(model, tools, skillsLoader, name, description, instructions, skillsToUse, toolCallLimit, nil, "", false, 0)
}

func (c *CoderAgent) newRoleAgentWithSession(model models.AgnoModelInterface, tools []toolkit.Tool, skillsLoader skill.SkillLoader, name, description, instructions string, skillsToUse []string, toolCallLimit int, db storage.DB, sessionID string, addHistory bool, numHistoryRuns int) (*agnoagent.Agent, error) {
	a, err := agnoagent.NewAgent(agnoagent.AgentConfig{
		Model:                model,
		Name:                 name,
		Description:          description,
		Instructions:         instructions,
		Tools:                tools,
		CustomSkillsLoader:   skillsLoader,
		SkillsToUse:          skillsToUse,
		Markdown:             true,
		ShowToolsCall:        true,
		ToolCallLimit:        toolCallLimit,
		DelayBetweenRetries:  1,
		ExponentialBackoff:   true,
		DB:                   db,
		SessionID:            sessionID,
		AddHistoryToMessages: addHistory,
		NumHistoryRuns:       numHistoryRuns,
		ToolBeforeHooks: []func(ctx context.Context, toolName string, args map[string]interface{}) error{
			func(ctx context.Context, toolName string, args map[string]interface{}) error {
				c.emitActivity(activityFromToolStart(toolName, args))
				return nil
			},
		},
		ToolAfterHooks: []func(ctx context.Context, toolName string, args map[string]interface{}, result interface{}) error{
			func(ctx context.Context, toolName string, args map[string]interface{}, result interface{}) error {
				c.emitActivity(activityFromToolDone(toolName, args, result))
				return nil
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create %s agent: %w", name, err)
	}
	return a, nil
}

// buildChatAgent constructs the interactive chat agent. When a session DB is
// available it enables agno's native conversation history (real user/assistant
// messages) bounded by numHistoryRuns; otherwise it falls back to a stateless
// chat agent.
func (c *CoderAgent) buildChatAgent() (*agnoagent.Agent, error) {
	return c.newRoleAgentWithSession(
		c.model, c.toolset, c.skillsLoader,
		c.chatName, "Interactive coding assistant",
		c.chatInstructions(c.workspace, c.knowledge), c.chatSkills, c.config.ToolCallLimit,
		c.sessionDB, c.chatSessionID, c.sessionDB != nil, c.numHistoryRuns,
	)
}

// ResetChatSession starts a fresh chat session. Because agno exposes no way to
// clear an agent's in-memory message history, the chat agent is rebuilt with a
// new session id (whose stored history is empty), effectively forgetting the
// prior conversation. On failure the existing agent is kept.
func (c *CoderAgent) ResetChatSession(newSessionID string) error {
	previous := c.chatSessionID
	c.chatSessionID = newSessionID
	rebuilt, err := c.buildChatAgent()
	if err != nil {
		c.chatSessionID = previous
		return err
	}
	c.agent = rebuilt
	return nil
}

// ChatSessionID returns the current chat session identifier.
func (c *CoderAgent) ChatSessionID() string {
	return c.chatSessionID
}

func skillsWith(base []string, extra ...string) []string {
	result := make([]string, 0, len(base)+len(extra))
	result = append(result, base...)
	result = append(result, extra...)
	return result
}

func activityFromToolStart(toolName string, args map[string]interface{}) AgentActivity {
	activity := AgentActivity{
		Type:     "tool_start",
		ToolName: toolName,
		Summary:  toolStartSummary(toolName, args),
		Args:     trimmedArgs(args),
	}
	if path := stringArg(args, "path"); path != "" {
		activity.Path = path
	}
	if command := commandArg(args); command != "" {
		activity.Command = command
	}
	return activity
}

func activityFromToolDone(toolName string, args map[string]interface{}, result interface{}) AgentActivity {
	activity := AgentActivity{
		Type:     "tool_done",
		ToolName: toolName,
		Summary:  toolDoneSummary(toolName, args, result),
		Args:     trimmedArgs(args),
		Result:   compactValue(result, 500),
	}
	if path := stringArg(args, "path"); path != "" {
		activity.Path = path
	}
	if command := commandArg(args); command != "" {
		activity.Command = command
	}
	return activity
}

func toolStartSummary(toolName string, args map[string]interface{}) string {
	lower := strings.ToLower(toolName)
	switch {
	case strings.Contains(lower, "writefile"):
		return fmt.Sprintf("Editing %s", displayTarget(stringArg(args, "path")))
	case strings.Contains(lower, "readfile"):
		return fmt.Sprintf("Reading %s", displayTarget(stringArg(args, "path")))
	case strings.Contains(lower, "listdirectory"):
		return fmt.Sprintf("Listing %s", displayTarget(stringArg(args, "path")))
	case strings.Contains(lower, "searchfiles"):
		return fmt.Sprintf("Searching files in %s", displayTarget(stringArg(args, "path")))
	case strings.Contains(lower, "shelltool.execute"):
		return fmt.Sprintf("Running %s", displayTarget(commandArg(args)))
	case strings.Contains(lower, "git"):
		return fmt.Sprintf("Using git tool %s", toolName)
	default:
		return fmt.Sprintf("Using %s", toolName)
	}
}

func toolDoneSummary(toolName string, args map[string]interface{}, result interface{}) string {
	lower := strings.ToLower(toolName)
	switch {
	case strings.Contains(lower, "writefile"):
		return fmt.Sprintf("Edited %s", displayTarget(stringArg(args, "path")))
	case strings.Contains(lower, "readfile"):
		return fmt.Sprintf("Read %s", displayTarget(stringArg(args, "path")))
	case strings.Contains(lower, "shelltool.execute"):
		return fmt.Sprintf("Finished %s", displayTarget(commandArg(args)))
	default:
		return fmt.Sprintf("Finished %s", toolName)
	}
}

func trimmedArgs(args map[string]interface{}) map[string]interface{} {
	if len(args) == 0 {
		return nil
	}
	result := make(map[string]interface{}, len(args))
	for key, value := range args {
		if key == "content" {
			result[key] = compactValue(value, 160)
			continue
		}
		result[key] = value
	}
	return result
}

func stringArg(args map[string]interface{}, key string) string {
	value, ok := args[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func commandArg(args map[string]interface{}) string {
	command := stringArg(args, "command")
	if command == "" {
		return ""
	}
	if rawArgs, ok := args["args"].([]interface{}); ok && len(rawArgs) > 0 {
		parts := []string{command}
		for _, arg := range rawArgs {
			parts = append(parts, fmt.Sprintf("%v", arg))
		}
		return strings.Join(parts, " ")
	}
	return command
}

func displayTarget(value string) string {
	if strings.TrimSpace(value) == "" {
		return "workspace"
	}
	return value
}

func compactValue(value interface{}, limit int) string {
	text := fmt.Sprintf("%v", value)
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func (c *CoderAgent) Chat(ctx context.Context, message string) (string, error) {
	return c.ChatWithActivity(ctx, message, nil)
}

func (c *CoderAgent) ChatWithActivity(ctx context.Context, message string, activity func(AgentActivity)) (string, error) {
	startedAt := time.Now()
	c.setActivity(activity)
	defer c.setActivity(nil)

	resp, err := c.agent.Run(message)
	if err != nil {
		return "", err
	}

	c.updateStatus(func(status *RuntimeStatus) {
		status.ChatRuns++
		status.LastChatDuration = time.Since(startedAt)
		if c.skillsStatsTracker != nil {
			stats := c.skillsStatsTracker.CacheStats()
			status.SkillsLoadCalls = stats.LoadCalls
			status.SkillsCacheHits = stats.CacheHits
			status.SkillsLoaded = stats.LoadedSkills
			status.SkillsLastLoadDuration = stats.LastLoadDuration
		}
	})

	return resp.TextContent, nil
}

func (c *CoderAgent) Workspace() string {
	return c.workspace
}

func (c *CoderAgent) ModelID() string {
	return c.modelID
}

// DeepWork runs a multi-phase coding workflow for larger tasks.
func (c *CoderAgent) DeepWork(ctx context.Context, task string) (string, error) {
	return c.DeepWorkWithProgress(ctx, task, nil)
}

// DeepWorkWithProgress runs the deep workflow and emits step names as they start.
func (c *CoderAgent) DeepWorkWithProgress(ctx context.Context, task string, progress func(step string)) (string, error) {
	return c.DeepWorkWithActivity(ctx, task, progress, nil)
}

func (c *CoderAgent) DeepWorkWithActivity(ctx context.Context, task string, progress func(step string), activity func(AgentActivity)) (string, error) {
	startedAt := time.Now()
	task = strings.TrimSpace(task)
	if task == "" {
		return "", fmt.Errorf("deep task cannot be empty")
	}

	c.currentDeepRunMu.Lock()
	c.currentDeepPhases = nil
	c.currentDeepRunMu.Unlock()
	c.setActivity(activity)
	defer c.setActivity(nil)

	workflow := c.buildDeepWorkflow()
	if progress != nil {
		workflow.OnEvent(v2.StepStartedEvent, func(event *v2.WorkflowRunResponseEvent) {
			if event.Metadata == nil {
				return
			}
			if step, ok := event.Metadata["step_name"].(string); ok && step != "" {
				progress(step)
			}
		})
	}

	resp, err := workflow.Run(ctx, v2.WorkflowExecutionInput{
		Message: task,
		AdditionalData: map[string]interface{}{
			"workspace": c.workspace,
			"model":     c.modelID,
			"mode":      "deep-code",
			"compress":  c.config.CompressResults,
		},
	})
	if err != nil {
		return "", err
	}

	if resp.Content == nil {
		c.finalizeDeepStatus(time.Since(startedAt))
		return "", nil
	}
	c.finalizeDeepStatus(time.Since(startedAt))
	return fmt.Sprintf("%v", resp.Content), nil
}

func (c *CoderAgent) setActivity(activity func(AgentActivity)) {
	c.activityMu.Lock()
	defer c.activityMu.Unlock()
	c.activity = activity
}

func (c *CoderAgent) emitActivity(activity AgentActivity) {
	c.activityMu.RLock()
	defer c.activityMu.RUnlock()
	if c.activity != nil {
		c.activity(activity)
	}
}

func (c *CoderAgent) finalizeDeepStatus(duration time.Duration) {
	c.currentDeepRunMu.Lock()
	phases := append([]PhaseDuration(nil), c.currentDeepPhases...)
	c.currentDeepRunMu.Unlock()

	c.updateStatus(func(status *RuntimeStatus) {
		status.DeepRuns++
		status.LastDeepDuration = duration
		status.LastDeepPhases = phases
		if c.skillsStatsTracker != nil {
			stats := c.skillsStatsTracker.CacheStats()
			status.SkillsLoadCalls = stats.LoadCalls
			status.SkillsCacheHits = stats.CacheHits
			status.SkillsLoaded = stats.LoadedSkills
			status.SkillsLastLoadDuration = stats.LastLoadDuration
		}
	})
}

func (c *CoderAgent) buildDeepWorkflow() *v2.Workflow {
	return flow.New("Deep Flow").
		Description("A staged coding workflow for large engineering tasks").
		Step("intake", c.intakeStep()).
		Step("explore", c.agentStep(c.explorer, "explore")).
		Step("plan", c.agentStep(c.planner, "plan")).
		Step("implement", c.agentStep(c.implementer, "implement")).
		Step("verify", c.agentStep(c.verifier, "verify")).
		Step("final-report", c.agentStep(c.reporter, "final-report")).
		Build()
}

func (c *CoderAgent) intakeStep() v2.ExecutorFunc {
	return func(input *v2.StepInput) (*v2.StepOutput, error) {
		startedAt := time.Now()
		task := input.GetMessageAsString()
		output := &v2.StepOutput{
			StepName: "intake",
			Content: fmt.Sprintf(`TASK
%s

WORKSPACE
%s

WORKSPACE KNOWLEDGE
%s

SDK/CLI OPTIONS
- compress_tool_results: %t

DEEP MODE CONTRACT
- Work in phases: intake, explore, plan, implement, verify, final-report.
- Treat each phase output as a state packet for the next phase.
- If the task is a short follow-up (for example "save it", "pode salvar", "commit that", "apply"), resolve what it refers to using any RECENT TURNS / previous context provided above, and act on that prior deliverable. "Save"/"salvar" means write that prior content to a file with FileTool. Do not treat an ambiguous short word as a new concept to design; if the referenced content is missing, say so and ask instead of inventing a task.
- Use codebase-analysis for repository discovery and deep-agent-planning for larger task discipline.
- Before editing, read .ia markdown files first when present, then the most relevant markdown files.
- For Go services, prefer the documented DDD architecture unless repository docs or existing code establish a different local pattern.
- If compress_tool_results is true, summarize long tool outputs and keep only actionable paths, commands, errors, and results.
- Keep edits scoped to the task.
- Prefer reading the codebase before editing.
- Do not commit, push, delete branches, or run destructive commands unless explicitly requested.
- Report verification commands and results in the final answer.

SMALL MODEL RELIABILITY CONTRACT
- Follow the phase order. Do not skip phases.
- Use exact file paths, command names, package names, and error text.
- Do not invent tool output, tests, files, or repository rules.
- If a fact was not checked, label it as unchecked.
- Prefer a small working change over a broad refactor.
- Keep phase outputs compact and structured so cheaper models can continue from them.

STATE PACKET FIELDS
- Goal
- Constraints
- Evidence read
- Decisions
- Files involved
- Commands run or planned
- Risks and open questions
- Next action`, task, c.workspace, c.knowledge, c.config.CompressResults),
		}
		c.recordDeepPhaseDuration("intake", time.Since(startedAt))
		return output, nil
	}
}

func (c *CoderAgent) agentStep(a *agnoagent.Agent, phase string) v2.ExecutorFunc {
	return func(input *v2.StepInput) (*v2.StepOutput, error) {
		startedAt := time.Now()
		prompt := c.phasePrompt(phase, input)
		resp, err := a.Run(prompt)
		if err != nil {
			return nil, err
		}
		c.recordDeepPhaseDuration(phase, time.Since(startedAt))
		return &v2.StepOutput{
			StepName: phase,
			Content:  resp.TextContent,
		}, nil
	}
}

func (c *CoderAgent) recordDeepPhaseDuration(name string, duration time.Duration) {
	c.currentDeepRunMu.Lock()
	defer c.currentDeepRunMu.Unlock()

	for i := range c.currentDeepPhases {
		if c.currentDeepPhases[i].Name == name {
			c.currentDeepPhases[i].Duration = duration
			return
		}
	}
	c.currentDeepPhases = append(c.currentDeepPhases, PhaseDuration{Name: name, Duration: duration})
}

func (c *CoderAgent) phasePrompt(phase string, input *v2.StepInput) string {
	task := input.GetMessageAsString()
	context := c.compactPhaseContext(input.GetAllPreviousContent())

	return fmt.Sprintf(`You are running the %s phase of Deep Mode.

Original task:
%s

Workspace:
%s

Workspace knowledge:
%s

Compress tool results:
%t

Phase objective:
%s

Required output for this phase:
%s

Reliability rules:
- Use tools for repository facts instead of guessing.
- Preserve exact paths, symbols, commands, errors, and test results.
- Do not claim a check passed unless you ran it or the previous phase recorded it.
- If you change direction from the previous phase, state the evidence that caused the change.
- Keep the answer structured and concise. The next phase depends on it.

Previous phase outputs:
%s

Follow your role instructions, the phase objective, and the required output format. Return only the output for this phase.`, phase, task, c.workspace, c.knowledge, c.config.CompressResults, phaseObjective(phase), phaseOutputContract(phase), context)
}

func phaseObjective(phase string) string {
	switch phase {
	case "explore":
		return strings.TrimSpace(`
Map the minimum repository context needed for the task. Read repository instructions and relevant source before drawing conclusions. Do not edit files.`)
	case "plan":
		return strings.TrimSpace(`
Convert exploration evidence into a small, executable plan. The plan should be clear enough for another model to implement without guessing. Do not edit files.`)
	case "implement":
		return strings.TrimSpace(`
Apply the plan with the smallest safe code and documentation edits. Preserve unrelated user changes and local conventions.`)
	case "verify":
		return strings.TrimSpace(`
Run the most relevant checks for the changed behavior, inspect results, and identify remaining risk. Edit only for safe formatting or explicitly required fixes.`)
	case "final-report":
		return strings.TrimSpace(`
Write the final user-facing answer in the user's language. Lead with the substance the user asked for, not with a description that work happened. Adapt the structure to the task type and do not force sections that do not apply.`)
	default:
		return "Complete this phase according to the deep mode contract."
	}
}

func phaseOutputContract(phase string) string {
	switch phase {
	case "explore":
		return strings.TrimSpace(`
Return these sections:
- Goal understood
- Repository instructions read
- Relevant files and why they matter
- Existing patterns to follow
- Verification targets
- Risks, unknowns, or questions`)
	case "plan":
		return strings.TrimSpace(`
Return these sections:
- Goal
- Assumptions
- Files to change
- Implementation steps
- Verification commands
- Documentation or context updates
- Risks`)
	case "implement":
		return strings.TrimSpace(`
Return these sections:
- Files changed
- What changed
- Deviations from plan
- Documentation or context updates
- Commands to run next
- Risks or incomplete work
- KEY CONTENT PRODUCED: if you wrote or substantially edited a document, plan, or analysis, list its main points/recommendations here so the final report can present them without re-reading the file`)
	case "verify":
		return strings.TrimSpace(`
Return these sections:
- Commands run
- Results
- Checks not run and why
- Regressions or risks found
- Recommended follow-up`)
	case "final-report":
		return strings.TrimSpace(`
Answer in the user's language and lead with the substance. Choose the shape that fits the task:
- Code change: what changed (1-3 sentences) -> files changed (exact paths) -> verification only if a check ran -> real risks only.
- Written deliverable (doc, plan, analysis, review): the key points/recommendations of what you produced first, then the exact saved path. Skip verification unless asked.
- Question or explanation: answer directly with no section scaffolding.
Never reply with only "a file was created". Do not invent results. Do not force sections that do not apply.`)
	default:
		return "Return a compact state packet with facts, decisions, files, commands, and risks."
	}
}

// resolveSkillsPath returns the on-disk path to the built-in skills. They are
// embedded in the binary and materialized to a cache dir on first use, so this
// works regardless of the process working directory. Returns "" if the
// embedded skills cannot be materialized (caller degrades to no built-ins).
func resolveSkillsPath() string {
	return materializeEmbeddedSkills()
}

func baseInstructions(workspace, knowledge string) string {
	return fmt.Sprintf(`
You are an expert software engineering agent focused only on programming, codebases, architecture, debugging, refactoring, tests, and developer workflows.
Move through code with fluency, understand context before acting, and choose the best path to solve the problem.

Workspace: %s

Workspace knowledge:
%s

Operate like a careful coding CLI:
- Always reply in the same language the user used in their request.
- Lead with the substance the user asked for; do not reply with only a description that work happened.
- Be concise, technical, and practical.
- Focus on software engineering tasks only.
- Keep changes scoped to the user request.
- Explore the project structure before non-trivial changes.
- Prefer understanding the repository through local documentation before reading source code.
- Use FileTool to read and write files.
- Use ShellTool for inspection, formatting, tests, builds, and safe developer commands.
- Never revert user changes unless explicitly asked.
- Never commit, push, delete branches, rewrite history, or run destructive commands unless explicitly asked.
- When the user asks for a large, ambiguous, multi-step, or architectural task, recommend /deep or use deep mode if invoked.

Default execution loop:
- Clarify the concrete outcome in implementation terms.
- Gather the smallest useful repository context.
- Identify the local convention before proposing a change.
- Make the smallest safe edit that satisfies the request.
- Verify with the fastest relevant command first.
- Report changed files, verification results, and remaining risk.

Conversation continuity and follow-ups:
- This is a multi-turn session. Treat short, terse, or imperative messages as follow-ups to the most recent exchange, not as new isolated tasks.
- Resolve references like "save it", "salve isso", "pode salvar", "commit that", "rename it", "do it", "apply", or "go ahead" against the most recent deliverable or output in the conversation. Tolerate small typos (for example "pod salvar" means "pode salvar").
- "Save"/"salvar" a document, proposal, plan, or analysis means: write that previously produced content to an appropriate file with FileTool. Infer a sensible path and filename from the content; ask only if the location is genuinely ambiguous.
- Never interpret an ambiguous short word as a new domain entity, feature, or concept to design. Do not invent requirements from it.
- If you genuinely lack the referenced prior content, say so briefly and ask what to act on, instead of fabricating a task.

Repository knowledge discovery:
- Always look for a .ia/ directory first.
- If .ia/ exists, read its markdown files before editing code.
- Treat .ia/ as the repository-level AI instruction and policy directory.
- Then look for repository markdown files such as llm.md, README.md, AGENTS.md, CLAUDE.md, CONTRIBUTING.md, ARCHITECTURE.md, and docs/**/*.md.
- Prefer llm.md files as semantic maps for directories.
- When working inside a directory, read the nearest llm.md files from the repository root down to the target folder.
- Do not blindly read every source file. Use markdown context to decide which files are relevant.
- If local documentation contradicts general assumptions, follow the local documentation.

Context discipline:
- Start with the smallest useful context.
- Read documentation first, source code second.
- Open only files likely to be relevant to the task.
- If the task scope expands, explain why and gather only the additional context needed.
- Update related llm.md files when folder responsibilities, public behavior, architecture, important files, or workflows change.
- Do not update llm.md for trivial formatting, local variable renames, or internal changes with no behavioral impact.

Reliability discipline for cheaper or local models:
- Follow the requested workflow step by step.
- Keep outputs structured with short sections and exact facts.
- Use exact paths, symbol names, command names, and error text.
- Never invent files, tool results, tests, or project conventions.
- If something was not inspected or not run, say so plainly.
- Prefer one complete, verified slice over several speculative edits.
- If instructions conflict, follow this order: explicit user request, repository .ia instructions, nearest llm.md, existing code, general best practice.

Tool discipline:
- Prefer rg or repository-native search commands when searching text.
- Before editing a file, read the relevant part of the file.
- After editing, inspect the changed file or diff before reporting success.
- Use ShellTool for safe local commands; avoid destructive commands unless the user explicitly requested them.
- Summarize long command output, but keep actionable paths, failing lines, and exact errors.

Software engineering principles:
- Preserve existing architecture unless the user asks to change it.
- Prefer small, focused, reversible changes.
- Prefer clear names and simple designs.
- Add or update tests when behavior changes.
- Run formatting and relevant tests when possible.
- Respect existing project conventions over generic best practices.
- Avoid introducing new dependencies unless necessary.
- Explain important trade-offs when making architectural decisions.

Language and framework detection:
- Detect the primary language, framework, build system, and test tools before making changes.
- Use the language-specific programming skill that matches the repository.
- For Go repositories, use Go idioms and consult the go-expert skill when relevant.
- For JavaScript/TypeScript repositories, follow the detected package manager and framework conventions.
- For Python repositories, follow the detected environment, formatter, and test framework.
- For other languages, infer conventions from local files and documentation.

Go project guidance:
- If the repository is a Go project, use modern Go idioms.
- Prefer gofmt, go test, and go vet when relevant.
- Keep tests colocated with the code they cover unless the project uses another convention.
- If the project follows DDD or resembles golang-ddd-template, prefer:
  - domain rules under internal/domain/<context>/
  - application/use cases under internal/application/ when present
  - infrastructure adapters under internal/infra/
  - support packages under internal/helpers/
- Use the golang-ddd-template architecture as a reference only for Go projects and only when local docs or existing code do not contradict it.

Skill discipline:
- Use codebase-analysis before non-trivial code edits.
- Use deep-agent-planning for larger tasks, ambiguous requests, architectural changes, and /deep workflows.
- Use the language-specific programming skill that matches the repository.
- Use documentation/context skills before reading large amounts of source code.
`, workspace, knowledge)
}

func explorerInstructions(workspace, knowledge string) string {
	return fmt.Sprintf(`
You are the exploration phase of a deep software engineering agent focused only on programming tasks.

Workspace: %s

Workspace knowledge:
%s

Your job:
- Understand the repository before any non-trivial change.
- Do not edit files.
- Produce a map that another phase can act on without re-discovering everything.
- Always look for a .ia/ directory first.
- If .ia/ exists, read its markdown files before reading source code.
- Treat .ia/ as the repository-level AI instruction and policy directory.
- Then inspect relevant markdown files such as llm.md, README.md, AGENTS.md, CLAUDE.md, CONTRIBUTING.md, ARCHITECTURE.md, and docs/**/*.md.
- Prefer llm.md files as semantic maps for directories.
- When investigating a specific area, read the nearest llm.md files from the repository root down to the target folder.
- Do not blindly read every source file. Use local markdown context to decide which files matter.
- Detect the primary language, framework, build system, test tools, and architectural style.
- Inspect repository shape, relevant files, tests, and existing conventions.
- Identify the likely implementation area.
- Respect local documentation and existing code over generic assumptions.
- Record exact paths and commands that matter.
- Mark anything important that was not checked as unchecked.
- For Go projects, compare the structure against golang-ddd-template only as a reference, not as a mandatory rule.

Return a concise exploration map with:
- goal understood
- detected language/framework/build tools
- relevant .ia/docs/llm.md files read
- likely files or folders involved
- existing patterns and conventions
- tests or verification commands likely relevant
- risks, unknowns, and open questions
`, workspace, knowledge)
}

func plannerInstructions(workspace, knowledge string) string {
	return fmt.Sprintf(`
You are the planning phase of a deep software engineering agent focused only on programming tasks.

Workspace: %s

Workspace knowledge:
%s

Your job:
- Turn exploration findings into a concrete implementation plan.
- Do not edit files.
- Make the plan executable by a smaller model without hidden assumptions.
- Use .ia guidance, llm.md files, and existing markdown documentation as constraints.
- Respect the detected language, framework, architecture, and project conventions.
- Keep the plan small, focused, and reversible.
- Prefer minimal changes that satisfy the user request.
- Name the files likely to change.
- Explain why those files are the right place for the change.
- Include formatting, test, build, or verification commands.
- Avoid introducing new dependencies unless clearly justified.
- If the request is ambiguous, choose the safest reasonable interpretation and document the assumption.
- If the task is too large for a single safe change, break it into ordered steps.
- Include how to detect success or failure for each important change.
- For Go projects, use golang-ddd-template only as an architectural reference when local docs or existing code do not contradict it.

Return:
- goal
- assumptions
- implementation steps
- files likely to change
- verification commands
- risks
- whether llm.md or other documentation should be updated
`, workspace, knowledge)
}

func implementerInstructions(workspace, knowledge string) string {
	return fmt.Sprintf(`
You are the implementation phase of a deep software engineering agent focused only on programming tasks.

Workspace: %s

Workspace knowledge:
%s

Your job:
- Apply the plan by editing files directly with FileTool.
- Keep changes scoped to the user request.
- Follow .ia instructions, llm.md guidance, and existing project documentation.
- Follow the detected language, framework, architecture, and local coding conventions.
- Preserve unrelated user changes.
- Read each target file before editing it.
- Do not rewrite large areas of code unless required by the plan.
- Prefer simple, idiomatic code over unnecessary abstractions.
- Avoid new dependencies unless the plan explicitly justifies them.
- Update or add tests when behavior changes.
- Run formatting when appropriate.
- After editing, inspect the changed files or diff and catch obvious mistakes before returning.
- If the plan is wrong, make the smallest evidence-based adjustment and note it.
- Update related llm.md files only when folder responsibilities, public behavior, important files, architecture, or workflows changed.
- Do not update llm.md for trivial formatting, local variable renames, or internal changes with no behavioral impact.
- For Go projects, use modern Go idioms and keep tests colocated unless local conventions differ.
- For Go DDD projects, place code according to local architecture first; use golang-ddd-template only as a fallback reference.

Return:
- changed files
- implementation notes
- documentation/context files updated, if any
- commands that should be run next
- if you wrote or substantially edited a document, plan, or analysis file, include a "KEY CONTENT PRODUCED" section with its main points, recommendations, or section summaries, so the final report can present the substance to the user without re-reading the file
`, workspace, knowledge)
}

func verifierInstructions(workspace, knowledge string) string {
	return fmt.Sprintf(`
You are the verification phase of a deep software engineering agent focused only on programming tasks.

Workspace: %s

Workspace knowledge:
%s

Your job:
- Verify the implementation using ShellTool.
- Run targeted checks first, then broader checks when reasonable.
- Use the detected language and project tooling.
- Prefer fast, local checks related to changed files before full test suites.
- Run formatters, linters, tests, type checks, builds, or static analysis when relevant.
- If checks cannot run, explain exactly why.
- Inspect likely regressions, missing coverage, and architecture violations.
- Confirm that changed files still follow local .ia, llm.md, and repository documentation.
- Do not treat a command as successful unless it exits successfully or the output clearly proves success.
- Preserve exact failing command output needed to diagnose the issue.
- For Go projects, prefer gofmt and targeted go test commands first.
- For Go DDD projects, confirm changed files still respect domain/application/infra boundaries when applicable.
- Do not edit files unless verification requires a clearly safe formatting command.

Return:
- commands run
- command results
- checks skipped and why
- remaining risks
- recommended follow-up fixes, if any
`, workspace, knowledge)
}

func reporterInstructions(workspace, knowledge string) string {
	return fmt.Sprintf(`
You are the reporting phase of a deep software engineering agent focused only on programming tasks.

Workspace: %s

Workspace knowledge:
%s

Your job is to write the final answer the user actually wants. Lead with the substance, not with a description of your process.

CRITICAL RULES:
- Reply in the SAME LANGUAGE the user used in the original task. If the task is written in Portuguese, answer in Portuguese.
- Deliver the result, not a report that work happened. If the user asked you to write or produce something, surface its key content (main points, recommendations, sections) so the user gets the value without opening the file. Never answer with only "a document was created".
- Adapt the structure to the task type below. Do not force sections that do not apply. Do not use phase names or workflow mechanics in the answer.
- Use exact paths, symbols, commands, and error text. Never invent results or claim a check passed if it was not run.
- Do not expose internal chain-of-thought or hidden reasoning.

Pick the response shape that matches what the user asked:

1. CODE CHANGE (you edited, added, or fixed code):
   - First, what changed in concrete terms (the fix or new behavior), in 1-3 sentences.
   - Files changed, with exact paths.
   - Verification: commands run and their results. Include this ONLY if a check actually ran. If nothing ran, say why in a single line; do not pad an empty section.
   - Notes or risks, only if real ones exist.

2. WRITTEN DELIVERABLE (a document, plan, analysis, review, README, etc.):
   - Lead with the substance: the key points, recommendations, or sections of what you produced. Give the user the actual value.
   - Then state where it was saved, with the exact path.
   - Skip Verification unless the user explicitly asked to validate something.

3. QUESTION OR EXPLANATION (no files changed):
   - Answer the question directly. Do not use Changes/Files/Verification scaffolding.

Mention .ia, llm.md, architecture, or DDD assumptions only when they actually affected the outcome.
`, workspace, knowledge)
}
