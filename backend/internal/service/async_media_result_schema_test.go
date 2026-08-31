//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractConfiguredImageURLsUsesResultField(t *testing.T) {
	intro := &ModelIntro{
		ResultField:  "image.url",
		OutputFields: []OutputFieldSpec{{Key: "image.url", Type: "string"}},
	}
	payload := map[string]any{
		"image": map[string]any{
			"url":          "https://cdn.example/image.png",
			"width":        float64(1832),
			"content_type": "image/png",
		},
	}

	urls, fields := extractConfiguredImageURLs(payload, intro)
	require.Equal(t, []string{"https://cdn.example/image.png"}, urls)
	require.Equal(t, []string{"image.url"}, fields)
}

func TestExtractConfiguredImageURLsSupportsWildcardArrays(t *testing.T) {
	intro := &ModelIntro{ResultField: "images[*].url"}
	payload := map[string]any{"images": []any{
		map[string]any{"url": "https://cdn.example/1.png"},
		map[string]any{"url": "https://cdn.example/2.png"},
	}}

	urls, fields := extractConfiguredImageURLs(payload, intro)
	require.Equal(t, []string{"https://cdn.example/1.png", "https://cdn.example/2.png"}, urls)
	require.Equal(t, []string{"images[*].url"}, fields)
}

func TestExtractConfiguredImageMetadataPreservesRawImageObject(t *testing.T) {
	payload := map[string]any{
		"image": map[string]any{
			"url":          "https://cdn.example/image.png",
			"content_type": "image/png",
			"file_name":    "result.png",
			"file_size":    float64(1234),
			"width":        float64(1832),
			"height":       float64(2289),
		},
	}

	metadata := extractConfiguredImageMetadata(payload, []string{"https://cdn.example/image.png"})
	require.Len(t, metadata, 1)
	require.Equal(t, ImageOutputMetadata{
		URL: "https://cdn.example/image.png", ContentType: "image/png", FileName: "result.png",
		FileSize: 1234, Width: 1832, Height: 2289,
	}, metadata[0])
	require.Equal(t, []string{"1832x2289"}, imageOutputSizesFromMetadata(metadata))
}
