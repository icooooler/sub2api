package service

import "strings"

// InferPlatformFromModel determines which platform a model name belongs to
// based on well-known model name prefixes.
// Returns PlatformOpenAI as default fallback for unknown models, since the
// OpenAI-compatible format is the most universal third-party API format.
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
		// Default to openai — the OpenAI-compatible format is the most universal
		// and covers the vast majority of third-party models (GLM, Qwen, etc.).
		// All Anthropic models are already covered by the explicit "claude-" prefix.
		// Antigravity models have their own dedicated routes with ForcePlatform.
		return PlatformOpenAI
	}
}
