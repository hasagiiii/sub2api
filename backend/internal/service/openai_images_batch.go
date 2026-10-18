package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	openAIImagesBatchConcurrency = 4
	openAIImagesBatchMaxCount    = 10
)

type openAIImagesBatchChildKey struct{}
type openAIImagesAccountSlotKey struct{}

// WithOpenAIImagesAccountSlot lets a batch reuse the slot already held by its
// HTTP handler. Every additional worker reserves a separate account slot.
func WithOpenAIImagesAccountSlot(ctx context.Context, accountID int64) context.Context {
	return context.WithValue(ctx, openAIImagesAccountSlotKey{}, accountID)
}

func isOpenAIImagesBatchChild(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	child, _ := ctx.Value(openAIImagesBatchChildKey{}).(bool)
	return child
}

type openAIImageBatchItem struct {
	result   *OpenAIForwardResult
	response map[string]json.RawMessage
	images   []json.RawMessage
	recorder *httptest.ResponseRecorder
	err      error
	finished time.Time
}

func (s *OpenAIGatewayService) forwardOpenAIImagesOAuthBatch(ctx context.Context, c *gin.Context, account *Account, parsed *OpenAIImagesRequest, mappedModel string) (*OpenAIForwardResult, error) {
	start := time.Now()
	if parsed.N > openAIImagesBatchMaxCount {
		err := &OpenAIImagesUpstreamError{StatusCode: http.StatusBadRequest, ErrorType: "invalid_request_error", Param: "n", Message: fmt.Sprintf("n/num_images must be between 1 and %d for subscription image accounts", openAIImagesBatchMaxCount)}
		writeOpenAIImagesUpstreamErrorResponse(c, err)
		return nil, err
	}
	workerCount := min(parsed.N, openAIImagesBatchConcurrency)
	if account.Concurrency > 0 {
		workerCount = min(workerCount, account.Concurrency)
	}
	heldAccount, hasHeldSlot := ctx.Value(openAIImagesAccountSlotKey{}).(int64)
	for worker := 0; worker < workerCount; worker++ {
		if worker == 0 && hasHeldSlot && heldAccount == account.ID {
			continue
		}
		slot, err := s.tryAcquireAccountSlot(ctx, account.ID, account.Concurrency)
		if err != nil || slot == nil || !slot.Acquired {
			if worker > 0 {
				workerCount = worker
				break
			}
			upErr := &OpenAIImagesUpstreamError{StatusCode: http.StatusTooManyRequests, ErrorType: "rate_limit_error", Message: "image generation concurrency limit exceeded"}
			writeOpenAIImagesUpstreamErrorResponse(c, upErr)
			return nil, upErr
		}
		if slot.ReleaseFunc != nil {
			defer slot.ReleaseFunc()
		}
	}
	// Account model/header caches and credential refreshes are mutable. Each
	// worker gets private maps, while the normal transport still enforces RPM.
	accounts := make([]Account, workerCount)
	for i := range accounts {
		accounts[i] = *account
		var err error
		accounts[i].Credentials, err = cloneAccountJSONMap(account.Credentials)
		if err != nil {
			return nil, fmt.Errorf("copy image account credentials: %w", err)
		}
		accounts[i].Extra, err = cloneAccountJSONMap(account.Extra)
		if err != nil {
			return nil, fmt.Errorf("copy image account settings: %w", err)
		}
	}
	// Copy under the heartbeat lock: Gin's Copy also reads its underlying writer.
	var snapshot *gin.Context
	if heartbeat := openAIImagesJSONKeepaliveFromContext(c); heartbeat != nil {
		heartbeat.mu.Lock()
		snapshot = c.Copy()
		heartbeat.mu.Unlock()
	} else {
		snapshot = c.Copy()
	}
	delete(snapshot.Keys, openAIImagesJSONKeepaliveKey)
	results := make([]openAIImageBatchItem, parsed.N)
	var workers sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for index := worker; index < parsed.N; index += workerCount {
				single := *parsed
				single.N = 1
				single.Stream = false
				single.PartialImages = nil
				if parsed.Stream {
					single.ResponseFormat = "b64_json"
				}
				childCtx := context.WithValue(ctx, openAIImagesBatchChildKey{}, true)
				recorder := httptest.NewRecorder()
				child, _ := gin.CreateTestContext(recorder)
				child.Keys = snapshot.Copy().Keys
				child.Request = snapshot.Request.Clone(childCtx)
				result, err := s.ForwardImages(childCtx, child, &accounts[worker], single.Body, &single, mappedModel)
				item := openAIImageBatchItem{result: result, recorder: recorder, err: err, finished: time.Now()}
				if err == nil {
					if recorder.Code < 200 || recorder.Code >= 300 {
						item.err = fmt.Errorf("image upstream returned HTTP %d", recorder.Code)
					} else if err = json.Unmarshal(recorder.Body.Bytes(), &item.response); err != nil {
						item.err = fmt.Errorf("decode image response: %w", err)
					} else if err = json.Unmarshal(item.response["data"], &item.images); err != nil {
						item.err = errors.New("image upstream returned invalid image data")
					} else {
						item.images = validOpenAIImageBatchData(item.images)
						if len(item.images) == 0 {
							item.err = errors.New("image upstream returned no images")
						}
					}
				}
				results[index] = item
			}
		}(worker)
	}
	workers.Wait()

	combined := &OpenAIForwardResult{Model: parsed.Model, Stream: parsed.Stream, ImageSize: parsed.SizeTier, ImageInputSize: parsed.Size, ImageQuality: parsed.Quality}
	var images []json.RawMessage
	var imageIndices []int
	var failures []gin.H
	var firstFailure *openAIImageBatchItem
	var response map[string]json.RawMessage
	var latestHeaders time.Time
	haveResult := false
	for index := range results {
		item := &results[index]
		if item.err != nil {
			if firstFailure == nil {
				firstFailure = item
			}
			message := "Image generation failed"
			errorType := "upstream_error"
			var upstreamErr *OpenAIImagesUpstreamError
			if errors.As(item.err, &upstreamErr) {
				message = upstreamErr.clientMessage()
				errorType = upstreamErr.clientErrorType()
			}
			failures = append(failures, gin.H{"image_index": index, "error": gin.H{"type": errorType, "message": message}})
			continue
		}
		if response == nil {
			response = item.response
		}
		images = append(images, item.images...)
		for range item.images {
			imageIndices = append(imageIndices, index)
		}
		if item.result != nil {
			part := item.result
			if !haveResult {
				haveResult = true
				combined.Model = part.Model
				combined.BillingModel = part.BillingModel
				combined.ServiceTier = part.ServiceTier
				combined.UpstreamResponseServiceTier = part.UpstreamResponseServiceTier
				combined.ReasoningEffort = part.ReasoningEffort
				combined.RequestedReasoningEffort = part.RequestedReasoningEffort
				combined.RequestID = part.RequestID
				combined.UpstreamSessionID = part.UpstreamSessionID
				combined.UpstreamModel = part.UpstreamModel
				combined.UpstreamEndpoint = part.UpstreamEndpoint
				combined.UpstreamResponseModel = part.UpstreamResponseModel
			} else if part.UpstreamResponseModel != "" && combined.UpstreamResponseModel != "" && part.UpstreamResponseModel != combined.UpstreamResponseModel {
				combined.UpstreamResponseModelConflict = true
			}
			combined.UpstreamResponseModelConflict = combined.UpstreamResponseModelConflict || part.UpstreamResponseModelConflict
			addImagePolicyUsage(&combined.Usage, part.Usage)
			combined.Usage.KiroCredits += part.Usage.KiroCredits
			combined.ImageOutputSizes = append(combined.ImageOutputSizes, part.ImageOutputSizes...)
			if item.finished.After(latestHeaders) {
				latestHeaders = item.finished
				combined.ResponseHeaders = part.ResponseHeaders.Clone()
				combined.UpstreamHeaders = part.UpstreamHeaders.Clone()
			}
		}
		// Read the actual response slots: direct Images results need not populate
		// ForwardResult's base64/URL slices. Keep empty slots aligned for COS upload.
		for _, raw := range item.images {
			var image struct {
				Base64 string `json:"b64_json"`
				URL    string `json:"url"`
			}
			_ = json.Unmarshal(raw, &image)
			combined.ImageOutputBase64 = append(combined.ImageOutputBase64, image.Base64)
			combined.ImageOutputURLs = append(combined.ImageOutputURLs, image.URL)
		}
	}
	if len(images) == 0 {
		if firstFailure != nil {
			if firstFailure.recorder.Code >= http.StatusBadRequest && firstFailure.recorder.Body.Len() > 0 {
				c.Data(firstFailure.recorder.Code, "application/json", firstFailure.recorder.Body.Bytes())
			} else if firstFailure.result != nil {
				upErr := &OpenAIImagesUpstreamError{StatusCode: http.StatusBadGateway, ErrorType: "upstream_error", Message: "image batch returned no valid images"}
				writeOpenAIImagesUpstreamErrorResponse(c, upErr)
				return nil, upErr
			}
			return nil, firstFailure.err
		}
		return nil, errors.New("image batch returned no images")
	}
	combined.ImageCount = len(images)
	combined.Duration = time.Since(start)
	SetOpsUpstreamModel(c, combined.UpstreamModel)
	SetActualOpenAIUpstreamEndpoint(c, combined.UpstreamEndpoint)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, combined.Duration.Milliseconds())
	response["data"], _ = json.Marshal(images)
	response["usage"], _ = json.Marshal(openAIImageBatchUsage(combined.Usage))
	response["requested_images"], _ = json.Marshal(parsed.N)
	response["generated_images"], _ = json.Marshal(len(images))
	if len(failures) > 0 {
		response["errors"], _ = json.Marshal(failures)
	}
	c.Writer.Header().Del("Content-Length")
	if parsed.Stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("X-Accel-Buffering", "no")
		c.Status(http.StatusOK)
		firstToken := int(time.Since(start).Milliseconds())
		combined.FirstTokenMs = &firstToken
		lastWrite := time.Now()
		event := openAIImagesStreamPrefix(parsed) + ".completed"
		for index, raw := range images {
			var item map[string]json.RawMessage
			_ = json.Unmarshal(raw, &item)
			item["type"], _ = json.Marshal(event)
			item["image_index"], _ = json.Marshal(imageIndices[index])
			for _, field := range []string{"background", "output_format", "quality", "size", "model"} {
				if len(item[field]) == 0 && len(response[field]) > 0 {
					item[field] = response[field]
				}
			}
			if parsed.ResponseFormat == "url" && len(item["url"]) == 0 {
				var b64, outputFormat string
				_ = json.Unmarshal(item["b64_json"], &b64)
				_ = json.Unmarshal(item["output_format"], &outputFormat)
				if outputFormat == "" {
					outputFormat = parsed.OutputFormat
				}
				if b64 != "" {
					item["url"], _ = json.Marshal("data:" + openAIImageOutputMIMEType(outputFormat) + ";base64," + b64)
				}
			}
			item["created_at"] = response["created"]
			if len(item["created_at"]) == 0 {
				item["created_at"], _ = json.Marshal(time.Now().Unix())
			}
			if index == len(images)-1 {
				item["usage"] = response["usage"]
				item["requested_images"] = response["requested_images"]
				item["generated_images"] = response["generated_images"]
				if len(failures) > 0 {
					item["batch_errors"] = response["errors"]
				}
			}
			payload, _ := json.Marshal(item)
			s.tryWriteOpenAIImagesStreamEvent(c, c.Writer, &combined.ClientDisconnect, &lastWrite, event, payload)
		}
	} else {
		c.JSON(http.StatusOK, response)
	}
	// Publish one batch result only; children must never mark the task finished.
	s.MarkResponsesImageStatusUpstreamDone(ctx, combined)
	s.scheduleOpenAIImageCosUpload(ctx, combined)
	return combined, nil
}

func openAIImageBatchUsage(usage OpenAIUsage) gin.H {
	return gin.H{
		"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens, "total_tokens": usage.InputTokens + usage.OutputTokens,
		"input_tokens_details":  gin.H{"image_tokens": usage.ImageInputTokens, "text_tokens": max(0, usage.InputTokens-usage.ImageInputTokens), "cached_tokens": usage.CacheReadInputTokens, "cached_tokens_details": gin.H{"image_tokens": usage.ImageCacheReadTokens, "text_tokens": max(0, usage.CacheReadInputTokens-usage.ImageCacheReadTokens)}},
		"output_tokens_details": gin.H{"image_tokens": usage.ImageOutputTokens},
	}
}

func validOpenAIImageBatchData(items []json.RawMessage) []json.RawMessage {
	valid := items[:0]
	for _, raw := range items {
		var item struct {
			Base64 string `json:"b64_json"`
			URL    string `json:"url"`
		}
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		if strings.TrimSpace(item.Base64) != "" || strings.TrimSpace(item.URL) != "" {
			valid = append(valid, raw)
		}
	}
	return valid
}
