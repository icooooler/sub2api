package service

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/tidwall/gjson"
)

// systemTagRe matches known XML system tags injected by AI coding clients (e.g. Claude Code CLI).
var systemTagRe = buildSystemTagRegexp(
	"local-command-caveat",
	"command-name",
	"command-message",
	"command-args",
	"local-command-stdout",
	"system-reminder",
	"user-prompt-submit-hook",
	"task-notification",
)

func buildSystemTagRegexp(tags ...string) *regexp.Regexp {
	parts := make([]string, len(tags))
	for i, tag := range tags {
		parts[i] = `<` + regexp.QuoteMeta(tag) + `>.*?</` + regexp.QuoteMeta(tag) + `>`
	}
	return regexp.MustCompile(`(?s)(?:` + strings.Join(parts, "|") + `)`)
}

func ExtractUserInputContent(body []byte, maxLen int) *string {
	if len(body) == 0 || maxLen <= 0 || !gjson.ValidBytes(body) {
		return nil
	}

	if content := extractLastResponsesInputContent(body); content != "" {
		return truncateUsageContent(stripXMLSystemTags(content), maxLen)
	}
	if content := extractLastUserMessageContent(body); content != "" {
		return truncateUsageContent(stripXMLSystemTags(content), maxLen)
	}
	if content := extractLastGeminiUserContent(body); content != "" {
		return truncateUsageContent(stripXMLSystemTags(content), maxLen)
	}
	return nil
}

func extractLastResponsesInputContent(body []byte) string {
	input := gjson.GetBytes(body, "input")
	if !input.Exists() {
		return ""
	}
	if input.Type == gjson.String {
		return strings.TrimSpace(input.String())
	}
	if !input.IsArray() {
		return ""
	}

	items := input.Array()
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		itemType := strings.TrimSpace(item.Get("type").String())
		switch itemType {
		case "input_text", "output_text", "text":
			if text := strings.TrimSpace(item.Get("text").String()); text != "" {
				return text
			}
		case "message":
			if strings.TrimSpace(item.Get("role").String()) != "user" {
				continue
			}
			if content := extractMessageContentText(item.Get("content")); content != "" {
				return content
			}
		}
	}

	texts := make([]string, 0)
	for _, item := range items {
		itemType := strings.TrimSpace(item.Get("type").String())
		if itemType != "input_text" && itemType != "output_text" && itemType != "text" {
			continue
		}
		text := strings.TrimSpace(item.Get("text").String())
		if text != "" {
			texts = append(texts, text)
		}
	}
	return strings.TrimSpace(strings.Join(texts, "\n"))
}

func extractLastUserMessageContent(body []byte) string {
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return ""
	}

	items := messages.Array()
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if strings.TrimSpace(item.Get("role").String()) != "user" {
			continue
		}
		if content := extractMessageContentText(item.Get("content")); content != "" {
			return content
		}
	}
	return ""
}

func extractMessageContentText(content gjson.Result) string {
	if !content.Exists() {
		return ""
	}
	if content.Type == gjson.String {
		return strings.TrimSpace(content.String())
	}
	if !content.IsArray() {
		return ""
	}

	parts := make([]string, 0)
	for _, part := range content.Array() {
		partType := strings.TrimSpace(part.Get("type").String())
		if partType != "text" && partType != "input_text" && partType != "output_text" {
			continue
		}
		text := strings.TrimSpace(part.Get("text").String())
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func extractLastGeminiUserContent(body []byte) string {
	contents := gjson.GetBytes(body, "contents")
	if !contents.Exists() || !contents.IsArray() {
		return ""
	}

	items := contents.Array()
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if strings.TrimSpace(item.Get("role").String()) != "user" {
			continue
		}
		parts := item.Get("parts")
		if !parts.Exists() || !parts.IsArray() {
			continue
		}
		texts := make([]string, 0)
		for _, part := range parts.Array() {
			text := strings.TrimSpace(part.Get("text").String())
			if text != "" {
				texts = append(texts, text)
			}
		}
		if len(texts) > 0 {
			return strings.TrimSpace(strings.Join(texts, "\n"))
		}
	}
	return ""
}

// stripXMLSystemTags removes known XML system tag blocks injected by AI coding
// clients so that only the actual user input text remains.
func stripXMLSystemTags(s string) string {
	return strings.TrimSpace(systemTagRe.ReplaceAllString(s, ""))
}

func truncateUsageContent(content string, maxLen int) *string {
	content = strings.TrimSpace(content)
	if content == "" || maxLen <= 0 {
		return nil
	}
	if utf8.RuneCountInString(content) <= maxLen {
		return &content
	}

	runes := []rune(content)
	truncated := strings.TrimSpace(string(runes[:maxLen]))
	if truncated == "" {
		return nil
	}
	return &truncated
}
