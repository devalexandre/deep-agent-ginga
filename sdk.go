package deepagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/devalexandre/deep-agent-ginga/agent"
)

// DeepAgentConfig configures Ginga when used as a Go SDK.
type DeepAgentConfig struct {
	APIKey              string
	Workspace           string
	Model               string
	ModelBaseURL        string
	EnableShell         bool
	CompressToolResults bool
	MaxIterations       int
	SkillsPath          string
	SkillURLs           []string
	Skills              []string

	// Brain enables the persistent, project-scoped knowledge base. The agent
	// recalls relevant knowledge before working and saves durable learnings
	// afterwards, sharing them across runs without inflating context.
	Brain bool
	// BrainDir overrides the brain root directory. Empty defaults to ~/.agno/brain.
	BrainDir string
	// BrainProject overrides the project name used to scope brain knowledge.
	// Empty defaults to the workspace directory name.
	BrainProject string
}

// DeepAgent is a reusable SDK wrapper around the Ginga deep workflow.
type DeepAgent struct {
	agent  *agent.DeepAgent
	config DeepAgentConfig
}

type runOptions struct {
	userID    string
	sessionID string
}

// RunOption configures one SDK run.
type RunOption func(*runOptions)

// WithUserID attaches a stable user identifier to the run context.
func WithUserID(userID string) RunOption {
	return func(options *runOptions) {
		options.userID = strings.TrimSpace(userID)
	}
}

// WithSessionID attaches a stable session identifier to the run context.
func WithSessionID(sessionID string) RunOption {
	return func(options *runOptions) {
		options.sessionID = strings.TrimSpace(sessionID)
	}
}

// NewDeepAgent creates a reusable Ginga SDK instance.
func NewDeepAgent(config DeepAgentConfig) (*DeepAgent, error) {
	resolved, err := resolveDeepAgentConfig(config)
	if err != nil {
		return nil, err
	}

	deepAgent, err := agent.NewDeepAgentWithConfig(agent.DeepAgentConfig{
		APIKey:           resolved.APIKey,
		ModelID:          normalizeModel(resolved.Model),
		ModelBaseURL:     resolved.ModelBaseURL,
		Workspace:        resolved.Workspace,
		SkillsPath:       resolved.SkillsPath,
		SkillURLs:        resolved.SkillURLs,
		SkillsCacheDir:   resolvedSkillsCacheDir(),
		AdditionalSkills: resolved.Skills,
		DisableShell:     !resolved.EnableShell,
		ToolCallLimit:    resolved.MaxIterations,
		CompressResults:  resolved.CompressToolResults,
		Brain:            resolved.Brain,
		BrainDir:         resolved.BrainDir,
		BrainProject:     resolved.BrainProject,
	})
	if err != nil {
		return nil, err
	}

	return &DeepAgent{agent: deepAgent, config: resolved}, nil
}

// RunDeepAgent runs one deep coding task using context.Background.
func RunDeepAgent(task string, config DeepAgentConfig, opts ...RunOption) (string, error) {
	return RunDeepAgentContext(context.Background(), task, config, opts...)
}

// RunDeepAgentContext runs one deep coding task with a caller-provided context.
func RunDeepAgentContext(ctx context.Context, task string, config DeepAgentConfig, opts ...RunOption) (string, error) {
	deepAgent, err := NewDeepAgent(config)
	if err != nil {
		return "", err
	}
	return deepAgent.Run(ctx, task, opts...)
}

// Run executes one deep coding task on a reusable SDK instance.
func (d *DeepAgent) Run(ctx context.Context, task string, opts ...RunOption) (string, error) {
	options := runOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	return d.agent.DeepWork(ctx, buildSDKTask(task, options))
}

func resolveDeepAgentConfig(config DeepAgentConfig) (DeepAgentConfig, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		config.APIKey = strings.TrimSpace(os.Getenv("GINGA_API_KEY"))
	}
	if strings.TrimSpace(config.Workspace) == "" {
		config.Workspace = "."
	}
	if strings.TrimSpace(config.Model) == "" {
		config.Model = "gpt-4o"
	}
	if config.MaxIterations <= 0 {
		config.MaxIterations = 8
	}
	config.Skills = uniqueClean(config.Skills)
	config.SkillURLs = uniqueClean(config.SkillURLs)
	return config, nil
}

func resolvedSkillsCacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".skills")
}

func buildSDKTask(task string, options runOptions) string {
	task = strings.TrimSpace(task)
	if options.userID == "" && options.sessionID == "" {
		return task
	}

	var b strings.Builder
	b.WriteString("SDK RUN CONTEXT\n")
	if options.userID != "" {
		b.WriteString("user_id: ")
		b.WriteString(options.userID)
		b.WriteString("\n")
	}
	if options.sessionID != "" {
		b.WriteString("session_id: ")
		b.WriteString(options.sessionID)
		b.WriteString("\n")
	}
	b.WriteString("\nTASK\n")
	b.WriteString(task)
	return b.String()
}

func normalizeModel(model string) string {
	return strings.TrimSpace(model)
}

func uniqueClean(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			result = append(result, part)
		}
	}
	return result
}
