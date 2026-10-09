package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const typesafePlatformAllowlist = "'anthropic', 'openai', 'gemini', 'antigravity', 'kiro', 'grok', 'fal', 'leonardo', 'atlascloud', 'apiz', 'higgsfield', 'kimi', 'zhipu', 'deepseek', 'minimax', 'bytedance', 'opencode_go', 'typesafe'"

func TestTypeSafePlatformMigration(t *testing.T) {
	for _, name := range []string{"241_add_typesafe_platform.sql", "278_typesafe_platform_constraints.sql"} {
		t.Run(name, func(t *testing.T) {
			content, err := FS.ReadFile(name)
			require.NoError(t, err)

			sql := strings.Join(strings.Fields(string(content)), " ")
			require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check")
			require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check")
			require.Contains(t, sql, "CHECK (platform IN ("+typesafePlatformAllowlist+"))")
			require.Contains(t, sql, "CHECK (target_platform IN ("+typesafePlatformAllowlist+"))")
		})
	}
}
