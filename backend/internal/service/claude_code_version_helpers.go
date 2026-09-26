package service

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"strings"
)

const SettingKeyClaudeCodeClientVersionSynced = "claude_code_client_version_synced"
const SettingKeyClaudeCodeClientVersion = "claude_code_client_version"
const SettingKeyClaudeCodeVersionAutoSyncEnabled = "claude_code_version_auto_sync_enabled"

func NormalizeClaudeCodeClientVersion(version string) string {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if v == "" || !claude.IsSupportedCLIVersion(v) {
		return ""
	}
	return v
}
