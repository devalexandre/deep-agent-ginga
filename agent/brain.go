package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/devalexandre/agno-golang/agno/tools/toolkit"
)

// BrainStore is a persistent, file-based knowledge base shared across agents and
// runs. Knowledge is scoped per project and organized by topic and subtopic so
// it can be recalled on demand instead of being loaded wholesale into context.
//
// On-disk layout (root defaults to ~/.agno/brain):
//
//	<root>/<project>/<topic-slug>.md   one file per topic, subtopics are "## " sections
//	<root>/<project>/index.md          auto-generated table of contents (titles only)
//
// The index is intentionally small (titles, no bodies) so it can be injected
// into agent instructions cheaply; full content is fetched with Recall.
type BrainStore struct {
	root    string
	project string
	mu      sync.Mutex
}

// BrainSection is one subtopic entry within a topic.
type BrainSection struct {
	Subtopic string `json:"subtopic"`
	Content  string `json:"content"`
}

// BrainTopic groups the subtopics stored under a single topic.
type BrainTopic struct {
	Topic    string         `json:"topic"`
	Slug     string         `json:"slug"`
	Sections []BrainSection `json:"sections"`
}

// BrainMatch is a single hit returned by Recall.
type BrainMatch struct {
	Topic    string `json:"topic"`
	Subtopic string `json:"subtopic"`
	Content  string `json:"content"`
}

const brainGeneralSubtopic = "General"

// NewBrainStore builds a store rooted at root for the given project. An empty
// root defaults to ~/.agno/brain; an empty project defaults to "default".
func NewBrainStore(root, project string) (*BrainStore, error) {
	if strings.TrimSpace(root) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("brain: cannot resolve home dir: %w", err)
		}
		root = filepath.Join(home, ".agno", "brain")
	}
	project = brainSlug(project)
	if project == "" {
		project = "default"
	}
	store := &BrainStore{root: root, project: project}
	if err := os.MkdirAll(store.projectDir(), 0o755); err != nil {
		return nil, fmt.Errorf("brain: cannot create project dir: %w", err)
	}
	return store, nil
}

// Project returns the resolved project slug.
func (b *BrainStore) Project() string { return b.project }

// Dir returns the absolute project directory.
func (b *BrainStore) Dir() string { return b.projectDir() }

func (b *BrainStore) projectDir() string { return filepath.Join(b.root, b.project) }

func (b *BrainStore) topicPath(slug string) string {
	return filepath.Join(b.projectDir(), slug+".md")
}

// Remember upserts a subtopic section under a topic. When replace is true (or
// the subtopic does not yet exist) the content overwrites the section; when
// replace is false and the section exists, content is appended to it.
func (b *BrainStore) Remember(topic, subtopic, content string, replace bool) error {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return fmt.Errorf("brain: topic is required")
	}
	subtopic = strings.TrimSpace(subtopic)
	if subtopic == "" {
		subtopic = brainGeneralSubtopic
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("brain: content is required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	slug := brainSlug(topic)
	existing, _ := b.readTopic(slug)
	title := topic
	if existing != nil && existing.Topic != "" {
		title = existing.Topic // preserve original casing/title once created
	}

	var sections []BrainSection
	if existing != nil {
		sections = existing.Sections
	}

	found := false
	for i := range sections {
		if strings.EqualFold(sections[i].Subtopic, subtopic) {
			if replace {
				sections[i].Content = content
			} else {
				sections[i].Content = strings.TrimSpace(sections[i].Content + "\n\n" + content)
			}
			found = true
			break
		}
	}
	if !found {
		sections = append(sections, BrainSection{Subtopic: subtopic, Content: content})
	}

	if err := b.writeTopic(slug, title, sections); err != nil {
		return err
	}
	return b.rebuildIndex()
}

// Forget removes a subtopic from a topic. When subtopic is empty the whole
// topic file is removed. Missing entries are treated as success.
func (b *BrainStore) Forget(topic, subtopic string) error {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return fmt.Errorf("brain: topic is required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	slug := brainSlug(topic)
	if strings.TrimSpace(subtopic) == "" {
		_ = os.Remove(b.topicPath(slug))
		return b.rebuildIndex()
	}

	existing, err := b.readTopic(slug)
	if err != nil || existing == nil {
		return b.rebuildIndex()
	}
	kept := existing.Sections[:0]
	for _, s := range existing.Sections {
		if !strings.EqualFold(s.Subtopic, strings.TrimSpace(subtopic)) {
			kept = append(kept, s)
		}
	}
	if len(kept) == 0 {
		_ = os.Remove(b.topicPath(slug))
		return b.rebuildIndex()
	}
	if err := b.writeTopic(slug, existing.Topic, kept); err != nil {
		return err
	}
	return b.rebuildIndex()
}

// Recall returns knowledge matching query. When topic is non-empty the search
// is restricted to that topic. An empty query with a topic returns the whole
// topic; an empty query with no topic returns nothing (use ListTopics instead).
func (b *BrainStore) Recall(query, topic string) ([]BrainMatch, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	topics, err := b.loadTopics()
	if err != nil {
		return nil, err
	}
	terms := brainTerms(query)
	var matches []BrainMatch
	for _, t := range topics {
		if topic != "" && !strings.EqualFold(t.Topic, topic) && !strings.EqualFold(t.Slug, brainSlug(topic)) {
			continue
		}
		for _, s := range t.Sections {
			if len(terms) == 0 || brainSectionMatches(t.Topic, s, terms) {
				matches = append(matches, BrainMatch{Topic: t.Topic, Subtopic: s.Subtopic, Content: s.Content})
			}
		}
	}
	return matches, nil
}

// ListTopics returns every topic with its subtopic titles (no bodies).
func (b *BrainStore) ListTopics() ([]BrainTopic, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.loadTopics()
}

// Index returns the markdown table of contents for the project (titles only).
func (b *BrainStore) Index() (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	topics, err := b.loadTopics()
	if err != nil {
		return "", err
	}
	return b.renderIndex(topics), nil
}

// --- internal helpers ---

func (b *BrainStore) loadTopics() ([]BrainTopic, error) {
	entries, err := os.ReadDir(b.projectDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var topics []BrainTopic
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == "index.md" {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".md")
		t, err := b.readTopic(slug)
		if err != nil || t == nil {
			continue
		}
		topics = append(topics, *t)
	}
	sort.Slice(topics, func(i, j int) bool {
		return strings.ToLower(topics[i].Topic) < strings.ToLower(topics[j].Topic)
	})
	return topics, nil
}

func (b *BrainStore) readTopic(slug string) (*BrainTopic, error) {
	data, err := os.ReadFile(b.topicPath(slug))
	if err != nil {
		return nil, err
	}
	return parseTopic(slug, string(data)), nil
}

func (b *BrainStore) writeTopic(slug, title string, sections []BrainSection) error {
	var sb strings.Builder
	sb.WriteString("# ")
	sb.WriteString(strings.TrimSpace(title))
	sb.WriteString("\n")
	for _, s := range sections {
		sb.WriteString("\n## ")
		sb.WriteString(strings.TrimSpace(s.Subtopic))
		sb.WriteString("\n\n")
		sb.WriteString(strings.TrimSpace(s.Content))
		sb.WriteString("\n")
	}
	if err := os.MkdirAll(b.projectDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(b.topicPath(slug), []byte(sb.String()), 0o644)
}

func (b *BrainStore) rebuildIndex() error {
	topics, err := b.loadTopics()
	if err != nil {
		return err
	}
	indexPath := filepath.Join(b.projectDir(), "index.md")
	if len(topics) == 0 {
		_ = os.Remove(indexPath)
		return nil
	}
	return os.WriteFile(indexPath, []byte(b.renderIndex(topics)), 0o644)
}

func (b *BrainStore) renderIndex(topics []BrainTopic) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Brain Index — %s\n", b.project))
	if len(topics) == 0 {
		sb.WriteString("\n_(empty)_\n")
		return sb.String()
	}
	for _, t := range topics {
		sb.WriteString("\n## ")
		sb.WriteString(t.Topic)
		sb.WriteString("\n")
		for _, s := range t.Sections {
			sb.WriteString("- ")
			sb.WriteString(s.Subtopic)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

var brainHeadingRe = regexp.MustCompile(`(?m)^##\s+(.+?)\s*$`)

func parseTopic(slug, raw string) *BrainTopic {
	topic := &BrainTopic{Slug: slug, Topic: slug}
	lines := strings.Split(raw, "\n")
	var current *BrainSection
	var body []string
	flush := func() {
		if current != nil {
			current.Content = strings.TrimSpace(strings.Join(body, "\n"))
			topic.Sections = append(topic.Sections, *current)
		}
		current, body = nil, nil
	}
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "# "):
			topic.Topic = strings.TrimSpace(strings.TrimPrefix(line, "# "))
		case strings.HasPrefix(line, "## "):
			flush()
			current = &BrainSection{Subtopic: strings.TrimSpace(strings.TrimPrefix(line, "## "))}
		default:
			if current != nil {
				body = append(body, line)
			}
		}
	}
	flush()
	return topic
}

func brainTerms(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !('a' <= r && r <= 'z') && !('0' <= r && r <= '9')
	})
	var terms []string
	for _, f := range fields {
		if len(f) >= 3 {
			terms = append(terms, f)
		}
	}
	return terms
}

func brainSectionMatches(topic string, s BrainSection, terms []string) bool {
	hay := strings.ToLower(topic + " " + s.Subtopic + " " + s.Content)
	for _, term := range terms {
		if strings.Contains(hay, term) {
			return true
		}
	}
	return false
}

var brainSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

func brainSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = brainSlugRe.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// ----------------------------------------------------------------------------
// BrainTool exposes the BrainStore to agents as an agno toolkit.
// ----------------------------------------------------------------------------

// BrainTool is the agent-facing toolkit wrapping a BrainStore.
type BrainTool struct {
	toolkit.Toolkit
	store *BrainStore
}

// BrainRememberParams are the parameters for Brain_Remember.
type BrainRememberParams struct {
	Topic    string `json:"topic" description:"High-level topic, e.g. 'Architecture', 'Build & Test', 'Gotchas'" required:"true"`
	Subtopic string `json:"subtopic,omitempty" description:"Specific subtopic under the topic. Defaults to 'General'."`
	Content  string `json:"content" description:"The durable, reusable knowledge to store, in markdown." required:"true"`
	Replace  bool   `json:"replace,omitempty" description:"Overwrite the subtopic instead of appending. Default false (append)."`
}

// BrainRecallParams are the parameters for Brain_Recall.
type BrainRecallParams struct {
	Query string `json:"query,omitempty" description:"Keywords to search for across topics, subtopics, and content."`
	Topic string `json:"topic,omitempty" description:"Restrict the search to a single topic. Combine with an empty query to read the whole topic."`
}

// BrainForgetParams are the parameters for Brain_Forget.
type BrainForgetParams struct {
	Topic    string `json:"topic" description:"Topic to remove from." required:"true"`
	Subtopic string `json:"subtopic,omitempty" description:"Subtopic to remove. Empty removes the entire topic."`
}

// BrainNoParams is used by methods that take no arguments.
type BrainNoParams struct{}

// BrainOpResult is returned by mutating brain methods.
type BrainOpResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// NewBrainTool builds a toolkit around store.
func NewBrainTool(store *BrainStore) *BrainTool {
	bt := &BrainTool{store: store}
	bt.Toolkit = toolkit.NewToolkit()
	bt.Toolkit.Name = "Brain"
	bt.Toolkit.Description = "Persistent, project-scoped knowledge base shared across agents and runs. " +
		"Store durable, reusable facts (architecture, conventions, build/test commands, gotchas) organized by topic and subtopic, " +
		"and recall them on demand instead of re-deriving them. Do not store transient task state or secrets."

	bt.Toolkit.Register("Recall", "Search stored knowledge by keywords and/or topic. Call this before exploring to reuse past learnings.", bt, bt.Recall, BrainRecallParams{})
	bt.Toolkit.Register("ListTopics", "List all topics and their subtopic titles (no bodies). A cheap overview of what is known.", bt, bt.ListTopics, BrainNoParams{})
	bt.Toolkit.Register("Remember", "Save a durable, reusable learning under a topic/subtopic. Use only for knowledge worth keeping across runs.", bt, bt.Remember, BrainRememberParams{})
	bt.Toolkit.Register("Forget", "Remove an outdated or wrong subtopic (or a whole topic).", bt, bt.Forget, BrainForgetParams{})
	return bt
}

// Remember stores knowledge.
func (bt *BrainTool) Remember(p BrainRememberParams) (interface{}, error) {
	if err := bt.store.Remember(p.Topic, p.Subtopic, p.Content, p.Replace); err != nil {
		return BrainOpResult{Success: false, Message: err.Error()}, nil
	}
	sub := strings.TrimSpace(p.Subtopic)
	if sub == "" {
		sub = brainGeneralSubtopic
	}
	return BrainOpResult{Success: true, Message: fmt.Sprintf("saved %q › %q", strings.TrimSpace(p.Topic), sub)}, nil
}

// Forget removes knowledge.
func (bt *BrainTool) Forget(p BrainForgetParams) (interface{}, error) {
	if err := bt.store.Forget(p.Topic, p.Subtopic); err != nil {
		return BrainOpResult{Success: false, Message: err.Error()}, nil
	}
	return BrainOpResult{Success: true, Message: "forgotten"}, nil
}

// Recall returns matching knowledge as formatted markdown.
func (bt *BrainTool) Recall(p BrainRecallParams) (interface{}, error) {
	matches, err := bt.store.Recall(p.Query, p.Topic)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return "No matching knowledge in the brain yet.", nil
	}
	var sb strings.Builder
	for _, m := range matches {
		sb.WriteString(fmt.Sprintf("## %s › %s\n%s\n\n", m.Topic, m.Subtopic, m.Content))
	}
	return strings.TrimSpace(sb.String()), nil
}

// ListTopics returns the project index.
func (bt *BrainTool) ListTopics(_ BrainNoParams) (interface{}, error) {
	return bt.store.Index()
}

// appendBrainKnowledge augments the workspace knowledge surfaced to agents with
// the brain index (titles only) plus a short usage note. Full content stays out
// of context until an agent recalls it on demand, keeping the brain shareable
// without inflating every prompt.
func appendBrainKnowledge(knowledge string, store *BrainStore) string {
	index, err := store.Index()
	if err != nil {
		return knowledge
	}
	section := fmt.Sprintf(`PROJECT BRAIN (project %q)
A persistent, shared knowledge base lives at %s. Use the Brain tool:
- Brain_Recall(query) / Brain_ListTopics to reuse prior learnings before exploring.
- Brain_Remember(topic, subtopic, content) to save only durable, reusable facts.
Only titles are listed below; recall a topic to read its content.

%s`, store.Project(), store.Dir(), strings.TrimSpace(index))
	if strings.TrimSpace(knowledge) == "" {
		return section
	}
	return knowledge + "\n\n" + section
}

func brainCuratorInstructions(workspace, knowledge string) string {
	return fmt.Sprintf(`
You are the brain-update phase of a deep software engineering agent. You curate
the persistent project brain so future runs start smarter.

Workspace: %s

Workspace knowledge:
%s

Your job:
- Review the full run (explore, plan, implement, verify, report outputs).
- Save ONLY durable, reusable, project-level knowledge with Brain_Remember.
- Good candidates: architecture and module responsibilities, stable conventions,
  build/test/run commands that actually worked, environment/setup quirks, and
  non-obvious gotchas or pitfalls discovered during the run.
- Do NOT save: transient task state, one-off requests, secrets or credentials,
  speculative ideas, or anything already present (check Brain_ListTopics first).
- Organize knowledge by a clear topic and a specific subtopic.
- Prefer updating/refining an existing subtopic over creating near-duplicates;
  use replace=true when correcting outdated knowledge, append otherwise.
- Keep each entry concise and factual, with exact paths, commands, and names.
- If nothing is worth keeping, save nothing and say so.

Do not edit source code or produce the user-facing answer in this phase.

Return a short list of "Topic > Subtopic - one-line reason", or "Nothing durable to save."
`, workspace, knowledge)
}
