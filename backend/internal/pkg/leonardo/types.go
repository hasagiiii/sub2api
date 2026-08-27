package leonardo

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/fal"
)

const (
	StatusCompleted = "COMPLETED"
	StatusFailed    = "FAILED"

	DefaultBaseURL             = "http://127.0.0.1:28080"
	DefaultEstimatedCreditCost = 8
)

type GenerateInput struct {
	Prompt             string   `json:"prompt"`
	Quality            string   `json:"quality"`
	Width              int      `json:"width"`
	Height             int      `json:"height"`
	ReferenceImageURLs []string `json:"reference_image_urls,omitempty"`
}

type SubmitRequest struct {
	Provider            string        `json:"provider"`
	TaskType            string        `json:"task_type"`
	Model               string        `json:"model"`
	Mode                string        `json:"mode"`
	Input               GenerateInput `json:"input"`
	EstimatedCreditCost float64       `json:"estimated_credit_cost"`
}

type Media struct {
	ID        string `json:"id,omitempty"`
	URL       string `json:"url"`
	Type      string `json:"type,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	MIMEType  string `json:"mime_type,omitempty"`
	FileName  string `json:"file_name,omitempty"`
	FileSize  int64  `json:"file_size,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
}

type Output struct {
	Media []Media `json:"media"`
}

type Task struct {
	TaskUUID string `json:"task_uuid"`
	Status   string `json:"status"`
	Output   Output `json:"output"`
	Error    any    `json:"error,omitempty"`
}

type imageSize struct {
	width  int
	height int
}

type imageSizeGroup struct {
	aspectRatio float64
	sizes       [3]imageSize
}

// Leonardo only accepts these image dimensions. Keep the nominal aspect ratio
// separate because a few supported dimensions are rounded to platform-aligned
// pixel boundaries.
var supportedImageSizeGroups = [...]imageSizeGroup{
	{aspectRatio: 21.0 / 9.0, sizes: [3]imageSize{{1584, 672}, {2048, 864}, {3808, 1632}}},
	{aspectRatio: 16.0 / 9.0, sizes: [3]imageSize{{1376, 768}, {2048, 1136}, {3584, 2016}}},
	{aspectRatio: 3.0 / 2.0, sizes: [3]imageSize{{1264, 848}, {2048, 1376}, {3504, 2336}}},
	{aspectRatio: 4.0 / 3.0, sizes: [3]imageSize{{1200, 896}, {2048, 1536}, {3264, 2448}}},
	{aspectRatio: 5.0 / 4.0, sizes: [3]imageSize{{1152, 928}, {2048, 1648}, {3200, 2560}}},
	{aspectRatio: 1.0, sizes: [3]imageSize{{1024, 1024}, {2048, 2048}, {2880, 2880}}},
	{aspectRatio: 4.0 / 5.0, sizes: [3]imageSize{{928, 1152}, {1648, 2048}, {2560, 3200}}},
	{aspectRatio: 3.0 / 4.0, sizes: [3]imageSize{{896, 1200}, {1536, 2048}, {2448, 3264}}},
	{aspectRatio: 2.0 / 3.0, sizes: [3]imageSize{{848, 1264}, {1376, 2048}, {2336, 3504}}},
	{aspectRatio: 9.0 / 16.0, sizes: [3]imageSize{{768, 1376}, {1136, 2048}, {2016, 3584}}},
}

func (t *Task) IsCompleted() bool {
	return t != nil && strings.EqualFold(strings.TrimSpace(t.Status), StatusCompleted)
}

func (t *Task) IsFailed() bool {
	return t != nil && strings.EqualFold(strings.TrimSpace(t.Status), StatusFailed)
}

func (t *Task) FailureMessage() string {
	if t == nil || t.Error == nil {
		return "task failed"
	}
	return fmt.Sprintf("task failed: %v", t.Error)
}

func BuildSubmitRequest(model string, input fal.ImageGenInput, estimatedCreditCost float64) *SubmitRequest {
	if strings.TrimSpace(model) == "" {
		model = "gpt-image-2"
	}
	if estimatedCreditCost <= 0 {
		estimatedCreditCost = DefaultEstimatedCreditCost
	}
	width, height := imageDimensions(input.Size)
	mode := "text-to-image"
	var referenceImageURLs []string
	if input.IsEdit {
		mode = "image-to-image"
		referenceImageURLs = trimReferenceImageURLs(input.ImageURLs)
	}
	return &SubmitRequest{
		Provider: "leonardo",
		TaskType: "IMAGE_GENERATION",
		Model:    strings.TrimSpace(model),
		Mode:     mode,
		Input: GenerateInput{
			Prompt:             strings.TrimSpace(input.Prompt),
			Quality:            normalizeQuality(input.Quality),
			Width:              width,
			Height:             height,
			ReferenceImageURLs: referenceImageURLs,
		},
		EstimatedCreditCost: estimatedCreditCost,
	}
}

func trimReferenceImageURLs(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func normalizeQuality(value string) string {
	switch strings.ToUpper(strings.TrimSpace(fal.MapQualityToFal(value))) {
	case "MEDIUM", "HIGH":
		return strings.ToUpper(strings.TrimSpace(fal.MapQualityToFal(value)))
	case "AUTO":
		// Leonardo does not expose an AUTO quality; use its medium preset.
		return "MEDIUM"
	default:
		return "LOW"
	}
}

func imageDimensions(value string) (int, int) {
	value = strings.ToLower(strings.TrimSpace(value))
	parts := strings.SplitN(value, "x", 2)
	if len(parts) != 2 {
		return 1024, 1024
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return 1024, 1024
	}
	closest := closestSupportedImageSize(w, h)
	return closest.width, closest.height
}

func closestSupportedImageSize(width, height int) imageSize {
	targetRatio := float64(width) / float64(height)
	group := supportedImageSizeGroups[0]
	closestRatioDistance := math.Abs(targetRatio - group.aspectRatio)
	for _, candidate := range supportedImageSizeGroups[1:] {
		distance := math.Abs(targetRatio - candidate.aspectRatio)
		if distance < closestRatioDistance {
			group = candidate
			closestRatioDistance = distance
		}
	}

	closest := group.sizes[0]
	closestDistance := squaredDimensionDistance(width, height, closest)
	for _, candidate := range group.sizes[1:] {
		distance := squaredDimensionDistance(width, height, candidate)
		if distance < closestDistance {
			closest = candidate
			closestDistance = distance
		}
	}
	return closest
}

func squaredDimensionDistance(width, height int, candidate imageSize) float64 {
	widthDelta := float64(width) - float64(candidate.width)
	heightDelta := float64(height) - float64(candidate.height)
	return widthDelta*widthDelta + heightDelta*heightDelta
}
