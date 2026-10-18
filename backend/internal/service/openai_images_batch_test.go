package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type batchImageUpstream struct {
	calls       atomic.Int32
	active      atomic.Int32
	peak        atomic.Int32
	mu          sync.Mutex
	bodies      [][]byte
	paths       []string
	entered     chan struct{}
	gate        <-chan struct{}
	fail        func(int) int
	nativeBatch bool
}

func (u *batchImageUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	id := int(u.calls.Add(1)) - 1
	active := u.active.Add(1)
	defer u.active.Add(-1)
	for previous := u.peak.Load(); active > previous; previous = u.peak.Load() {
		if u.peak.CompareAndSwap(previous, active) {
			break
		}
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.mu.Lock()
	u.bodies = append(u.bodies, body)
	u.paths = append(u.paths, req.URL.Path)
	u.mu.Unlock()
	if u.gate != nil {
		u.entered <- struct{}{}
		<-u.gate
	}
	status := http.StatusOK
	if u.fail != nil {
		status = u.fail(id)
	}
	payload := `{"error":{"type":"invalid_request_error","message":"image rejected"}}`
	if status == http.StatusOK {
		count := 1
		if u.nativeBatch {
			count = max(1, int(gjson.GetBytes(body, "n").Int()))
		}
		images := make([]map[string]any, 0, count)
		for i := 0; i < count; i++ {
			images = append(images, map[string]any{"b64_json": base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("image-%d-%d", id, i))), "revised_prompt": "original prompt", "size": "1024x1024"})
		}
		encoded, _ := json.Marshal(map[string]any{"created": 1710000000, "model": "gpt-image-2", "data": images, "usage": map[string]any{"input_tokens": 10, "output_tokens": 20, "input_tokens_details": map[string]any{"image_tokens": 4, "cached_tokens": 2, "cached_tokens_details": map[string]any{"image_tokens": 1, "text_tokens": 1}}}})
		payload = string(encoded)
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {fmt.Sprintf("batch-%d", id)}}, Body: io.NopCloser(strings.NewReader(payload)), Request: req}, nil
}
func (u *batchImageUpstream) DoWithTLS(req *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, p, id, n)
}

func TestOpenAIImagesBatchParallelGenerationAndEdits(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
		for _, edit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/edit=%t", accountType, edit), func(t *testing.T) {
				payload := map[string]any{"model": "gpt-image-2.5-sunburst", "prompt": "draw", "num_images": 3}
				if edit {
					payload["images"] = []any{map[string]any{"image_url": "https://example.test/reference.png"}}
					payload["mask"] = map[string]any{"image_url": "https://example.test/mask.png"}
				}
				body, _ := json.Marshal(payload)
				c, rec := newOpenAIImagesTestContext(t, body)
				endpoint := "/backend-api/codex/images/generations"
				if edit {
					c.Request.URL.Path = "/v1/images/edit"
					endpoint = "/backend-api/codex/images/edits"
				}
				gate := make(chan struct{})
				var release sync.Once
				t.Cleanup(func() { release.Do(func() { close(gate) }) })
				upstream := &batchImageUpstream{gate: gate, entered: make(chan struct{}, 3)}
				svc := newOpenAIImagesTestService(upstream)
				store := &responsesImageStatusStoreStub{status: &ResponsesImageStatus{RequestID: "batch-status", Status: ResponsesImageStatusRunning}}
				svc.responsesImageStatusStore = store
				parsed, err := svc.ParseOpenAIImagesRequest(c, body)
				require.NoError(t, err)
				account := directImagesTestAccount()
				account.Type = accountType
				account.Concurrency = 3
				type outcome struct {
					result *OpenAIForwardResult
					err    error
				}
				done := make(chan outcome, 1)
				go func() {
					r, e := svc.ForwardImages(WithResponsesImageStatusRequestID(context.Background(), "batch-status"), c, account, body, parsed, "")
					done <- outcome{r, e}
				}()
				// All three requests must arrive before ANY upstream response is released.
				for range 3 {
					select {
					case <-upstream.entered:
					case <-time.After(5 * time.Second):
						t.Fatal("batch requests were not sent in parallel")
					}
				}
				require.Zero(t, store.sets, "children must not complete the parent's status")
				release.Do(func() { close(gate) })
				out := <-done
				require.NoError(t, out.err)
				require.Equal(t, int32(3), upstream.calls.Load())
				require.Equal(t, int32(3), upstream.peak.Load())
				require.Equal(t, 3, out.result.ImageCount)
				require.Equal(t, 30, out.result.Usage.InputTokens)
				require.Equal(t, 60, out.result.Usage.OutputTokens)
				require.Equal(t, 60, out.result.Usage.ImageOutputTokens)
				require.Equal(t, 12, out.result.Usage.ImageInputTokens)
				require.Equal(t, 3, out.result.Usage.ImageCacheReadTokens)
				require.Equal(t, int64(30), gjson.Get(rec.Body.String(), "usage.input_tokens").Int())
				require.Len(t, gjson.Get(rec.Body.String(), "data").Array(), 3)
				require.Equal(t, 2, store.sets, "only the combined upstream-done and succeeded updates are published")
				for i, request := range upstream.bodies {
					require.Equal(t, endpoint, upstream.paths[i])
					require.LessOrEqual(t, gjson.GetBytes(request, "n").Int(), int64(1))
					require.False(t, gjson.GetBytes(request, "num_images").Exists())
					if edit {
						require.Equal(t, "https://example.test/reference.png", gjson.GetBytes(request, "images.0.image_url").String())
						require.Equal(t, "https://example.test/mask.png", gjson.GetBytes(request, "mask.image_url").String())
					}
				}
			})
		}
	}
}

func TestOpenAIImagesBatchPartialSuccessReturnsActualCount(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"draw","n":3}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	upstream := &batchImageUpstream{fail: func(i int) int {
		if i == 1 {
			return 400
		}
		return 200
	}}
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(context.Background(), c, directImagesTestAccount(), body, parsed, "")
	require.NoError(t, err, "partial success must not trigger a whole-batch retry or refund")
	require.Equal(t, int32(3), upstream.calls.Load())
	require.Equal(t, 2, result.ImageCount)
	require.Equal(t, 20, result.Usage.InputTokens)
	require.Len(t, gjson.Get(rec.Body.String(), "data").Array(), 2)
	require.Len(t, gjson.Get(rec.Body.String(), "errors").Array(), 1)
	require.EqualValues(t, 3, gjson.Get(rec.Body.String(), "requested_images").Int())
	require.EqualValues(t, 2, gjson.Get(rec.Body.String(), "generated_images").Int())
}

func TestOpenAIImagesBatchAllFailedPreservesClientError(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"draw","n":2}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	svc := newOpenAIImagesTestService(&batchImageUpstream{fail: func(int) int { return 400 }})
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(context.Background(), c, directImagesTestAccount(), body, parsed, "")
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, 400, rec.Code)
	require.Equal(t, "image rejected", gjson.Get(rec.Body.String(), "error.message").String())
}

func TestOpenAIImagesBatchStreamEmitsAllCompletedImages(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"edit","images":[{"image_url":"data:image/png;base64,AA=="}],"n":3,"stream":true,"response_format":"url"}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	c.Request.URL.Path = "/v1/images/edits"
	upstream := &batchImageUpstream{}
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(context.Background(), c, directImagesTestAccount(), body, parsed, "")
	require.NoError(t, err)
	require.Equal(t, 3, result.ImageCount)
	require.True(t, result.Stream)
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	require.Equal(t, 3, strings.Count(rec.Body.String(), "event: image_edit.completed"))
	require.Contains(t, rec.Body.String(), `"image_index":2`)
	require.Contains(t, rec.Body.String(), "data:image/png;base64,")
	require.Equal(t, 3, strings.Count(rec.Body.String(), `"b64_json":`))
	require.Equal(t, 1, strings.Count(rec.Body.String(), `"usage":`), "batch totals are emitted once")
}

func TestOpenAIImagesBatchAPIKeyUsesNativeBatch(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"draw","n":3}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	upstream := &batchImageUpstream{nativeBatch: true}
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(context.Background(), c, newOpenAIImagesAPIKeyAccount(), body, parsed, "")
	require.NoError(t, err)
	require.Equal(t, int32(1), upstream.calls.Load())
	require.EqualValues(t, 3, gjson.GetBytes(upstream.bodies[0], "n").Int())
	require.Equal(t, 3, result.ImageCount)
	require.Len(t, gjson.Get(rec.Body.String(), "data").Array(), 3)
}

func TestOpenAIImagesBatchCountValidation(t *testing.T) {
	for _, body := range []string{`{"num_images":0}`, `{"num_images":1.5}`, `{"num_images":"3"}`, `{"n":-1}`} {
		c, _ := newOpenAIImagesTestContext(t, []byte(body))
		_, err := newOpenAIImagesTestService(nil).ParseOpenAIImagesRequest(c, []byte(body))
		require.Error(t, err, body)
	}
	body := []byte(`{"model":"gpt-image-2","prompt":"draw","n":2,"num_images":3}`)
	c, _ := newOpenAIImagesTestContext(t, body)
	svc := newOpenAIImagesTestService(nil)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	require.Equal(t, 2, parsed.N)
	var multipartBody bytes.Buffer
	writer := multipart.NewWriter(&multipartBody)
	require.NoError(t, writer.WriteField("num_images", "3"))
	require.NoError(t, writer.WriteField("n", "2"))
	require.NoError(t, writer.Close())
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	parsed, err = svc.ParseOpenAIImagesRequest(c, multipartBody.Bytes())
	require.NoError(t, err)
	require.Equal(t, 2, parsed.N)
}

func TestOpenAIImagesBatchRejectsExcessiveCountBeforeSending(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"draw","n":11}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	upstream := &batchImageUpstream{}
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(context.Background(), c, directImagesTestAccount(), body, parsed, "")
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, 400, rec.Code)
	require.Zero(t, upstream.calls.Load())
}

type batchImageConcurrencyCache struct {
	ConcurrencyCache
	mu       sync.Mutex
	current  int
	peak     int
	acquires int
}

func (c *batchImageConcurrencyCache) AcquireAccountSlot(_ context.Context, _ int64, limit int, _ string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.acquires++
	if c.current >= limit {
		return false, nil
	}
	c.current++
	c.peak = max(c.peak, c.current)
	return true, nil
}
func (c *batchImageConcurrencyCache) ReleaseAccountSlot(context.Context, int64, string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current--
	return nil
}

func TestOpenAIImagesBatchConcurrencySlots(t *testing.T) {
	for _, test := range []struct {
		name     string
		limit    int
		held     bool
		expected int
	}{
		{"parent slot at account limit", 1, true, 1},
		{"unified facade reserves its own slots", 2, false, 2},
		{"parallelism is bounded", 10, false, 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(`{"model":"gpt-image-2","prompt":"draw","n":6}`)
			c, _ := newOpenAIImagesTestContext(t, body)
			gate := make(chan struct{})
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(gate) }) })
			upstream := &batchImageUpstream{gate: gate, entered: make(chan struct{}, 6)}
			svc := newOpenAIImagesTestService(upstream)
			cache := &batchImageConcurrencyCache{}
			svc.concurrencyService = NewConcurrencyService(cache)
			account := directImagesTestAccount()
			account.Concurrency = test.limit
			ctx := context.Background()
			if test.held {
				slot, err := svc.concurrencyService.AcquireAccountSlot(ctx, account.ID, account.Concurrency)
				require.NoError(t, err)
				require.True(t, slot.Acquired)
				defer slot.ReleaseFunc()
				ctx = WithOpenAIImagesAccountSlot(ctx, account.ID)
			}
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() { _, err := svc.ForwardImages(ctx, c, account, body, parsed, ""); done <- err }()
			for range test.expected {
				select {
				case <-upstream.entered:
				case <-time.After(5 * time.Second):
					t.Fatal("workers did not acquire usable slots")
				}
			}
			release.Do(func() { close(gate) })
			require.NoError(t, <-done)
			require.Equal(t, int32(6), upstream.calls.Load())
			require.LessOrEqual(t, upstream.peak.Load(), int32(test.expected))
			require.Equal(t, test.expected, cache.peak)
			if test.held {
				require.Equal(t, 1, cache.current)
				require.Equal(t, 1, cache.acquires)
			} else {
				require.Zero(t, cache.current)
			}
		})
	}
}

type batchImageRPMCache struct {
	RPMCache
	count atomic.Int32
}

func (c *batchImageRPMCache) TryAcquireRPM(_ context.Context, _ int64, limit int) (bool, int, time.Time, error) {
	count := int(c.count.Add(1))
	return count <= limit, count, time.Now().Add(time.Minute), nil
}
func TestOpenAIImagesBatchCountsEachActualSendForRPM(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"draw","n":3}`)
	c, _ := newOpenAIImagesTestContext(t, body)
	upstream := &batchImageUpstream{}
	svc := newOpenAIImagesTestService(upstream)
	cache := &batchImageRPMCache{}
	svc.rpmCache = cache
	account := directImagesTestAccount()
	account.Extra = map[string]any{"base_rpm": 3}
	allowed, reservation, err := svc.TryAcquireOpenAIOAuthRPM(context.Background(), account)
	require.NoError(t, err)
	require.True(t, allowed)
	ctx := WithOpenAIRPMReservation(context.Background(), account, reservation)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(ctx, c, account, body, parsed, "")
	require.NoError(t, err)
	require.Equal(t, 3, result.ImageCount)
	require.Equal(t, int32(3), cache.count.Load(), "the handler's first reservation is shared, not duplicated")
	require.Equal(t, int32(3), upstream.calls.Load())
}

func TestOpenAIImagesBatchResponsesFallback(t *testing.T) {
	var directCalls, responsesCalls atomic.Int32
	var badCount atomic.Bool
	upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		if strings.Contains(req.URL.Path, "/images/") {
			directCalls.Add(1)
			if gjson.GetBytes(body, "n").Int() > 1 {
				badCount.Store(true)
			}
			return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"not found"}}`)), Request: req}, nil
		}
		responsesCalls.Add(1)
		if gjson.GetBytes(body, "tools.0.n").Int() > 1 {
			badCount.Store(true)
		}
		response := "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":10},\"output\":[{\"type\":\"image_generation_call\",\"result\":\"aGVsbG8=\",\"size\":\"1024x1024\"}]}}\n\n"
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(response)), Request: req}, nil
	}}
	body := []byte(`{"model":"gpt-image-2","prompt":"draw","n":3}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(context.Background(), c, directImagesTestAccount(), body, parsed, "")
	require.NoError(t, err)
	require.Equal(t, 3, result.ImageCount)
	require.Equal(t, int32(3), directCalls.Load())
	require.Equal(t, int32(3), responsesCalls.Load())
	require.False(t, badCount.Load())
	require.Len(t, gjson.Get(rec.Body.String(), "data").Array(), 3)
	require.Equal(t, 15, result.Usage.InputTokens)
}

func TestOpenAIImagesBatchDrainsAfterStreamClientDisconnect(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"draw","n":3,"stream":true}`)
	c, _ := newOpenAIImagesTestContext(t, body)
	c.Writer = &failingOpenAIImageWriter{ResponseWriter: c.Writer, failAfter: 0}
	upstream := &batchImageUpstream{}
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(context.Background(), c, directImagesTestAccount(), body, parsed, "")
	require.NoError(t, err)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 3, result.ImageCount)
	require.Equal(t, 30, result.Usage.InputTokens)
	require.Equal(t, int32(3), upstream.calls.Load())
}

func TestOpenAIImagesBatchMultipartEditCopiesImagesAndMask(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{"model": "gpt-image-2", "prompt": "edit", "num_images": "2"} {
		require.NoError(t, writer.WriteField(key, value))
	}
	imageBase64 := encodeOpenAIImageTestPNG(t, 1, 1)
	imageBytes, err := base64.StdEncoding.DecodeString(imageBase64)
	require.NoError(t, err)
	for _, field := range []string{"image[]", "mask"} {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="image.png"`, field))
		header.Set("Content-Type", "image/png")
		part, err := writer.CreatePart(header)
		require.NoError(t, err)
		_, err = part.Write(imageBytes)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	c, _ := newOpenAIImagesTestContext(t, body.Bytes())
	c.Request.URL.Path = "/v1/images/edits"
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	upstream := &batchImageUpstream{}
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body.Bytes())
	require.NoError(t, err)
	result, err := svc.ForwardImages(context.Background(), c, directImagesTestAccount(), body.Bytes(), parsed, "")
	require.NoError(t, err)
	require.Equal(t, 2, result.ImageCount)
	require.Equal(t, int32(2), upstream.calls.Load())
	for _, payload := range upstream.bodies {
		require.Equal(t, "data:image/png;base64,"+imageBase64, gjson.GetBytes(payload, "images.0.image_url").String())
		require.Equal(t, "data:image/png;base64,"+imageBase64, gjson.GetBytes(payload, "mask.image_url").String())
	}
}
