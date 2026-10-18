package service

import (
	"io"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

// upstreamSessionID reads the final outbound identity, after account isolation,
// protocol conversion and header overrides. Request IDs and cache keys alone are
// not conversation identities and are deliberately excluded.
func upstreamSessionID(headers http.Header, body []byte) string {
	for _, name := range []string{"thread_id", "thread-id", "conversation_id", "X-Conversation-ID", grokConversationIDHeader, "X-Claude-Code-Session-Id", "x-opencode-session", "session_id", "session-id", "X-Session-ID", "Anthropic-Session-Id"} {
		// Some upstream builders preserve raw header casing instead of canonicalizing.
		for key, values := range headers {
			if strings.EqualFold(key, name) {
				for _, value := range values {
					if id := sanitizeSessionID(value); id != "" {
						return id
					}
				}
			}
		}
	}
	for _, path := range []string{"client_metadata.thread_id", "client_metadata.session_id", "conversationState.conversationId", "request.sessionId", "conversation_id", "conversationId", "session_id", "sessionId"} {
		value := gjson.GetBytes(body, path)
		if value.Type == gjson.String {
			if id := sanitizeSessionID(value.String()); id != "" {
				return id
			}
		}
	}
	if userID := ParseMetadataUserID(gjson.GetBytes(body, "metadata.user_id").String()); userID != nil {
		return sanitizeSessionID(userID.SessionID)
	}
	return ""
}

// Read only the replayable outbound body: consuming req.Body here would disturb
// streaming/retries. Never fall back to the incoming client's identity.
func upstreamSessionIDFromResponse(resp *http.Response) string {
	if resp == nil || resp.Request == nil {
		return ""
	}
	req := resp.Request
	if id := upstreamSessionID(req.Header, nil); id != "" {
		return id
	}
	if req.GetBody == nil || !strings.Contains(strings.ToLower(getHeaderRaw(req.Header, "content-type")), "json") {
		return ""
	}
	body, err := req.GetBody()
	if err != nil {
		return ""
	}
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(body)
	if err != nil {
		return ""
	}
	return upstreamSessionID(req.Header, data)
}
