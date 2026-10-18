//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

// Like net/http, retain the final outbound request on the response.
type upstreamSessionHTTPRecorder struct{ httpUpstreamRecorder }

func (u *upstreamSessionHTTPRecorder) Do(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	resp, err := u.httpUpstreamRecorder.Do(req, proxyURL, accountID, concurrency)
	if resp != nil {
		resp.Request = req
	}
	return resp, err
}
func (u *upstreamSessionHTTPRecorder) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

func TestUpstreamSessionForward_UsesActualOutboundBody(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "buffered", true: "stream"}[stream], func(t *testing.T) {
			response := nativeAnthropicBufferedResponse()
			streamJSON := "false"
			if stream {
				response = nativeAnthropicStreamResponse()
				streamJSON = "true"
			}
			upstream := &upstreamSessionHTTPRecorder{httpUpstreamRecorder: httpUpstreamRecorder{resp: response}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			body := []byte(`{"model":"k3","max_tokens":32,"stream":` + streamJSON + `,"metadata":{"user_id":"{\"device_id\":\"device\",\"session_id\":\"outbound-conversation\"}"},"messages":[{"role":"user","content":"hi"}]}`)
			c := adaptiveProtocolTestContext("/v1/messages", body)
			result, err := svc.ForwardAsAnthropic(context.Background(), c, nativeAnthropicTestAccount(), body, "", "")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Empty(t, ExtractClientSessionID(c), "no client session header was supplied")
			require.Equal(t, "outbound-conversation", result.UpstreamSessionID)
		})
	}
}
