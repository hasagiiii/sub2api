package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotGroupStreamOnlyRoundtrip(t *testing.T) {
	groupID := int64(51)
	apiKey := &APIKey{
		ID: 83, UserID: 41, GroupID: &groupID, Key: "sk-stream-only-roundtrip", Status: StatusActive,
		User: &User{ID: 41, Status: StatusActive, UserGroupDeniedModels: []string{"blocked-model"}},
		Group: &Group{
			ID: groupID, Name: "stream-only", Platform: PlatformAnthropic, Status: StatusActive,
			Hydrated: true, StreamOnly: true, ImageCountBilling: true,
		},
	}
	svc := &APIKeyService{}

	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), apiKey)})
	require.NoError(t, err)
	var cached APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &cached))

	materialized, used, err := svc.applyAuthCacheEntry(apiKey.Key, &cached)
	require.NoError(t, err)
	require.True(t, used)
	require.True(t, materialized.Group.StreamOnly)
	require.True(t, materialized.Group.ImageCountBilling)
	require.Equal(t, []string{"blocked-model"}, materialized.DeniedModelsInGroup())

	// Neither merge parent's cache contains all merged policy fields:
	// upstream v25 lacks stream_only; local v29 lacks image_count_billing.
	for _, legacyVersion := range []int{25, 29} {
		cached.Snapshot.Version = legacyVersion
		materialized, used, err = svc.applyAuthCacheEntry(apiKey.Key, &cached)
		require.NoError(t, err)
		require.False(t, used, "reject incomplete v%d snapshot", legacyVersion)
		require.Nil(t, materialized)
	}
}
