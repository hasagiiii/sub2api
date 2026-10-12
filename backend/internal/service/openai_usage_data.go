package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// QueryUsageData returns the complete /wham/usage JSON for the account data
// viewer. Unlike QueryUsage, it neither projects fields nor fetches reset-credit
// details, updates quota state, or triggers automatic credit consumption.
func (s *OpenAIQuotaService) QueryUsageData(ctx context.Context, accountID int64) (json.RawMessage, error) {
	return s.queryOpenAIData(ctx, accountID, chatGPTUsageURL, nil)
}

// QueryUsageAnalytics only permits the supported daily usage endpoints and
// builds their query parameters independently of account credentials.
func (s *OpenAIQuotaService) QueryUsageAnalytics(ctx context.Context, accountID int64, metric, startDate, endDate string) (json.RawMessage, error) {
	var endpoint, limitKey string
	var extraHeaders map[string]string
	switch metric {
	case "plugins":
		endpoint, limitKey = "analytics/daily-plugin-usage-metrics", "top_plugin_limit"
	case "skills":
		endpoint, limitKey = "analytics/daily-skill-usage-metrics", "top_skill_limit"
	case "tokens":
		endpoint = "usage/daily-token-usage-breakdown"
		extraHeaders = map[string]string{"x-openai-codex-usage-categories": "1"}
	case "workspace":
		endpoint = "analytics/daily-workspace-usage-counts"
	default:
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_USAGE_INVALID_METRIC", "unsupported OpenAI usage metric")
	}
	start, startErr := time.Parse(time.DateOnly, startDate)
	end, endErr := time.Parse(time.DateOnly, endDate)
	if startErr != nil || endErr != nil || start.After(end) {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_USAGE_INVALID_DATES", "start_date and end_date must be YYYY-MM-DD dates with start_date <= end_date")
	}
	query := url.Values{
		"start_date": {startDate},
		"end_date":   {endDate},
		"group_by":   {"day"},
	}
	if limitKey != "" {
		query.Set(limitKey, "10")
	}
	if metric != "tokens" {
		query.Set("workspace_user", "true")
	}
	return s.queryOpenAIData(ctx, accountID, "https://chatgpt.com/backend-api/wham/"+endpoint+"?"+query.Encode(), extraHeaders)
}

func (s *OpenAIQuotaService) queryOpenAIData(ctx context.Context, accountID int64, endpoint string, extraHeaders map[string]string) (json.RawMessage, error) {
	callCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()

	accessToken, chatGPTAccountID, proxyURL, fedRAMP, err := s.prepareUpstreamCall(callCtx, accountID)
	if err != nil {
		return nil, err
	}
	client, err := s.privacyClientFactory(proxyURL)
	if err != nil {
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_QUOTA_CLIENT_ERROR", "failed to build upstream client")
	}
	agentIdentity := s.isAgentIdentityAccount(callCtx, accountID)
	for recovered := false; ; {
		headers, expectedTaskID, err := s.buildCodexQuotaHeaders(callCtx, accountID, accessToken, chatGPTAccountID, fedRAMP)
		if err != nil {
			return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_QUOTA_AUTH_FAILED", "failed to build upstream authentication")
		}
		headers["accept-language"] = "zh-CN,zh;q=0.9"
		headers["oai-app-brand"] = "chatgpt"
		headers["originator"] = "Codex Browser"
		headers["referer"] = "https://chatgpt.com/"
		headers["sec-fetch-site"] = "same-origin"
		headers["sec-fetch-mode"] = "cors"
		headers["priority"] = "u=1, i"
		headers["x-openai-codex-pricing-chooser"] = "1"
		for key, value := range extraHeaders {
			headers[key] = value
		}
		resp, err := client.R().SetContext(callCtx).SetHeaders(headers).Get(endpoint)
		if err != nil {
			return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_QUOTA_REQUEST_FAILED", "OpenAI usage request failed or timed out")
		}
		if !resp.IsSuccessState() {
			if agentIdentity && !recovered && isAgentIdentityTaskInvalidHTTPResponse(resp.StatusCode, resp.Bytes()) {
				recovered = true
				if err := s.recoverAgentIdentityTask(callCtx, accountID, expectedTaskID); err != nil {
					return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_QUOTA_AUTH_FAILED", "agent identity task recovery failed")
				}
				continue
			}
			// Upstream 401/403 must not be mistaken for an expired panel session.
			// Error pages can contain reflected credentials or browser challenges.
			status := http.StatusBadGateway
			if resp.StatusCode == http.StatusTooManyRequests {
				status = http.StatusTooManyRequests
			}
			return nil, infraerrors.Newf(status, "OPENAI_QUOTA_UPSTREAM_ERROR", "OpenAI usage upstream returned HTTP %d", resp.StatusCode)
		}
		body := bytes.TrimSpace(resp.Bytes())
		if !json.Valid(body) || len(body) == 0 || (body[0] != '{' && body[0] != '[') {
			return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_QUOTA_INVALID_RESPONSE", "OpenAI usage upstream returned an invalid JSON object or array")
		}
		return json.RawMessage(body), nil
	}
}
