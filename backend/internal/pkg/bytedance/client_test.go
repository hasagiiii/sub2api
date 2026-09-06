package bytedance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRequest(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"text", `{"prompt":"test"}`, true},
		{"single", `{"prompt":"test","image":"https://example.com/image.png"}`, true},
		{"multiple", `{"prompt":"test","image":["https://example.com/a.png","https://example.com/b.png"]}`, true},
		{"layer", `{"prompt":"test","image":["https://example.com/a.png"],"layer_decomposition":true}`, true},
		{"missing prompt", `{"image":"https://example.com/a.png"}`, false},
		{"missing reference", `{"prompt":"test","layer_decomposition":true}`, false},
		{"multiple layer references", `{"prompt":"test","image":["https://example.com/a.png","https://example.com/b.png"],"layer_decomposition":true}`, false},
		{"sequential", `{"prompt":"test","sequential_image_generation":"disabled"}`, false},
		{"stream", `{"prompt":"test","stream":true}`, false},
		{"search", `{"prompt":"test","tools":[{"type":"web_search"}]}`, false},
		{"invalid reference", `{"prompt":"test","image":[12]}`, false},
		{"wrong boolean", `{"prompt":"test","layer_decomposition":"true"}`, false},
		{"non-url output", `{"prompt":"test","response_format":"b64_json"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, err := NormalizeRequest([]byte(tc.body), domain.SeedreamModel)
			if !tc.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, domain.SeedreamModel, p["model"])
			require.Equal(t, "2K", p["size"])
			require.Equal(t, "url", p["response_format"])
		})
	}
	refs := make([]string, 11)
	for i := range refs {
		refs[i] = "https://example.com/a.png"
	}
	raw, _ := json.Marshal(map[string]any{"prompt": "test", "image": refs})
	_, _, err := NormalizeRequest(raw, domain.SeedreamModel)
	require.Error(t, err)
}

func TestNormalizeRequestPreservesPromptAndRemovesLocalMetadata(t *testing.T) {
	p, meta, err := NormalizeRequest([]byte(`{"prompt":"<bbox>1 2 30 40</bbox>","image":["https://example.com/a.png"],"_annotations":{"image":{}},"_annotation_prompt":{"image":"text"}}`), domain.SeedreamModel)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/a.png", p["image"])
	require.Equal(t, "<bbox>1 2 30 40</bbox>", p["prompt"])
	require.NotContains(t, p, "_annotations")
	require.NotContains(t, p, "_annotation_prompt")
	require.Contains(t, meta, "_annotations")
}

func TestBillableImagesExcludesBackgroundWithoutSubtractingUsage(t *testing.T) {
	for _, n := range []int{0, 8, 16, 17} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			result := map[string]any{"usage": map[string]any{"generated_images": float64(n)}, "data": []any{map[string]any{"url": "https://example.com/base.png", "z_index": float64(0)}}}
			count, err := BillableImages(result, true)
			require.NoError(t, err)
			require.Equal(t, n, count)
		})
	}
	data := []any{map[string]any{"url": "https://example.com/base.png", "z_index": float64(0)}, map[string]any{"url": "https://example.com/layer.png", "z_index": float64(1)}}
	count, err := BillableImages(map[string]any{"data": data}, true)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	count, err = BillableImages(map[string]any{"data": data[:1]}, true)
	require.NoError(t, err)
	require.Zero(t, count)
	_, err = BillableImages(map[string]any{"data": []any{map[string]any{"url": "https://example.com/a.png"}}}, true)
	require.Error(t, err)
	_, err = BillableImages(map[string]any{"usage": map[string]any{"generated_images": 1.5}}, true)
	require.Error(t, err)
}

func TestClientUsesArkProtocolAndPreservesNativeResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v3/images/generations", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		var p map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&p))
		require.Equal(t, domain.SeedreamModel, p["model"])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"url":"https://example.com/layer.png","z_index":1,"bounding_box":{"absolute":[1,2,3,4]},"name":"layer","output_format":"png"}],"usage":{"generated_images":8}}`))
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), BaseURL: server.URL + "/api/v3", APIKey: "test-key"}
	result, err := client.Generate(context.Background(), map[string]any{"model": domain.SeedreamModel, "prompt": "test"})
	require.NoError(t, err)
	data, ok := result["data"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, data)
	entry, ok := data[0].(map[string]any)
	require.True(t, ok)
	require.Contains(t, entry, "bounding_box")
	count, err := BillableImages(result, true)
	require.NoError(t, err)
	require.Equal(t, 8, count)
}

func TestClientRejectsUnsafeConfiguration(t *testing.T) {
	for _, url := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com?key=secret", "file:///tmp/a"} {
		_, err := NewClient(url, "key", "")
		require.Error(t, err)
	}
	_, err := NewClient("", "", "")
	require.Error(t, err)
}
