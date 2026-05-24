//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type captchaVerifierSpy struct {
	called       int
	lastProvider string
	result       *CaptchaVerifyResponse
	err          error
}

func (s *captchaVerifierSpy) VerifyToken(_ context.Context, provider, _ string, _, _ string) (*CaptchaVerifyResponse, error) {
	s.called++
	s.lastProvider = provider
	if s.err != nil {
		return nil, s.err
	}
	if s.result != nil {
		return s.result, nil
	}
	return &CaptchaVerifyResponse{Success: true}, nil
}

func newCaptchaServiceForTest(settings map[string]string, verifier CaptchaVerifier) *CaptchaService {
	return NewCaptchaService(NewSettingService(&settingRepoStub{values: settings}, &config.Config{}), verifier)
}

func TestCaptchaService_VerifyToken_SkipWhenDisabled(t *testing.T) {
	verifier := &captchaVerifierSpy{}
	service := newCaptchaServiceForTest(map[string]string{
		SettingKeyCaptchaProvider: CaptchaProviderHcaptcha,
		SettingKeyCaptchaConfig:   `{"enabled":"false","site_key":"site","secret_key":"secret"}`,
	}, verifier)

	err := service.VerifyToken(context.Background(), "", "127.0.0.1")

	require.NoError(t, err)
	require.Equal(t, 0, verifier.called)
}

func TestCaptchaService_VerifyToken_RequiresSecret(t *testing.T) {
	service := newCaptchaServiceForTest(map[string]string{
		SettingKeyCaptchaProvider: CaptchaProviderHcaptcha,
		SettingKeyCaptchaConfig:   `{"enabled":"true","site_key":"site"}`,
	}, &captchaVerifierSpy{})

	err := service.VerifyToken(context.Background(), "token", "127.0.0.1")

	require.ErrorIs(t, err, ErrCaptchaNotConfigured)
}

func TestCaptchaService_VerifyToken_RequiresToken(t *testing.T) {
	service := newCaptchaServiceForTest(map[string]string{
		SettingKeyCaptchaProvider: CaptchaProviderHcaptcha,
		SettingKeyCaptchaConfig:   `{"enabled":"true","site_key":"site","secret_key":"secret"}`,
	}, &captchaVerifierSpy{})

	err := service.VerifyToken(context.Background(), "", "127.0.0.1")

	require.ErrorIs(t, err, ErrCaptchaVerificationFailed)
}

func TestCaptchaService_VerifyToken_ReturnsProviderFailure(t *testing.T) {
	service := newCaptchaServiceForTest(map[string]string{
		SettingKeyCaptchaProvider: CaptchaProviderHcaptcha,
		SettingKeyCaptchaConfig:   `{"enabled":"true","site_key":"site","secret_key":"secret"}`,
	}, &captchaVerifierSpy{result: &CaptchaVerifyResponse{Success: false}})

	err := service.VerifyToken(context.Background(), "token", "127.0.0.1")

	require.ErrorIs(t, err, ErrCaptchaVerificationFailed)
}

func TestCaptchaService_VerifyToken_PropagatesRequestError(t *testing.T) {
	service := newCaptchaServiceForTest(map[string]string{
		SettingKeyCaptchaProvider: CaptchaProviderHcaptcha,
		SettingKeyCaptchaConfig:   `{"enabled":"true","site_key":"site","secret_key":"secret"}`,
	}, &captchaVerifierSpy{err: errors.New("network failed")})

	err := service.VerifyToken(context.Background(), "token", "127.0.0.1")

	require.ErrorContains(t, err, "send request")
}

func TestCaptchaService_ValidateProviderSecretKey(t *testing.T) {
	service := newCaptchaServiceForTest(nil, &captchaVerifierSpy{
		result: &CaptchaVerifyResponse{ErrorCodes: []string{"invalid-input-secret"}},
	})

	err := service.ValidateProviderSecretKey(context.Background(), CaptchaProviderHcaptcha, "bad-secret")

	require.ErrorIs(t, err, ErrCaptchaInvalidSecretKey)
}

func TestCaptchaService_ValidateSecretKey_PreservesTurnstileCompatibility(t *testing.T) {
	service := newCaptchaServiceForTest(nil, &captchaVerifierSpy{
		result: &CaptchaVerifyResponse{ErrorCodes: []string{"missing-input-secret"}},
	})

	err := service.ValidateSecretKey(context.Background(), "bad-secret")

	require.ErrorIs(t, err, ErrTurnstileInvalidSecretKey)
}
