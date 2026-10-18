package service

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamSessionIDFromFinalRequest(t *testing.T) {
	for _, tc := range []struct {
		name       string
		headers    http.Header
		body, want string
	}{
		{"outbound isolated ID", http.Header{"session_id": {"isolated-upstream"}}, `{}`, "isolated-upstream"},
		{"thread wins over shared session", http.Header{"Session_id": {"shared"}, "thread-id": {"thread-7"}}, `{}`, "thread-7"},
		{"conversation wins over session", http.Header{"Session_id": {"shared"}, "Conversation_id": {"conversation-3"}}, `{}`, "conversation-3"},
		{"Claude generated metadata", nil, `{"metadata":{"user_id":"{\"device_id\":\"device\",\"account_uuid\":\"account\",\"session_id\":\"claude-session\"}"}}`, "claude-session"},
		{"Grok", http.Header{"X-Grok-Conv-Id": {"grok-conversation"}}, `{}`, "grok-conversation"},
		{"OpenCode", http.Header{"x-opencode-session": {"generated-opencode"}}, `{}`, "generated-opencode"},
		{"Kiro", nil, `{"conversationState":{"conversationId":"kiro-1"}}`, "kiro-1"},
		{"Antigravity", nil, `{"request":{"sessionId":"ag-1"}}`, "ag-1"},
		{"no fabricated identity from cache or request ID", http.Header{"X-Request-Id": {"request-only"}}, `{"prompt_cache_key":"cache-only","messages":[]}`, ""},
		{"invalid ID", http.Header{"Session_id": {strings.Repeat("x", 256)}}, `{}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, "https://upstream.example/v1/responses", strings.NewReader(tc.body))
			require.NoError(t, err)
			for key, values := range tc.headers {
				req.Header[key] = values
			}
			req.Header["content-type"] = []string{"application/json"}
			require.Equal(t, tc.want, upstreamSessionIDFromResponse(&http.Response{Request: req}))
			remaining, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Equal(t, tc.body, string(remaining), "identity inspection must not consume the outbound body")
		})
	}
	require.Empty(t, upstreamSessionIDFromResponse(nil))
	require.Empty(t, upstreamSessionIDFromResponse(&http.Response{}))
}
