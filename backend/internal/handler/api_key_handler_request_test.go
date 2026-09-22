package handler

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateAPIKeyRequestTracksExplicitNullGroup(t *testing.T) {
	var clearGroup UpdateAPIKeyRequest
	require.NoError(t, json.Unmarshal([]byte(`{"group_id":null,"user_subscription_id":77}`), &clearGroup))
	require.True(t, clearGroup.GroupIDSet)
	require.Nil(t, clearGroup.GroupID)

	var omittedGroup UpdateAPIKeyRequest
	require.NoError(t, json.Unmarshal([]byte(`{"user_subscription_id":77}`), &omittedGroup))
	require.False(t, omittedGroup.GroupIDSet)
	require.Nil(t, omittedGroup.GroupID)
}
