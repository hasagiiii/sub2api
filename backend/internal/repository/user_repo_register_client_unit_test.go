package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserRepositoryCreatePersistsRegisterClientInfoFromContext(t *testing.T) {
	repo, _ := newUserEntRepo(t)
	longUA := strings.Repeat("u", 2000)
	ctx := service.WithRegisterClientInfo(context.Background(), " 203.0.113.7 ", longUA)

	user := &service.User{Email: "signup@example.com", PasswordHash: "hash", Role: service.RoleUser, Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, user))
	require.Equal(t, "203.0.113.7", user.RegisterIP)

	got, err := repo.GetByID(context.Background(), user.ID)
	require.NoError(t, err)
	require.Equal(t, "203.0.113.7", got.RegisterIP)
	require.Len(t, got.RegisterUserAgent, 1024, "user agent is bounded")
}

func TestUserRepositoryCreateWithoutRegisterContextLeavesFieldsEmpty(t *testing.T) {
	repo, _ := newUserEntRepo(t)
	user := &service.User{Email: "admin-created@example.com", PasswordHash: "hash", Role: service.RoleUser, Status: service.StatusActive}
	require.NoError(t, repo.Create(context.Background(), user))

	got, err := repo.GetByID(context.Background(), user.ID)
	require.NoError(t, err)
	require.Empty(t, got.RegisterIP)
	require.Empty(t, got.RegisterUserAgent)
}
