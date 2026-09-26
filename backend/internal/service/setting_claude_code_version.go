package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

func (s *SettingService) GetClaudeCodeClientVersion(ctx context.Context) string {
	if s == nil || s.settingRepo == nil {
		return claude.CLIVersion()
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyClaudeCodeClientVersion, SettingKeyClaudeCodeClientVersionSynced})
	if err != nil {
		return claude.CLIVersion()
	}
	if value := NormalizeClaudeCodeClientVersion(values[SettingKeyClaudeCodeClientVersion]); value != "" {
		return value
	}
	if value := NormalizeClaudeCodeClientVersion(values[SettingKeyClaudeCodeClientVersionSynced]); value != "" {
		return value
	}
	return claude.CLIVersion()
}
