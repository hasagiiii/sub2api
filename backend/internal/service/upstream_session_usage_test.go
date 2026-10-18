//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRecordUsage_PersistsIndependentUpstreamSession(t *testing.T) {
	t.Run("Claude", func(t *testing.T) {
		repo := &openAIRecordUsageLogRepoStub{inserted: true}
		svc := newGatewayRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
		err := svc.RecordUsage(context.Background(), &RecordUsageInput{
			Result: &ForwardResult{RequestID: "req-claude", UpstreamSessionID: "outbound-claude", Model: "claude-sonnet-4", Usage: ClaudeUsage{InputTokens: 10, OutputTokens: 5}, Duration: time.Second},
			APIKey: &APIKey{ID: 1}, User: &User{ID: 2}, Account: &Account{ID: 3},
		})
		require.NoError(t, err)
		require.NotNil(t, repo.lastLog)
		require.Nil(t, repo.lastLog.SessionID)
		require.Equal(t, "outbound-claude", *repo.lastLog.UpstreamSessionID)
	})
	t.Run("OpenAI", func(t *testing.T) {
		repo := &openAIRecordUsageLogRepoStub{inserted: true}
		svc := newOpenAIRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
		err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
			Result:    &OpenAIForwardResult{RequestID: "req-openai", UpstreamSessionID: "isolated-thread", Model: "gpt-5.1", Usage: OpenAIUsage{InputTokens: 10, OutputTokens: 5}, Duration: time.Second},
			SessionID: "client-session", APIKey: &APIKey{ID: 1}, User: &User{ID: 2}, Account: &Account{ID: 3},
		})
		require.NoError(t, err)
		require.NotNil(t, repo.lastLog)
		require.Equal(t, "client-session", *repo.lastLog.SessionID)
		require.Equal(t, "isolated-thread", *repo.lastLog.UpstreamSessionID)
	})
}
