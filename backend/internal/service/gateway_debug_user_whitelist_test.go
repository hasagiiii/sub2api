package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type gatewayDebugWhitelistUserRepo struct {
	UserRepository
	idsByEmail map[string]int64
}

func (r *gatewayDebugWhitelistUserRepo) GetByEmail(_ context.Context, email string) (*User, error) {
	if id, ok := r.idsByEmail[strings.ToLower(strings.TrimSpace(email))]; ok {
		return &User{ID: id, Email: email}, nil
	}
	return nil, ErrUserNotFound
}

type gatewayDebugBroadcasterStub struct{ published int }

func (b *gatewayDebugBroadcasterStub) Publish(context.Context) error     { b.published++; return nil }
func (b *gatewayDebugBroadcasterStub) Subscribe(context.Context, func()) {}

func resetGatewayDebugWhitelistForTest(t *testing.T) {
	t.Helper()
	SetGatewayDebugLogUserWhitelist(nil)
	t.Cleanup(func() { SetGatewayDebugLogUserWhitelist(nil) })
}

func userCtx(id int64) context.Context {
	return context.WithValue(context.Background(), ctxkey.UserID, id)
}

func TestGatewayDebugUserAllowed(t *testing.T) {
	resetGatewayDebugWhitelistForTest(t)

	require.True(t, gatewayDebugUserAllowed(context.TODO()), "no whitelist: allow everything")
	require.True(t, gatewayDebugUserAllowed(userCtx(7)))

	SetGatewayDebugLogUserWhitelist([]int64{7, 9, 0, -1})
	require.Equal(t, []int64{7, 9}, GatewayDebugLogUserWhitelist())
	require.True(t, gatewayDebugUserAllowed(userCtx(7)))
	require.True(t, gatewayDebugUserAllowed(userCtx(9)))
	require.False(t, gatewayDebugUserAllowed(userCtx(8)))
	require.False(t, gatewayDebugUserAllowed(context.Background()), "unknown user is excluded once whitelist is set")
	var nilCtx context.Context
	require.False(t, gatewayDebugUserAllowed(nilCtx), "missing context is excluded once whitelist is set")

	SetGatewayDebugLogUserWhitelist([]int64{})
	require.Nil(t, GatewayDebugLogUserWhitelist())
	require.True(t, gatewayDebugUserAllowed(userCtx(8)))
}

func TestGatewayDebugSnapshotAndResponseRespectWhitelist(t *testing.T) {
	resetGatewayDebugWhitelistForTest(t)
	path := filepath.Join(t.TempDir(), "gateway_debug.log")
	debugGatewayBodyPath.Store(&path)
	require.NoError(t, SetGatewayDebugLogEnabled(true))
	SetGatewayDebugRespEnabled(true)
	t.Cleanup(func() {
		SetGatewayDebugRespEnabled(false)
		_ = SetGatewayDebugLogEnabled(false)
	})

	SetGatewayDebugLogUserWhitelist([]int64{42})
	debugLogGatewaySnapshot(userCtx(42), "CLIENT_ORIGINAL", http.Header{}, []byte(`{"who":"allowed"}`), nil)
	debugLogGatewaySnapshot(userCtx(43), "CLIENT_ORIGINAL", http.Header{}, []byte(`{"who":"blocked"}`), nil)
	debugLogGatewaySnapshot(context.Background(), "CLIENT_ORIGINAL", http.Header{}, []byte(`{"who":"anonymous"}`), nil)

	for _, item := range []struct {
		userID int64
		body   string
	}{{42, `{"resp":"allowed"}`}, {43, `{"resp":"blocked"}`}} {
		req := httptest.NewRequest(http.MethodPost, "https://upstream.example/v1", nil).WithContext(userCtx(item.userID))
		resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(item.body))}
		MaybeWrapGatewayDebugResponse(req, resp)
		require.NoError(t, resp.Body.Close())
	}

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(content)
	require.Contains(t, text, `"who": "allowed"`)
	require.NotContains(t, text, "blocked")
	require.NotContains(t, text, "anonymous")
	require.Contains(t, text, "UPSTREAM_RESPONSE")

	SetGatewayDebugLogUserWhitelist(nil)
	debugLogGatewaySnapshot(userCtx(43), "CLIENT_ORIGINAL", http.Header{}, []byte(`{"who":"everyone"}`), nil)
	content, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(content), `"who": "everyone"`)
}

func TestUpdateOpsAdvancedSettings_GatewayDebugLogUserWhitelist(t *testing.T) {
	resetGatewayDebugWhitelistForTest(t)
	repo := newRuntimeSettingRepoStub()
	broadcaster := &gatewayDebugBroadcasterStub{}
	svc := &OpsService{
		settingRepo:             repo,
		userRepo:                &gatewayDebugWhitelistUserRepo{idsByEmail: map[string]int64{"alice@example.com": 11, "bob@example.com": 12}},
		gatewayDebugBroadcaster: broadcaster,
	}

	cfg := defaultOpsAdvancedSettings()
	cfg.GatewayDebugLogUserEmails = []string{" Alice@Example.com ", "bob@example.com", "alice@example.com", ""}
	cfg.GatewayDebugLogUserIDs = []int64{999}
	updated, err := svc.UpdateOpsAdvancedSettings(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, []string{"alice@example.com", "bob@example.com"}, updated.GatewayDebugLogUserEmails)
	require.Equal(t, []int64{11, 12}, updated.GatewayDebugLogUserIDs, "client supplied ids are ignored")
	require.Equal(t, []int64{11, 12}, GatewayDebugLogUserWhitelist(), "applied on local node")
	require.Equal(t, 1, broadcaster.published, "other nodes are notified")

	var persisted OpsAdvancedSettings
	require.NoError(t, json.Unmarshal([]byte(repo.values[SettingKeyOpsAdvancedSettings]), &persisted))
	require.Equal(t, []int64{11, 12}, persisted.GatewayDebugLogUserIDs)

	// An older client that omits the field keeps the stored whitelist.
	legacy := defaultOpsAdvancedSettings()
	legacy.GatewayDebugLogUserEmails = nil
	updated, err = svc.UpdateOpsAdvancedSettings(context.Background(), legacy)
	require.NoError(t, err)
	require.Equal(t, []string{"alice@example.com", "bob@example.com"}, updated.GatewayDebugLogUserEmails)
	require.Equal(t, []int64{11, 12}, updated.GatewayDebugLogUserIDs)

	// An explicit empty list clears the whitelist.
	cleared := defaultOpsAdvancedSettings()
	cleared.GatewayDebugLogUserEmails = []string{}
	updated, err = svc.UpdateOpsAdvancedSettings(context.Background(), cleared)
	require.NoError(t, err)
	require.Empty(t, updated.GatewayDebugLogUserEmails)
	require.Empty(t, updated.GatewayDebugLogUserIDs)
	require.Nil(t, GatewayDebugLogUserWhitelist())
}

func TestUpdateOpsAdvancedSettings_GatewayDebugLogUserWhitelistRejectsInvalid(t *testing.T) {
	resetGatewayDebugWhitelistForTest(t)
	repo := newRuntimeSettingRepoStub()
	svc := &OpsService{
		settingRepo: repo,
		userRepo:    &gatewayDebugWhitelistUserRepo{idsByEmail: map[string]int64{"alice@example.com": 11}},
	}

	for name, emails := range map[string][]string{
		"bad format":   {"not-an-email"},
		"display name": {"Alice <alice@example.com>"},
		"unknown user": {"alice@example.com", "ghost@example.com"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := defaultOpsAdvancedSettings()
			cfg.GatewayDebugLogUserEmails = emails
			_, err := svc.UpdateOpsAdvancedSettings(context.Background(), cfg)
			require.Error(t, err)
			require.Empty(t, repo.values[SettingKeyOpsAdvancedSettings], "invalid whitelist must not be persisted")
			require.Nil(t, GatewayDebugLogUserWhitelist())
		})
	}

	tooMany := make([]string, opsGatewayDebugLogUserWhitelistMax+1)
	for i := range tooMany {
		tooMany[i] = "user" + strings.Repeat("x", i) + "@example.com"
	}
	cfg := defaultOpsAdvancedSettings()
	cfg.GatewayDebugLogUserEmails = tooMany
	_, err := svc.UpdateOpsAdvancedSettings(context.Background(), cfg)
	require.ErrorContains(t, err, "at most")
}

func TestGatewayDebugLogUserWhitelistSyncsFromDB(t *testing.T) {
	resetGatewayDebugWhitelistForTest(t)
	repo := newRuntimeSettingRepoStub()
	stored := defaultOpsAdvancedSettings()
	stored.GatewayDebugLogUserEmails = []string{"alice@example.com"}
	stored.GatewayDebugLogUserIDs = []int64{11}
	raw, err := json.Marshal(stored)
	require.NoError(t, err)
	repo.values[SettingKeyOpsAdvancedSettings] = string(raw)

	// Broadcast path: another node saved the settings.
	svc := &OpsService{settingRepo: repo}
	svc.applyGatewayDebugLogFromDB(context.Background())
	require.Equal(t, []int64{11}, GatewayDebugLogUserWhitelist())

	// Polling fallback: the 30s refresh converges even if a broadcast is lost.
	SetGatewayDebugLogUserWhitelist(nil)
	require.NoError(t, svc.RefreshRuntimeSettings(context.Background()))
	require.Equal(t, []int64{11}, GatewayDebugLogUserWhitelist())

	cfg, err := svc.GetOpsAdvancedSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"alice@example.com"}, cfg.GatewayDebugLogUserEmails)
}
