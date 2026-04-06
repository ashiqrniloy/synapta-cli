package components

import (
	"fmt"
	"strings"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

func inferThinkingLevel(modelID, modelName string) string {
	id := strings.ToLower(modelID + " " + modelName)
	if strings.Contains(id, "thinking") || strings.Contains(id, "reason") || strings.Contains(id, "r1") || strings.Contains(id, "o1") || strings.Contains(id, "o3") || strings.Contains(id, "gpt-5") || strings.Contains(id, "codex") {
		return "medium"
	}
	return "standard"
}

func thinkingLevelsForModel(providerID, modelID, modelName string) []string {
	level := inferThinkingLevel(modelID, modelName)
	if level == "standard" {
		return []string{"standard"}
	}
	if modelID == "" {
		return []string{"standard"}
	}
	if providerID == "" {
		return []string{"standard"}
	}
	return []string{"minimal", "low", "medium", "high"}
}

func nextThinkingLevel(current string, levels []string) (string, bool) {
	if len(levels) <= 1 {
		return "", false
	}
	idx := 0
	for i, l := range levels {
		if l == current {
			idx = i
			break
		}
	}
	return levels[(idx+1)%len(levels)], true
}

func resolveModelContextWindow(providerID, modelID string) int {
	if providerID == "" || modelID == "" {
		return 128000
	}
	for _, model := range llm.DefaultModels() {
		if model.Provider == providerID && model.ID == modelID {
			if model.ContextWindow > 0 {
				return model.ContextWindow
			}
			break
		}
	}
	return 128000
}

func formatTokenCount(v int) string {
	if v >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(v)/1_000_000)
	}
	if v >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(v)/1_000)
	}
	return fmt.Sprintf("%d", v)
}

func thinkingInstruction(level string) string {
	lv := strings.TrimSpace(strings.ToLower(level))
	switch lv {
	case "", "standard":
		return ""
	case "minimal", "low", "medium", "high":
		return fmt.Sprintf("Reasoning effort preference: %s. Use this as guidance for depth of internal reasoning and response detail.", lv)
	default:
		return ""
	}
}

func parseModelSelectionKey(key string) (providerID string, modelID string, ok bool) {
	parts := strings.SplitN(key, "::", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func modelProviderRank(provider string) int {
	switch provider {
	case "github-copilot":
		return 0
	case "kilo":
		return 1
	default:
		return 2
	}
}

func copilotModelTier(providerID, modelID string) string {
	if providerID != "github-copilot" {
		return ""
	}
	id := strings.ToLower(strings.TrimSpace(modelID))
	if id == "gpt-4o" {
		return "free"
	}
	if strings.Contains(id, "codex") || strings.Contains(id, "gpt-5") || strings.Contains(id, "claude-") || strings.Contains(id, "gemini-") || strings.Contains(id, "grok") {
		return "premium"
	}
	return "premium"
}
