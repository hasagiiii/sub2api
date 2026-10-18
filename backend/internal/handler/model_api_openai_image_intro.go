package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/fal"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Used only when no administrator-authored introduction exists for this slug.
func defaultOpenAIImageIntro(slug string) *videoModelIntroDTO {
	params := map[string]any{
		"prompt": map[string]any{
			"value": "", "required": true, "widget": "PromptTextArea", "rows": 6,
			"description": "描述要生成或编辑的图片。", "description_en": "Describe the image to generate or edit.",
			"extra": map[string]any{"x-order": 1},
		},
		"image_size": map[string]any{
			"value": fal.SizeAuto, "enum": true, "options": []string{fal.SizeAuto, fal.SizeSquareHD, fal.SizeLandscape43, fal.SizePortrait43},
			"description": "输出图片尺寸。", "description_en": "Output image size.",
			"extra": map[string]any{"x-order": 3},
		},
		"quality": map[string]any{
			"value": "auto", "enum": true, "options": []string{"auto", "low", "medium", "high"},
			"description": "图片质量。", "description_en": "Image quality.",
			"extra": map[string]any{"x-order": 4},
		},
		"num_images": map[string]any{
			"value": 1, "min": 1, "description": "生成图片数量。", "description_en": "Number of images to generate.",
			"extra": map[string]any{"x-order": 5},
		},
	}
	if modelAPIImageAPI(slug) == service.FalAPIEdit {
		params["image_urls"] = map[string]any{
			"items": map[string]any{"value": "", "widget": "image"}, "value": []any{}, "required": true, "minItems": 1,
			"widget": "ImageUrls", "description": "需要编辑的参考图片。", "description_en": "Reference images to edit.",
			"extra": map[string]any{"x-order": 2},
		}
	}
	return &videoModelIntroDTO{
		Title: slug, Description: "OpenAI 图片生成与编辑模型。", DescriptionEn: "OpenAI image generation and editing model.",
		DefaultParams: params,
		OutputFields:  []service.OutputFieldSpec{{Key: "images", Type: "array", Description: "生成的图片。", Items: map[string]any{"properties": map[string]any{"url": map[string]any{"value": "", "description": "图片 URL"}}}}},
		ResultField:   "images[0].url", ResultType: "image",
	}
}
