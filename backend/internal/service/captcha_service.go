package service

import (
	"context"
	"fmt"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

var (
	ErrTurnstileVerificationFailed = infraerrors.BadRequest("TURNSTILE_VERIFICATION_FAILED", "turnstile verification failed")
	ErrTurnstileNotConfigured      = infraerrors.ServiceUnavailable("TURNSTILE_NOT_CONFIGURED", "turnstile not configured")
	ErrTurnstileInvalidSecretKey   = infraerrors.BadRequest("TURNSTILE_INVALID_SECRET_KEY", "invalid turnstile secret key")
	ErrCaptchaVerificationFailed   = infraerrors.BadRequest("CAPTCHA_VERIFICATION_FAILED", "captcha verification failed")
	ErrCaptchaNotConfigured        = infraerrors.ServiceUnavailable("CAPTCHA_NOT_CONFIGURED", "captcha not configured")
	ErrCaptchaInvalidSecretKey     = infraerrors.BadRequest("CAPTCHA_INVALID_SECRET_KEY", "invalid captcha secret key")
)

const (
	CaptchaProviderTurnstile = "turnstile"
	CaptchaProviderHcaptcha  = "hcaptcha"
)

type CaptchaVerifier interface {
	VerifyToken(ctx context.Context, provider, secretKey, token, remoteIP string) (*CaptchaVerifyResponse, error)
}

// CaptchaService verifies tokens for the currently configured captcha provider.
type CaptchaService struct {
	settingService *SettingService
	verifier       CaptchaVerifier
}

// CaptchaVerifyResponse is the shared siteverify response shape used by Turnstile and hCaptcha.
type CaptchaVerifyResponse struct {
	Success     bool     `json:"success"`
	ChallengeTS string   `json:"challenge_ts"`
	Hostname    string   `json:"hostname"`
	ErrorCodes  []string `json:"error-codes"`
	Action      string   `json:"action"`
	CData       string   `json:"cdata"`
}

func NewCaptchaService(settingService *SettingService, verifier CaptchaVerifier) *CaptchaService {
	return &CaptchaService{
		settingService: settingService,
		verifier:       verifier,
	}
}

// VerifyToken 验证当前启用的人机验证 token
func (s *CaptchaService) VerifyToken(ctx context.Context, token string, remoteIP string) error {
	runtime := s.settingService.GetCaptchaRuntime(ctx)
	if !runtime.Enabled {
		logger.LegacyPrintf("service.captcha", "%s", "[Captcha] Disabled, skipping verification")
		return nil
	}

	if runtime.SecretKey == "" {
		logger.LegacyPrintf("service.captcha", "[Captcha] Secret key not configured for provider=%s", runtime.Provider)
		if runtime.Provider == CaptchaProviderTurnstile {
			return ErrTurnstileNotConfigured
		}
		return ErrCaptchaNotConfigured
	}

	if token == "" {
		logger.LegacyPrintf("service.captcha", "%s", "[Captcha] Token is empty")
		if runtime.Provider == CaptchaProviderTurnstile {
			return ErrTurnstileVerificationFailed
		}
		return ErrCaptchaVerificationFailed
	}

	logger.LegacyPrintf("service.captcha", "[Captcha] Verifying token for provider=%s IP=%s", runtime.Provider, remoteIP)
	result, err := s.verifier.VerifyToken(ctx, runtime.Provider, runtime.SecretKey, token, remoteIP)
	if err != nil {
		logger.LegacyPrintf("service.captcha", "[Captcha] Request failed: %v", err)
		return fmt.Errorf("send request: %w", err)
	}

	if !result.Success {
		logger.LegacyPrintf("service.captcha", "[Captcha] Verification failed, error codes: %v", result.ErrorCodes)
		if runtime.Provider == CaptchaProviderTurnstile {
			return ErrTurnstileVerificationFailed
		}
		return ErrCaptchaVerificationFailed
	}

	logger.LegacyPrintf("service.captcha", "%s", "[Captcha] Verification successful")
	return nil
}

// IsEnabled checks whether any captcha provider is enabled.
func (s *CaptchaService) IsEnabled(ctx context.Context) bool {
	return s.settingService.GetCaptchaRuntime(ctx).Enabled
}

// ValidateSecretKey 验证 Turnstile Secret Key 是否有效
func (s *CaptchaService) ValidateSecretKey(ctx context.Context, secretKey string) error {
	return s.ValidateProviderSecretKey(ctx, CaptchaProviderTurnstile, secretKey)
}

func (s *CaptchaService) ValidateProviderSecretKey(ctx context.Context, provider, secretKey string) error {
	// 发送一个测试token的验证请求来检查secret_key是否有效
	result, err := s.verifier.VerifyToken(ctx, provider, secretKey, "test-validation", "")
	if err != nil {
		return fmt.Errorf("validate secret key: %w", err)
	}

	invalidSecretCodes := map[string]bool{
		"invalid-input-secret": true,
		"missing-input-secret": true,
	}
	for _, code := range result.ErrorCodes {
		if invalidSecretCodes[code] {
			if provider == CaptchaProviderTurnstile {
				return ErrTurnstileInvalidSecretKey
			}
			return ErrCaptchaInvalidSecretKey
		}
	}

	// 其他错误（如 invalid-input-response）说明 secret key 是有效的
	return nil
}
