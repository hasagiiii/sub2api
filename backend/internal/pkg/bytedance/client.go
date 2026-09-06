// Package bytedance implements the synchronous Ark image protocol.
package bytedance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

const responseLimit = 32 << 20

var ErrOutcomeUnknown = errors.New("bytedance: image request outcome is unknown")

type Client struct {
	HTTP    *http.Client
	BaseURL string
	APIKey  string
}

func NewClient(baseURL, apiKey, proxyURL string) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = domain.BytedanceBaseURL
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("bytedance: base URL must be an HTTPS origin and path")
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("bytedance: API key is required")
	}
	shared, err := httpclient.GetClient(httpclient.Options{ProxyURL: proxyURL, Timeout: 5 * time.Minute, ValidateResolvedIP: true})
	if err != nil {
		return nil, err
	}
	client := *shared
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{HTTP: &client, BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey}, nil
}

func (c *Client) Generate(ctx context.Context, payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/images/generations", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	logger.L().Info("bytedance.images.upstream_request",
		zap.String("url", req.URL.String()),
		zap.Any("headers", headersForLog(req.Header)),
		zap.String("body", string(body)),
	)
	rsp, err := c.HTTP.Do(req)
	if err != nil {
		logger.L().Info("bytedance.images.upstream_response",
			zap.String("url", req.URL.String()),
			zap.Error(err),
		)
		return nil, ErrOutcomeUnknown
	}
	defer func() { _ = rsp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(rsp.Body, responseLimit+1))
	if err != nil || len(raw) > responseLimit {
		logger.L().Info("bytedance.images.upstream_response",
			zap.String("url", req.URL.String()),
			zap.Any("headers", rsp.Header),
			zap.Int("status", rsp.StatusCode),
			zap.String("body", string(raw)),
			zap.Error(err),
		)
		return nil, fmt.Errorf("%w: invalid or oversized response", ErrOutcomeUnknown)
	}
	logger.L().Info("bytedance.images.upstream_response",
		zap.String("url", req.URL.String()),
		zap.Any("headers", rsp.Header),
		zap.Int("status", rsp.StatusCode),
		zap.String("body", string(raw)),
	)
	if rsp.StatusCode < 200 || rsp.StatusCode >= 300 {
		return nil, fmt.Errorf("bytedance: upstream HTTP %d", rsp.StatusCode)
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result == nil {
		return nil, fmt.Errorf("%w: invalid JSON response", ErrOutcomeUnknown)
	}
	if result["error"] != nil {
		return nil, errors.New("bytedance: upstream generation error")
	}
	return result, nil
}

func headersForLog(headers http.Header) map[string][]string {
	result := make(map[string][]string, len(headers))
	for key, values := range headers {
		if strings.EqualFold(key, "Authorization") {
			result[key] = []string{"Bearer [REDACTED]"}
			continue
		}
		result[key] = append([]string(nil), values...)
	}
	return result
}

// NormalizeRequest strips local editor data and validates the supported model contract.
func NormalizeRequest(raw []byte, model string) (map[string]any, map[string]any, error) {
	var p map[string]any
	if json.Unmarshal(raw, &p) != nil || p == nil {
		return nil, nil, errors.New("invalid JSON image request")
	}
	metadata := map[string]any{}
	if v, ok := p["_annotations"]; ok {
		metadata["_annotations"] = v
		delete(p, "_annotations")
	}
	if v, ok := p["_annotation_prompt"]; ok {
		metadata["_annotation_prompt"] = v
		delete(p, "_annotation_prompt")
	}
	for _, key := range []string{"sequential_image_generation", "sequential_image_generation_options", "tools", "web_search", "search"} {
		if _, ok := p[key]; ok {
			return nil, nil, fmt.Errorf("%s is not supported", key)
		}
	}
	if v, ok := p["stream"]; ok && v != false {
		return nil, nil, errors.New("streaming is not supported")
	}
	delete(p, "stream")
	prompt, ok := p["prompt"].(string)
	if !ok || strings.TrimSpace(prompt) == "" {
		return nil, nil, errors.New("prompt is required")
	}
	if v, ok := p["layer_decomposition"]; ok {
		if _, valid := v.(bool); !valid {
			return nil, nil, errors.New("layer_decomposition must be boolean")
		}
	}
	if v, ok := p["watermark"]; ok {
		if _, valid := v.(bool); !valid {
			return nil, nil, errors.New("watermark must be boolean")
		}
	}
	var images []string
	switch v := p["image"].(type) {
	case nil:
	case string:
		if strings.TrimSpace(v) != "" {
			images = append(images, v)
		}
	case []any:
		for _, item := range v {
			text, valid := item.(string)
			if !valid || strings.TrimSpace(text) == "" {
				return nil, nil, errors.New("image entries must be URLs")
			}
			images = append(images, text)
		}
	default:
		return nil, nil, errors.New("image must be a URL or an array of URLs")
	}
	if len(images) > 10 {
		return nil, nil, errors.New("at most 10 reference images are allowed")
	}
	for _, image := range images {
		u, err := url.Parse(image)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
			return nil, nil, errors.New("image must be an HTTP(S) URL")
		}
	}
	if p["layer_decomposition"] == true && len(images) != 1 {
		return nil, nil, errors.New("layer decomposition requires exactly one reference image")
	}
	switch len(images) {
	case 0:
		delete(p, "image")
	case 1:
		p["image"] = images[0]
	default:
		p["image"] = images
	}
	for key, value := range map[string]any{"size": "2K", "output_format": "jpeg", "response_format": "url", "watermark": true, "layer_decomposition": false} {
		if _, ok := p[key]; !ok {
			p[key] = value
		}
	}
	if p["response_format"] != "url" {
		return nil, nil, errors.New("only response_format=url is supported")
	}
	for _, key := range []string{"size", "output_format"} {
		if v, ok := p[key].(string); !ok || strings.TrimSpace(v) == "" {
			return nil, nil, fmt.Errorf("%s must be a nonempty string", key)
		}
	}
	p["model"] = model
	return p, metadata, nil
}

// BillableImages treats generated_images as already excluding the free background.
func BillableImages(result map[string]any, layers bool) (int, error) {
	if usage, ok := result["usage"].(map[string]any); ok {
		if raw, exists := usage["generated_images"]; exists {
			if n, valid := integer(raw); valid && n >= 0 {
				return n, nil
			}
			return 0, errors.New("invalid generated_images")
		}
	}
	data, ok := result["data"].([]any)
	if !ok || len(data) == 0 {
		return 0, errors.New("missing image output")
	}
	count := 0
	for _, raw := range data {
		entry, ok := raw.(map[string]any)
		if !ok {
			return 0, errors.New("invalid image output")
		}
		imageURL, _ := entry["url"].(string)
		if strings.TrimSpace(imageURL) == "" {
			continue
		}
		if !layers {
			count++
			continue
		}
		z, valid := integer(entry["z_index"])
		if !valid || z < 0 {
			return 0, errors.New("missing layer index")
		}
		if z > 0 {
			count++
		}
	}
	return count, nil
}

func integer(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if !math.IsNaN(n) && !math.IsInf(n, 0) && n == math.Trunc(n) && n >= 0 && n <= math.MaxInt32 {
			return int(n), true
		}
	case int:
		return n, n >= 0 && n <= math.MaxInt32
	}
	return 0, false
}
