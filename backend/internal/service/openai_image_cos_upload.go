// Package service: OpenAI 出图 COS 异步转存。
//
// 设计目标（详见对话约定）：
//  1. 客户端响应不被 COS 上传阻塞：响应写完后再 schedule 一个 goroutine 上传。
//  2. usage_log 仍能拿到 cos URL：RecordUsage 在写 usage_log 前会等待 goroutine 完成（带超时）。
//  3. 客户端协议不变：响应仍返回 b64_json/url，cos URL 仅写入 usage_log.cos_urls 字段。
//
// 仅当 OpenAIGatewayService.cosService 已配置且启用、且结果包含 base64 或 URL 时
// 才会真正执行上传；其它情况直接返回（等价 no-op），并把 ImageOutputCosURLs 留空。
package service

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

// openAIImageCosUploadTimeout 是 RecordUsage 等待异步上传完成的最长时间。
// 超时则放弃等待，本次 usage_log.cos_urls 留空但不阻塞计费扣费。
const openAIImageCosUploadTimeout = 10 * time.Second

// scheduleOpenAIImageCosUpload 在写完客户端响应后调用，把 result 中的 base64 图片或
// 临时 URL 异步上传到 COS。结果回填到 result.ImageOutputCosURLs。
//
// done channel 挂到 result.imageCosUploadDone，RecordUsage 调用 waitOpenAIImageCosUpload 等待。
//
// 安全保证：
//   - cosService 为 nil、未启用、未配置时直接返回（done channel 立即关闭，wait 不阻塞）
//   - base64 和 URL 切片均为空时直接返回
//   - 单张图片解码或上传失败不影响其他图片，对应 slot 保留为空字符串
func (s *OpenAIGatewayService) scheduleOpenAIImageCosUpload(ctx context.Context, result *OpenAIForwardResult) {
	if result == nil || s == nil {
		return
	}
	base64s := result.ImageOutputBase64
	sourceURLs := result.ImageOutputURLs
	if len(base64s) == 0 && len(sourceURLs) == 0 {
		s.SucceedResponsesImageStatus(ctx, result)
		return
	}
	if s.cosService == nil {
		s.SucceedResponsesImageStatus(ctx, result)
		return
	}
	// 仅在配置启用时才发起上传。这里不阻塞客户端响应：IsEnabled 内部走 setting 读，已有缓存。
	// The request context may already be canceled when a client disconnects or
	// after the upstream response has been fully drained. Configuration lookup
	// must still be allowed to complete; the actual upload already uses its own
	// bounded context below.
	configCtx := context.Background()
	if ctx != nil {
		configCtx = context.WithoutCancel(ctx)
	}
	if !s.cosService.IsEnabled(configCtx) {
		s.SucceedResponsesImageStatus(ctx, result)
		return
	}

	requestID := strings.TrimSpace(result.RequestID)
	model := strings.TrimSpace(result.Model)

	// 预分配 cos urls 切片（优先与 base64 同序）。失败 slot 保持空串。
	cosURLCount := len(base64s)
	if cosURLCount == 0 {
		cosURLCount = len(sourceURLs)
	}
	cosURLs := make([]string, cosURLCount)
	result.ImageOutputCosURLs = cosURLs
	s.MarkResponsesImageStatusCOSUploading(ctx, result)

	done := make(chan struct{})
	result.imageCosUploadDone = done

	// 使用一个独立的 goroutine 上下文（不绑定 gin request ctx 的取消，避免客户端断开导致上传中断）。
	// 但仍受全局上传超时约束。
	uploadCtx, cancel := context.WithTimeout(context.Background(), openAIImageCosUploadTimeout)

	go func() {
		defer close(done)
		defer cancel()

		// URL 形态的 Responses 结果没有 base64 可供 UploadImageBytes 使用，
		// 直接复用图片 URL 转存链路。通常该分支与 base64 形态互斥；混合
		// 结果时将转存结果填入尚未占用的 slot。
		needsURLTransfer := len(base64s) == 0
		if !needsURLTransfer {
			for _, b64 := range base64s {
				if strings.TrimSpace(b64) == "" {
					needsURLTransfer = true
					break
				}
			}
		}
		if len(sourceURLs) > 0 && needsURLTransfer {
			transferred, _ := s.cosService.TransferImages(uploadCtx, sourceURLs)
			if len(base64s) == 0 {
				copy(cosURLs, transferred)
			} else {
				urlIndex := 0
				for i, b64 := range base64s {
					if strings.TrimSpace(b64) != "" || urlIndex >= len(transferred) {
						continue
					}
					cosURLs[i] = transferred[urlIndex]
					urlIndex++
				}
			}
		}

		var wg sync.WaitGroup
		// 并发上传多张图片，受 COS 服务自身的连接池/限速。
		for i, b64 := range base64s {
			b64 = strings.TrimSpace(b64)
			if b64 == "" {
				continue
			}
			i, b64 := i, b64
			wg.Add(1)
			go func() {
				defer wg.Done()
				data, err := decodeOpenAIImageBase64(b64)
				if err != nil {
					logger.L().Warn("openai.images.cos_upload.decode_failed",
						zap.String("component", "service.openai_gateway"),
						zap.String("request_id", requestID),
						zap.Int("index", i),
						zap.Int("base64_len", len(b64)),
						zap.Error(err))
					return
				}
				cosURL, err := s.cosService.UploadImageBytes(uploadCtx, data, "", model)
				if err != nil {
					logger.L().Warn("openai.images.cos_upload.upload_failed",
						zap.String("component", "service.openai_gateway"),
						zap.String("request_id", requestID),
						zap.Int("index", i),
						zap.Int("data_size", len(data)),
						zap.Error(err))
					return
				}
				if cosURL == "" {
					// IsEnabled=false 时 UploadImageBytes 返回 ("", nil)；理论上前面已做拦截，这里防御。
					return
				}
				cosURLs[i] = cosURL
			}()
		}
		wg.Wait()
		successCount := 0
		for _, u := range cosURLs {
			if u != "" {
				successCount++
			}
		}
		logger.L().Info("openai.images.cos_upload.completed",
			zap.String("component", "service.openai_gateway"),
			zap.String("request_id", requestID),
			zap.Int("total", len(cosURLs)),
			zap.Int("success", successCount))
		s.SucceedResponsesImageStatus(uploadCtx, result)
	}()
}

func decodeOpenAIImageBase64(payload string) ([]byte, error) {
	payload = strings.TrimSpace(payload)
	if comma := strings.IndexByte(payload, ','); strings.HasPrefix(strings.ToLower(payload), "data:") && comma >= 0 {
		payload = payload[comma+1:]
	}
	decoders := []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		base64.URLEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
	}
	var lastErr error
	for _, decode := range decoders {
		data, err := decode(payload)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// waitOpenAIImageCosUpload 在 RecordUsage 写 usage_log 之前调用，等待
// scheduleOpenAIImageCosUpload 启动的异步上传完成。
//
// 等待边界：
//   - result 为 nil 或 imageCosUploadDone 为 nil：立即返回（无异步任务）
//   - 收到 done 信号：上传协程已结束，cos urls 已就绪
//   - openAIImageCosUploadTimeout 超时：放弃等待，cos urls 保留当前部分结果（可能全为空）
//   - ctx 取消：放弃等待，原因同上
func waitOpenAIImageCosUpload(ctx context.Context, result *OpenAIForwardResult) {
	if result == nil || result.imageCosUploadDone == nil {
		return
	}
	timer := time.NewTimer(openAIImageCosUploadTimeout)
	defer timer.Stop()
	select {
	case <-result.imageCosUploadDone:
		return
	case <-ctx.Done():
		logger.L().Warn("openai.images.cos_upload.wait_ctx_cancelled",
			zap.String("component", "service.openai_gateway"),
			zap.String("request_id", strings.TrimSpace(result.RequestID)),
			zap.Error(ctx.Err()))
		return
	case <-timer.C:
		logger.L().Warn("openai.images.cos_upload.wait_timeout",
			zap.String("component", "service.openai_gateway"),
			zap.String("request_id", strings.TrimSpace(result.RequestID)),
			zap.Duration("timeout", openAIImageCosUploadTimeout))
		return
	}
}

// nonEmptyStringSlice 返回切片本身（如有任意非空元素），否则返回 nil。
// 用于 usage_log.cos_urls 字段：避免对全空切片 marshal 为 "[]" 入库。
func nonEmptyStringSlice(in []string) []string {
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			return in
		}
	}
	return nil
}
