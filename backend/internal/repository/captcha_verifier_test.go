package repository

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type CaptchaVerifierSuite struct {
	suite.Suite
	ctx      context.Context
	verifier *captchaHTTPVerifier
	received chan url.Values
}

func (s *CaptchaVerifierSuite) SetupTest() {
	s.ctx = context.Background()
	s.received = make(chan url.Values, 1)
	verifier, ok := NewCaptchaVerifier().(*captchaHTTPVerifier)
	require.True(s.T(), ok, "type assertion failed")
	s.verifier = verifier
}

func (s *CaptchaVerifierSuite) setupTransport(provider string, handler http.HandlerFunc) {
	s.verifier.verifyURLs[provider] = "http://in-process/" + provider
	s.verifier.httpClient = &http.Client{
		Transport: newInProcessTransport(handler, nil),
	}
}

func (s *CaptchaVerifierSuite) TestVerifyToken_SendsFormAndDecodesJSON() {
	s.setupTransport(service.CaptchaProviderTurnstile, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture form data in main goroutine context later
		body, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(body))
		s.received <- values

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(service.CaptchaVerifyResponse{Success: true})
	}))

	resp, err := s.verifier.VerifyToken(s.ctx, service.CaptchaProviderTurnstile, "sk", "token", "1.1.1.1")
	require.NoError(s.T(), err, "VerifyToken")
	require.NotNil(s.T(), resp)
	require.True(s.T(), resp.Success, "expected success response")

	// Assert form fields in main goroutine
	select {
	case values := <-s.received:
		require.Equal(s.T(), "sk", values.Get("secret"))
		require.Equal(s.T(), "token", values.Get("response"))
		require.Equal(s.T(), "1.1.1.1", values.Get("remoteip"))
	default:
		require.Fail(s.T(), "expected server to receive request")
	}
}

func (s *CaptchaVerifierSuite) TestVerifyToken_ContentType() {
	var contentType string
	s.setupTransport(service.CaptchaProviderTurnstile, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(service.CaptchaVerifyResponse{Success: true})
	}))

	_, err := s.verifier.VerifyToken(s.ctx, service.CaptchaProviderTurnstile, "sk", "token", "1.1.1.1")
	require.NoError(s.T(), err)
	require.True(s.T(), strings.HasPrefix(contentType, "application/x-www-form-urlencoded"), "unexpected content-type: %s", contentType)
}

func (s *CaptchaVerifierSuite) TestVerifyToken_EmptyRemoteIP_NotSent() {
	s.setupTransport(service.CaptchaProviderTurnstile, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(body))
		s.received <- values

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(service.CaptchaVerifyResponse{Success: true})
	}))

	_, err := s.verifier.VerifyToken(s.ctx, service.CaptchaProviderTurnstile, "sk", "token", "")
	require.NoError(s.T(), err)

	select {
	case values := <-s.received:
		require.Equal(s.T(), "", values.Get("remoteip"), "remoteip should be empty or not sent")
	default:
		require.Fail(s.T(), "expected server to receive request")
	}
}

func (s *CaptchaVerifierSuite) TestVerifyToken_RequestError() {
	s.verifier.verifyURLs[service.CaptchaProviderTurnstile] = "http://in-process/turnstile"
	s.verifier.httpClient = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial failed")
		}),
	}

	_, err := s.verifier.VerifyToken(s.ctx, service.CaptchaProviderTurnstile, "sk", "token", "1.1.1.1")
	require.Error(s.T(), err, "expected error when server is closed")
}

func (s *CaptchaVerifierSuite) TestVerifyToken_InvalidJSON() {
	s.setupTransport(service.CaptchaProviderTurnstile, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "not-valid-json")
	}))

	_, err := s.verifier.VerifyToken(s.ctx, service.CaptchaProviderTurnstile, "sk", "token", "1.1.1.1")
	require.Error(s.T(), err, "expected error for invalid JSON response")
}

func (s *CaptchaVerifierSuite) TestVerifyToken_SuccessFalse() {
	s.setupTransport(service.CaptchaProviderTurnstile, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(service.CaptchaVerifyResponse{
			Success:    false,
			ErrorCodes: []string{"invalid-input-response"},
		})
	}))

	resp, err := s.verifier.VerifyToken(s.ctx, service.CaptchaProviderTurnstile, "sk", "token", "1.1.1.1")
	require.NoError(s.T(), err, "VerifyToken should not error on success=false")
	require.NotNil(s.T(), resp)
	require.False(s.T(), resp.Success)
	require.Contains(s.T(), resp.ErrorCodes, "invalid-input-response")
}

func (s *CaptchaVerifierSuite) TestVerifyToken_DispatchesProviderURLs() {
	var requestedPaths []string
	s.verifier.verifyURLs[service.CaptchaProviderTurnstile] = "http://in-process/turnstile"
	s.verifier.verifyURLs[service.CaptchaProviderHcaptcha] = "http://in-process/hcaptcha"
	s.verifier.httpClient = &http.Client{
		Transport: newInProcessTransport(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPaths = append(requestedPaths, r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(service.CaptchaVerifyResponse{Success: true})
		}), nil),
	}

	_, err := s.verifier.VerifyToken(s.ctx, service.CaptchaProviderTurnstile, "sk", "token", "")
	require.NoError(s.T(), err)
	_, err = s.verifier.VerifyToken(s.ctx, service.CaptchaProviderHcaptcha, "sk", "token", "")
	require.NoError(s.T(), err)

	require.Equal(s.T(), []string{"/turnstile", "/hcaptcha"}, requestedPaths)
}

func (s *CaptchaVerifierSuite) TestVerifyToken_UnsupportedProvider() {
	_, err := s.verifier.VerifyToken(s.ctx, "unknown", "sk", "token", "")
	require.ErrorContains(s.T(), err, "unsupported captcha provider")
}

func TestCaptchaVerifierSuite(t *testing.T) {
	suite.Run(t, new(CaptchaVerifierSuite))
}
