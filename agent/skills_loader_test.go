package agent

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/devalexandre/agno-golang/agno/skill"
)

func TestNormalizeSkillsURLConvertsGitHubRepoToZip(t *testing.T) {
	got, err := normalizeSkillsURL("https://github.com/devalexandre/my-skills")
	if err != nil {
		t.Fatalf("normalize github URL: %v", err)
	}

	want := "https://codeload.github.com/devalexandre/my-skills/zip/refs/heads/main"
	if got != want {
		t.Fatalf("unexpected URL:\nwant %s\ngot  %s", want, got)
	}
}

func TestNormalizeSkillsURLPreservesGitHubBranch(t *testing.T) {
	got, err := normalizeSkillsURL("https://github.com/devalexandre/my-skills/tree/develop")
	if err != nil {
		t.Fatalf("normalize github branch URL: %v", err)
	}

	want := "https://codeload.github.com/devalexandre/my-skills/zip/refs/heads/develop"
	if got != want {
		t.Fatalf("unexpected URL:\nwant %s\ngot  %s", want, got)
	}
}

func TestFindSkillRootPrefersInternalSkills(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "repo-main/internal/skills/go-review/SKILL.md", "---\nname: go-review\n---\n")
	writeTestFile(t, root, "repo-main/docs/README.md", "# Docs")

	got, err := findSkillRoot(root)
	if err != nil {
		t.Fatalf("find skill root: %v", err)
	}

	want := filepath.Join(root, "repo-main", "internal", "skills")
	if got != want {
		t.Fatalf("unexpected skill root:\nwant %s\ngot  %s", want, got)
	}
}

func TestUniqueSkillsSplitsCommasAndDedupes(t *testing.T) {
	got := uniqueSkills([]string{"go-expert,custom", "go-expert", " other "})
	want := []string{"go-expert", "custom", "other"}

	if len(got) != len(want) {
		t.Fatalf("unexpected length: want %v got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected skills: want %v got %v", want, got)
		}
	}
}

func TestMultiSkillsLoaderCachesLoadedSkillsAndExposesStats(t *testing.T) {
	loader := &multiSkillsLoader{
		loaders: []skill.SkillLoader{
			stubSkillsLoader{skills: []skill.Skill{{Name: "go-expert"}}},
			stubSkillsLoader{skills: []skill.Skill{{Name: "codebase-analysis"}}},
		},
	}

	first, err := loader.Load()
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("expected 2 skills on first load, got %d", len(first))
	}

	second, err := loader.Load()
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if len(second) != 2 {
		t.Fatalf("expected 2 skills on second load, got %d", len(second))
	}

	stats := loader.CacheStats()
	if stats.LoadCalls != 2 {
		t.Fatalf("expected 2 load calls, got %d", stats.LoadCalls)
	}
	if stats.CacheHits != 1 {
		t.Fatalf("expected 1 cache hit, got %d", stats.CacheHits)
	}
	if stats.LoadedSkills != 2 {
		t.Fatalf("expected 2 loaded skills in cache, got %d", stats.LoadedSkills)
	}
	if stats.LastLoadDuration <= 0 {
		t.Fatalf("expected positive last load duration, got %s", stats.LastLoadDuration)
	}
}

func TestMultiSkillsLoaderHandlesSourceErrorsGracefully(t *testing.T) {
	loader := &multiSkillsLoader{
		loaders: []skill.SkillLoader{
			stubSkillsLoader{err: errors.New("boom")},
			stubSkillsLoader{skills: []skill.Skill{{Name: "go-expert"}}},
		},
	}

	got, err := loader.Load()
	if err != nil {
		t.Fatalf("load should not fail when one source errors: %v", err)
	}
	if len(got) != 1 || got[0].Name != "go-expert" {
		t.Fatalf("unexpected skills after partial load failure: %#v", got)
	}

	stats := loader.CacheStats()
	if stats.LoadCalls != 1 {
		t.Fatalf("expected 1 load call, got %d", stats.LoadCalls)
	}
	if stats.CacheHits != 0 {
		t.Fatalf("expected 0 cache hits on first load, got %d", stats.CacheHits)
	}
	if stats.LoadedSkills != 1 {
		t.Fatalf("expected cached loaded skills=1, got %d", stats.LoadedSkills)
	}
}

type stubSkillsLoader struct {
	skills []skill.Skill
	err    error
	delay  time.Duration
}

func (s stubSkillsLoader) Load() ([]skill.Skill, error) {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	if s.err != nil {
		return nil, s.err
	}
	result := make([]skill.Skill, len(s.skills))
	copy(result, s.skills)
	return result, nil
}
