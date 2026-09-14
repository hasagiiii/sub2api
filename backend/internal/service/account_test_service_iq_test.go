package service

import (
	"testing"
)

func TestExtractAccountTestUsageSupportsResponsesAndChatCompletionsFields(t *testing.T) {
	tests := []struct {
		name                             string
		data                             map[string]any
		wantInput, wantOutput, wantTotal int
	}{
		{
			name: "responses usage",
			data: map[string]any{"response": map[string]any{"usage": map[string]any{
				"input_tokens": float64(12), "output_tokens": float64(8), "total_tokens": float64(20),
			}}},
			wantInput: 12, wantOutput: 8, wantTotal: 20,
		},
		{
			name: "chat completions usage",
			data: map[string]any{"usage": map[string]any{
				"prompt_tokens": float64(5), "completion_tokens": float64(3), "total_tokens": float64(8),
			}},
			wantInput: 5, wantOutput: 3, wantTotal: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAccountTestUsage(tt.data)
			if got.InputTokens != tt.wantInput || got.OutputTokens != tt.wantOutput || got.TotalTokens != tt.wantTotal {
				t.Fatalf("usage = %+v, want input=%d output=%d total=%d", got, tt.wantInput, tt.wantOutput, tt.wantTotal)
			}
		})
	}
}

func TestParseTestSSEOutputWithUsageInfersTotalTokens(t *testing.T) {
	body := "data: {\"type\":\"content\",\"text\":\"<html>ok</html>\"}\n\n" +
		"data: {\"type\":\"usage\",\"input_tokens\":7,\"output_tokens\":4}\n\n"
	got := parseTestSSEOutputWithUsage(body)
	if got.ResponseText != "<html>ok</html>" || got.InputTokens != 7 || got.OutputTokens != 4 || got.TotalTokens != 11 {
		t.Fatalf("result = %+v", got)
	}
}
