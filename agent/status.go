package agent

import (
	"time"
)

type PhaseDuration struct {
	Name     string
	Duration time.Duration
}

type RuntimeStatus struct {
	Workspace string
	ModelID   string

	StartupDuration        time.Duration
	KnowledgeBuildDuration time.Duration
	KnowledgeBuiltAt       time.Time

	IndexDuration    time.Duration
	IndexFromCache   bool
	IndexFiles       int
	IndexFingerprint string

	SkillsLoadCalls        int
	SkillsCacheHits        int
	SkillsLoaded           int
	SkillsLastLoadDuration time.Duration

	LocalSignalsDuration    time.Duration
	LocalSignalsCacheHits   int
	LocalSignalsCacheMisses int

	LastChatDuration time.Duration
	ChatRuns         int

	LastDeepDuration time.Duration
	DeepRuns         int
	LastDeepPhases   []PhaseDuration
}

func (c *DeepAgent) Status() RuntimeStatus {
	c.statusMu.RLock()
	defer c.statusMu.RUnlock()

	status := c.status
	status.LastDeepPhases = append([]PhaseDuration(nil), c.status.LastDeepPhases...)
	return status
}

func (c *DeepAgent) updateStatus(update func(*RuntimeStatus)) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	update(&c.status)
}

type localSignalsDetails struct {
	Duration    time.Duration
	CacheHits   int
	CacheMisses int
}

type SkillsLoaderCacheStats struct {
	LoadCalls        int
	CacheHits        int
	LoadedSkills     int
	LastLoadDuration time.Duration
}

type skillsLoaderStatsProvider interface {
	CacheStats() SkillsLoaderCacheStats
}
