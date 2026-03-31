package service

import "strings"

// InferPlatformFromModel determines which platform a model name belongs to
// based on well-known model name prefixes.
// Returns PlatformAnthropic as default fallback for unknown models.
func InferPlatformFromModel(model string) string {
	model = strings.ToLower(model)

	switch {
	// OpenAI models
	case strings.HasPrefix(model, "gpt-"),
		strings.HasPrefix(model, "o1-"),
		strings.HasPrefix(model, "o3-"),
		strings.HasPrefix(model, "o4-"),
		strings.HasPrefix(model, "chatgpt-"),
		strings.HasPrefix(model, "codex-"):
		return PlatformOpenAI

	// Gemini models
	case strings.HasPrefix(model, "gemini-"):
		return PlatformGemini

	// Claude / Anthropic models (explicit + default)
	case strings.HasPrefix(model, "claude-"):
		return PlatformAnthropic

	default:
		// Default to anthropic (preserves existing behavior for unknown models,
		// including antigravity models which are served by anthropic groups)
		return PlatformAnthropic
	}
}
