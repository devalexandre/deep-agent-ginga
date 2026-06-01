package agent

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	maxPhaseContextChars           = 18000
	compressedPhaseContextChars    = 12000
	maxCompactedPhaseImportantRows = 80
	maxCompactedPhaseTailChars     = 7000
)

var phaseImportantPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(error|failed|failure|panic|warning|risk|blocked|unverified)\b`),
	regexp.MustCompile(`(?i)\b(test|go test|go vet|gofmt|build|verify|verification|passed|skipped)\b`),
	regexp.MustCompile(`(?i)\b(changed|created|updated|deleted|renamed|moved|implemented|fixed)\b`),
	regexp.MustCompile(`(?i)\b(decision|assumption|architecture|config|skill|workspace|session)\b`),
	regexp.MustCompile(`(?i)\b(key content|deliverable|recommendation|proposal|summary)\b`),
	regexp.MustCompile(`(?:^|\s)(?:[\w.-]+/)+[\w.-]+\.(?:go|md|json|yaml|yml|txt|sh|py|js|ts|tsx|jsx|toml)(?:\s|$)`),
}

func (c *DeepAgent) compactPhaseContext(value string) string {
	value = strings.TrimSpace(value)
	limit := maxPhaseContextChars
	if c.config.CompressResults {
		limit = compressedPhaseContextChars
	}
	if len(value) <= limit {
		return value
	}

	important := compactImportantLines(value)
	tail := tailString(value, maxCompactedPhaseTailChars)

	var b strings.Builder
	fmt.Fprintf(&b, "COMPACTED PREVIOUS PHASE OUTPUTS\nOriginal context was %d characters and was compacted to save model tokens.\n", len(value))
	if len(important) > 0 {
		b.WriteString("\nIMPORTANT LINES\n")
		for _, line := range important {
			fmt.Fprintf(&b, "- %s\n", line)
		}
	}
	if tail != "" {
		b.WriteString("\nRECENT CONTEXT TAIL\n")
		b.WriteString(tail)
	}

	result := strings.TrimSpace(b.String())
	if len(result) > limit {
		return trimString(result, limit)
	}
	return result
}

func compactImportantLines(value string) []string {
	seen := map[string]struct{}{}
	var result []string
	for _, raw := range strings.Split(value, "\n") {
		line := compactWhitespace(raw)
		if line == "" {
			continue
		}
		for _, pattern := range phaseImportantPatterns {
			if !pattern.MatchString(line) {
				continue
			}
			line = trimString(line, 260)
			if _, ok := seen[line]; ok {
				break
			}
			seen[line] = struct{}{}
			result = append(result, line)
			break
		}
		if len(result) >= maxCompactedPhaseImportantRows {
			break
		}
	}
	return result
}

func compactWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func tailString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return "..." + string(runes[len(runes)-limit:])
}

func trimString(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}
