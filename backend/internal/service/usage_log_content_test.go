package service

import "testing"

func TestExtractUserInputContent(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    *string
		maxLen  int
	}{
		{
			name:   "responses string input",
			body:   `{"model":"gpt-5","input":"hello"}`,
			want:   usageContentStringPtr("hello"),
			maxLen: 500,
		},
		{
			name:   "responses input text array",
			body:   `{"model":"gpt-5.1","input":[{"type":"input_text","text":"hello"},{"type":"input_text","text":"world"}]}`,
			want:   usageContentStringPtr("world"),
			maxLen: 500,
		},
		{
			name:   "responses user message content array",
			body:   `{"model":"gpt-5.1","input":[{"type":"message","role":"assistant","content":[{"type":"input_text","text":"ignore"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"},{"type":"input_text","text":"world"}]}]}`,
			want:   usageContentStringPtr("hello\nworld"),
			maxLen: 500,
		},
		{
			name:   "responses prefers latest user message",
			body:   `{"model":"gpt-5.1","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"first"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ignore"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"latest"}]}]}`,
			want:   usageContentStringPtr("latest"),
			maxLen: 500,
		},
		{
			name:   "anthropic messages still work",
			body:   `{"messages":[{"role":"assistant","content":"ignore"},{"role":"user","content":[{"type":"text","text":"hello anthropic"}]}]}`,
			want:   usageContentStringPtr("hello anthropic"),
			maxLen: 500,
		},
		{
			name:   "gemini contents still work",
			body:   `{"contents":[{"role":"model","parts":[{"text":"ignore"}]},{"role":"user","parts":[{"text":"hello gemini"}]}]}`,
			want:   usageContentStringPtr("hello gemini"),
			maxLen: 500,
		},
		{
			name:   "truncates by rune count",
			body:   `{"input":"你好世界"}`,
			want:   usageContentStringPtr("你好"),
			maxLen: 2,
		},
		{
			name:   "strips system tags from messages content",
			body:   `{"messages":[{"role":"user","content":"<local-command-caveat>Caveat: ignore</local-command-caveat>\n<command-name>/model</command-name>\n<local-command-stdout>Set model</local-command-stdout>\n你好"}]}`,
			want:   usageContentStringPtr("你好"),
			maxLen: 500,
		},
		{
			name:   "strips system-reminder tags",
			body:   `{"messages":[{"role":"user","content":"hello<system-reminder>some reminder</system-reminder>"}]}`,
			want:   usageContentStringPtr("hello"),
			maxLen: 500,
		},
		{
			name:   "returns nil when only system tags",
			body:   `{"messages":[{"role":"user","content":"<command-name>/model</command-name>\n<local-command-stdout>done</local-command-stdout>"}]}`,
			want:   nil,
			maxLen: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractUserInputContent([]byte(tt.body), tt.maxLen)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("expected nil, got %q", *got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil result")
			}
			if *got != *tt.want {
				t.Fatalf("expected %q, got %q", *tt.want, *got)
			}
		})
	}
}

func usageContentStringPtr(s string) *string {
	return &s
}
