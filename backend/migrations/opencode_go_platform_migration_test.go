package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenCodeGoPlatformMigration(t *testing.T) {
	content, err := FS.ReadFile("238_opencode_go_platform.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "user_platform_quotas_platform_check")
	require.Contains(t, sql, "composite_model_routes_target_platform_check")
	require.Contains(t, sql, "channel_monitors_provider_check")
	require.Contains(t, sql, "channel_monitor_request_templates_provider_check")
	require.Contains(t, sql, "'opencode_go'")
	require.Contains(t, sql, "'minimax'")
	require.Contains(t, sql, "position('opencode_go' IN monitor_constraint_def) = 0")
	require.Contains(t, sql, "position('opencode_go' IN template_constraint_def) = 0")
	require.Contains(t, sql,
		"CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'kiro', 'grok', 'fal', 'leonardo', 'atlascloud', 'apiz', 'higgsfield', 'kimi', 'zhipu', 'deepseek', 'minimax', 'bytedance', 'opencode_go'))")
	require.Contains(t, sql,
		"CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'kiro', 'grok', 'fal', 'leonardo', 'atlascloud', 'apiz', 'higgsfield', 'kimi', 'zhipu', 'deepseek', 'minimax', 'bytedance', 'opencode_go'))")
	require.Contains(t, sql,
		"CHECK (provider IN ('openai', 'anthropic', 'gemini', 'antigravity', 'kiro', 'grok', 'fal', 'leonardo', 'atlascloud', 'apiz', 'higgsfield', 'kimi', 'zhipu', 'deepseek', 'minimax', 'bytedance', 'opencode_go'))")
}

func TestPlatformCheckConstraintRepairMigration(t *testing.T) {
	content, err := FS.ReadFile("247_repair_platform_check_constraints.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	for _, constraint := range []string{
		"user_platform_quotas_platform_check",
		"composite_model_routes_target_platform_check",
		"channel_monitors_provider_check",
		"channel_monitor_request_templates_provider_check",
	} {
		require.Contains(t, sql, constraint)
	}
	for _, platform := range []string{"kiro", "fal", "leonardo", "atlascloud", "apiz", "higgsfield", "bytedance", "opencode_go"} {
		require.Contains(t, sql, "'"+platform+"'")
	}
}
