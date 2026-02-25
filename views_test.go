package main

import "testing"

func TestFormatMessagesInputTokenCount(t *testing.T) {
	tests := []struct {
		name string
		req  *LLMRequest
		want string
	}{
		{
			name: "nil request",
			req:  nil,
			want: "-",
		},
		{
			name: "actual input tokens",
			req: &LLMRequest{
				InputTokens:          12345,
				EstimatedInputTokens: 6789,
			},
			want: "12,345",
		},
		{
			name: "estimated input tokens",
			req: &LLMRequest{
				EstimatedInputTokens: 1234,
			},
			want: "~1,234 (estimated)",
		},
		{
			name: "no input tokens",
			req:  &LLMRequest{},
			want: "-",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatMessagesInputTokenCount(tc.req)
			if got != tc.want {
				t.Fatalf("formatMessagesInputTokenCount() = %q, want %q", got, tc.want)
			}
		})
	}
}
