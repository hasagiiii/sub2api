package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

func (s *AsyncMediaService) storeInlineImageResult(ctx context.Context, task *AsyncMediaTask, result *AsyncMediaInlineResult, checkpoint func() error) error {
	if s.cos == nil {
		applyInlineImageStorage(result)
		return nil
	}
	config, err := s.cos.loadConfig(ctx)
	if err != nil {
		return err
	}
	if config == nil || !config.Enabled || !config.IsConfigured() {
		reason := "missing_config"
		if config != nil {
			reason = "disabled"
			if config.Enabled {
				reason = "incomplete_config"
			}
		}
		logger.L().Info("image_task.storage_skipped", zap.Int64("task_id", task.ID), zap.String("reason", reason))
		applyInlineImageStorage(result)
		return nil
	}
	if len(result.COSURLs) != len(result.ImageURLs) {
		result.COSURLs = make([]string, len(result.ImageURLs))
	}
	for index, source := range result.ImageURLs {
		if result.COSURLs[index] != "" {
			continue
		}
		uploadCtx, stop := context.WithTimeout(ctx, 30*time.Second)
		var data []byte
		var contentType string
		if strings.HasPrefix(source, "data:") {
			comma := strings.IndexByte(source, ',')
			if comma < 0 || !strings.Contains(source[:comma], ";base64") {
				stop()
				return fmt.Errorf("invalid image data at index %d", index)
			}
			contentType = strings.TrimSuffix(source[5:comma], ";base64")
			data, err = decodeOpenAIImageBase64(source[comma+1:])
		} else {
			data, contentType, err = s.cos.download(uploadCtx, source)
		}
		if err != nil {
			stop()
			return fmt.Errorf("read image %d for storage: %w", index+1, err)
		}
		if contentType == "" {
			contentType = http.DetectContentType(data)
		}
		ext := "png"
		switch normalizeImageContentType(contentType) {
		case "image/jpeg":
			ext = "jpg"
		case "image/webp":
			ext = "webp"
		case "image/gif":
			ext = "gif"
		}
		hash := sha256.Sum256([]byte(task.InternalRequestID))
		key := fmt.Sprintf("%s/tasks/%d-%x/%d.%s", strings.Trim(config.Prefix, "/"), task.ID, hash[:8], index+1, ext)
		logger.L().Info("image_task.image_upload_started", zap.Int64("task_id", task.ID), zap.String("request_id", task.InternalRequestID), zap.Int("image_index", index+1))
		stored, uploadErr := s.cos.UploadBytesWithKey(uploadCtx, key, data, contentType)
		stop()
		if uploadErr != nil {
			return fmt.Errorf("upload image %d: %w", index+1, uploadErr)
		}
		result.COSURLs[index] = stored
		// Journal each completed upload before moving on. A retry uses the same object key.
		if err = checkpoint(); err != nil {
			return err
		}
		logger.L().Info("image_task.image_upload_succeeded", zap.Int64("task_id", task.ID), zap.String("request_id", task.InternalRequestID), zap.Int("image_index", index+1))
	}
	applyInlineImageStorage(result)
	return nil
}

// A partial transfer must never replace the complete image list with empty slots.
func applyInlineImageStorage(result *AsyncMediaInlineResult) {
	mapping := map[string]string{}
	allStored := len(result.COSURLs) == len(result.ImageURLs)
	for index, source := range result.ImageURLs {
		if index >= len(result.COSURLs) || result.COSURLs[index] == "" {
			allStored = false
			continue
		}
		stored := result.COSURLs[index]
		mapping[source] = stored
		result.ImageURLs[index] = stored
		if index < len(result.ImageMetadata) {
			result.ImageMetadata[index].URL = stored
		}
	}
	replaceURLsInPayload(result.ResultPayload, mapping)
	if !allStored {
		result.COSURLs = nil
	}
}
